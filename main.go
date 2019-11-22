package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/vishvananda/netns"
)

type netInfo struct {
	InOctets  uint64
	OutOctets uint64
}

type containerInfo struct {
	ID             string
	Name           string
	NetworkSummary *netInfo
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

func extractNetInfo(headers string, content string) (*netInfo, error) {
	splittedHeaders := strings.Split(headers, " ")
	splittedContent := strings.Split(content, " ")

	mappedContent := make(map[string]uint64)

	for i, header := range splittedHeaders {
		value, err := strconv.ParseUint(splittedContent[i], 10, 64)
		if err != nil {
			return nil, err
		}
		mappedContent[header] = value
	}
	log.Printf("Map: %v", mappedContent)
	return &netInfo{
		InOctets:  mappedContent["InOctets"],
		OutOctets: mappedContent["OutOctets"],
	}, nil
}

// Reads /proc/net/netstat information in the namespace of the specified docker container
// and parses the content into a netInfo structure
func netstat() (*netInfo, error) {
	file, err := os.OpenFile("/proc/net/netstat", os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("Cannot read /proc/net/netstat: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var headers string
	var content string
	for scanner.Scan() {
		text := scanner.Text()
		if strings.HasPrefix(text, "IpExt: ") {
			if len(headers) == 0 {
				headers = text[7:] // Strip "IpExt: "
			} else {
				content = text[7:] // Strip "IpExt: "
			}
		}
	}
	if len(headers) > 0 && len(content) > 0 {
		return extractNetInfo(headers, content)
	}
	return nil, errors.New("Cannot find IpExt information in /proc/net/netstat")
}

// Collects all network information from all running contaniers and
// returns them in a map where the keys are the contanier IDs
func collectInfo(cli *client.Client) (contanierInfoMap, error) {
	result := make(contanierInfoMap)
	context := context.Background()

	containers, err := cli.ContainerList(context, types.ContainerListOptions{})
	if err != nil {
		return nil, err
	}

	// Stick to the thread because setns() call has thread local resources
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Store current namespace and restore it later
	currentNs, err := netns.Get()
	if err != nil {
		return nil, fmt.Errorf("Unable to get current namespace: %v", err)
	}
	// Note: from now on, do not return before resetting the namespace
	defer currentNs.Close()

	for _, container := range containers {
		name, err := parseContainerName(container.Names)
		if err != nil {
			log.Println(fmt.Errorf("Error while parsing container names %v for container %v: %v",
				container.Names, container.ID, err))
			continue
		}

		handle, err := netns.GetFromDocker(container.ID)
		if err != nil {
			log.Println(fmt.Errorf("Cannot get network namespace file handle for container %v: %v", container.ID, err))
			continue
		}

		err = netns.Set(handle)
		log.Printf("Handle: %v", handle.UniqueId())
		handle.Close()
		if err != nil {
			log.Println(fmt.Errorf("Cannot switch network namespace: %v", err))
			continue
		}

		netInfo, err := netstat()
		if err != nil {
			log.Println(fmt.Errorf("Error while collecting netstats for container %v: %v", container.ID, err))
			continue
		}

		result[container.ID] = containerInfo{
			ID:             container.ID,
			Name:           name,
			NetworkSummary: netInfo,
		}
	}

	if err := netns.Set(currentNs); err != nil {
		return nil, err
	}
	return result, nil
}

var containerMap atomic.Value // holds current contanier information
func collector(cli *client.Client) {
	for {
		currentContainerMap, err := collectInfo(cli)
		if err == nil {
			containerMap.Store(currentContainerMap)
		}
		time.Sleep(5 * time.Second)
	}
}

func metrics(cli *client.Client, w http.ResponseWriter, req *http.Request) error {
	const SentCounterName = "docker_network_sent_bytes_total"
	const ReceivedCounterName = "docker_network_received_bytes_total"
	const MetricHeaderFormat = "# HELP %[1]s\n# TYPE %[1]s counter\n"
	const MetricFormat = "%s{container=\"%s\"} %d\n"

	currentContainerMap := containerMap.Load().(contanierInfoMap)

	fmt.Fprintf(w, MetricHeaderFormat, ReceivedCounterName)
	for _, container := range currentContainerMap {
		fmt.Fprintf(w, MetricFormat, ReceivedCounterName, container.Name, container.NetworkSummary.InOctets)
	}

	fmt.Fprintf(w, MetricHeaderFormat, SentCounterName)
	for _, container := range currentContainerMap {
		fmt.Fprintf(w, MetricFormat, SentCounterName, container.Name, container.NetworkSummary.OutOctets)
	}

	return nil
}

func main() {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}
	cli.NegotiateAPIVersion(ctx)

	go collector(cli)

	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if err := metrics(cli, w, r); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})
	log.Fatal(http.ListenAndServe(":8099", nil))
}
