package configuration

import (
	"fmt"
	"os"

	"github.com/akamensky/argparse"
)

// The Configuration is a set of options specified on the command line
type Configuration struct {
	Port                   int
	Interval               int
	SuppressDefaultMetrics bool
	MetricsPath            *string
}

// RetrieveConfiguration will retrieve the configuration by parsing the command line
func RetrieveConfiguration() *Configuration {
	parser := argparse.NewParser("docker-prometheus-exporter",
		"A Prometheus exporter which makes available some metrics from running docker containers")

	port := parser.Int("p", "port", &argparse.Options{Required: true, Help: "The port the server will be listening on eg. 8090"})
	interval := parser.Int("i", "interval", &argparse.Options{Required: true, Help: "The interval of checking in milliseconds eg. 10000"})
	suppressDefaultMetrics := parser.Flag("s", "suppress-default-metrics", &argparse.Options{Help: "The interval of checking in milliseconds eg. 10000"})
	metricsPath := parser.String("m", "metrics-path", &argparse.Options{
		Help:    "The path at which metrics are exposed eg. /the_metrics",
		Default: "/metrics"})

	err := parser.Parse(os.Args)
	if err != nil {
		// In case of error print error and print usage
		// This can also be done by passing -h or --help flags
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	return &Configuration{
		Port:                   *port,
		Interval:               *interval,
		SuppressDefaultMetrics: *suppressDefaultMetrics,
		MetricsPath:            metricsPath}
}
