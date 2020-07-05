package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/cosenmarco/docker-prometheus-exporter/configuration"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Enum for the health of running containers
var healthStateMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "docker_container_health",
	Help: "Health check status of the container. 0 for n/a, 1 for starting, 2 for healthy, -1 for unhealthy",
}, []string{"container_name"})

// Max (across all processes inside the container) open file descriptors per container
var maxOpenFilesMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "docker_container_max_open_files",
	Help: "Momentary max (across all processes inside the container) open file descriptors per container",
}, []string{"container_name"})

func healthCheckToGaugeValue(state string) float64 {
	switch state {
	case "starting":
		return 1.0
	case "unhealthy":
		return -1.0
	case "healthy":
		return 2.0
	default:
		return -2.0
	}
}

func healthGaugeValueFromInspection(inspection types.ContainerJSON) float64 {
	if inspection.State.Health != nil {
		return healthCheckToGaugeValue(inspection.State.Health.Status)
	}
	return 0
}

// Counts the number of file descriptors by listing the /proc{pid}/fd directory
func fetchOpenFilesCount(pid string) (int64, error) {
	var count int64 = 0
	files, err := ioutil.ReadDir("/proc/" + pid + "/fd")
	if err != nil {
		return 0, err
	}
	for _, file := range files {
		if !file.IsDir() {
			count++
		}
	}
	return count, nil
}

func max(x, y int64) int64 {
	if x < y {
		return y
	}
	return x
}

func maxOpenFilesGaugeValueFromTop(top container.ContainerTopOKBody, cli *client.Client, ctx *context.Context) (float64, error) {
	var maxOpenFiles int64 = 0
	for i := 0; i < len(top.Processes); i++ {
		pid := top.Processes[i][1]
		openFiles, err := fetchOpenFilesCount(pid)
		if err != nil {
			return 0, err
		}
		maxOpenFiles = max(maxOpenFiles, openFiles)
	}
	return float64(maxOpenFiles), nil
}

func collectInfo(config *configuration.Configuration, cli *client.Client, ctx *context.Context) {
	containers, err := cli.ContainerList(*ctx, types.ContainerListOptions{
		All: true, // We want to delete gauges for non-running containers
	})

	if err != nil {
		panic(err)
	}

	for _, container := range containers {
		name := container.Names[0]
		var err error

		inspection, err := cli.ContainerInspect(*ctx, container.ID)
		if err != nil {
			log.Println(fmt.Errorf("Error while inspecting container %v: %v",
				container.ID, err))
			continue
		}

		if inspection.State.Running {
			healthGauge, err := healthStateMetric.GetMetricWithLabelValues(name)
			if err != nil {
				log.Println(fmt.Errorf("can't get health gauge for container %v: %v",
					container.ID, err))
				continue
			}
			healthGauge.Set(healthGaugeValueFromInspection(inspection))

			maxOpenFilesGauge, err := maxOpenFilesMetric.GetMetricWithLabelValues(name)
			if err != nil {
				log.Println(fmt.Errorf("can't get maxOpenFiles gauge for container %v: %v",
					container.ID, err))
				continue
			}

			top, err := cli.ContainerTop(*ctx, container.ID, []string{})
			if err != nil {
				log.Println(fmt.Errorf("can't perform top for container %v: %v",
					container.ID, err))
				continue
			}
			maxOpenFiles, err := maxOpenFilesGaugeValueFromTop(top, cli, ctx)
			if err != nil {
				log.Println(fmt.Errorf("can't count max open files for container %v: %v",
					container.ID, err))
				continue
			}
			maxOpenFilesGauge.Set(maxOpenFiles)

		} else {
			healthStateMetric.DeleteLabelValues(name)
			maxOpenFilesMetric.DeleteLabelValues(name)
		}
	}
}

func collector(config *configuration.Configuration) {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}
	defer cli.Close()
	cli.NegotiateAPIVersion(ctx)

	for {
		collectInfo(config, cli, &ctx)
		time.Sleep(time.Duration(config.Interval) * time.Millisecond)
	}
}

func main() {
	config := configuration.RetrieveConfiguration()
	registerer := prometheus.DefaultRegisterer
	gatherer := prometheus.DefaultGatherer

	if config.SuppressDefaultMetrics {
		registry := prometheus.NewRegistry()
		registerer = registry
		gatherer = registry
	}

	registerer.MustRegister(healthStateMetric)
	registerer.MustRegister(maxOpenFilesMetric)

	go collector(config)

	http.Handle(*config.MetricsPath, promhttp.HandlerFor(
		gatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.Port), nil))
}
