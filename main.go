package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cosenmarco/docker-prometheus-exporter/configuration"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type netInfo struct {
	InOctets  uint64
	OutOctets uint64
}

type containerInfo struct {
	ID             string
	Name           string
	NetworkSummary *types.StatsJSON
}

type contanierInfoMap map[string]containerInfo

func parseContainerName(original []string) (string, error) {
	if len(original) < 1 {
		return "", errors.New("No container names given")
	}
	nameParts := strings.Split(original[0], "_")
	if len(nameParts) < 2 {
		return "", errors.New("Cannot find interesting container name part")
	}
	return nameParts[1], nil
}

// Collects all network information from all running containers and
// returns them in a map where the keys are the contanier IDs
func collectInfo(config *configuration.Configuration) (contanierInfoMap, error) {
	result := make(contanierInfoMap)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}
	cli.NegotiateAPIVersion(ctx)

	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return nil, err
	}

	for _, container := range containers {
		name := container.Names[0]
		var err error
		if !config.SkipNameProcessing {
			name, err = parseContainerName(container.Names)
			if err != nil {
				log.Println(fmt.Errorf("Error while parsing container names %v for container %v: %v",
					container.Names, container.ID, err))
				continue
			}
		}

		stats, err := cli.ContainerStats(ctx, container.ID, false)
		if err != nil {
			log.Println(fmt.Errorf("Error while getting stats of container %v: %v",
				container.ID, err))
			continue
		}

		var containerStats types.StatsJSON
		err = json.NewDecoder(stats.Body).Decode(&containerStats)
		stats.Body.Close()
		if err != nil {
			log.Println(fmt.Errorf("Error while reading and parsing stats of container %v: %v",
				container.ID, err))
			continue
		}

		result[container.ID] = containerInfo{
			ID:             container.ID,
			Name:           name,
			NetworkSummary: &containerStats,
		}
	}

	return result, nil
}

var containerMap atomic.Value // holds current contanier information
func collector(config *configuration.Configuration) {
	for {
		currentContainerMap, err := collectInfo(config)
		if err == nil {
			containerMap.Store(currentContainerMap)
		}
		time.Sleep(5 * time.Second)
	}
}

func metrics(w http.ResponseWriter, req *http.Request) error {
	const SentCounterName = "docker_network_sent_bytes_total"
	const ReceivedCounterName = "docker_network_received_bytes_total"
	const MetricHeaderFormat = "# HELP %[1]s\n# TYPE %[1]s counter\n"
	const MetricFormat = "%s{container=\"%s\"} %d\n"

	currentContainerMap := containerMap.Load().(contanierInfoMap)

	fmt.Fprintf(w, MetricHeaderFormat, ReceivedCounterName)
	for _, container := range currentContainerMap {
		fmt.Fprintf(w, MetricFormat, ReceivedCounterName, container.Name,
			container.NetworkSummary.Networks["eth0"].RxBytes)
	}

	fmt.Fprintf(w, MetricHeaderFormat, SentCounterName)
	for _, container := range currentContainerMap {
		fmt.Fprintf(w, MetricFormat, SentCounterName, container.Name,
			container.NetworkSummary.Networks["eth0"].TxBytes)
	}

	return nil
}

func main() {
	containerMap.Store(make(contanierInfoMap))
	config := configuration.RetrieveConfiguration()

	go collector(config)

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if err := metrics(w, r); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.Port), nil))
}
