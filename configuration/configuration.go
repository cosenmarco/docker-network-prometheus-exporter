package configuration

import (
	"fmt"
	"os"

	"github.com/akamensky/argparse"
)

// The Configuration is a set of options specified on the command line
type Configuration struct {
	Port               int
	SkipNameProcessing bool
}

// RetrieveConfiguration will retrieve the configuration by parsing the command line
func RetrieveConfiguration() *Configuration {
	parser := argparse.NewParser("docker-prometheus-exporter",
		"A Prometheus exporter which makes available some metrics from running docker containers")

	port := parser.Int("p", "port", &argparse.Options{Required: true, Help: "The port the server will be listening on"})
	names := parser.Flag("n", "original-names", &argparse.Options{Help: "Disable container name processing"})

	err := parser.Parse(os.Args)
	if err != nil {
		// In case of error print error and print usage
		// This can also be done by passing -h or --help flags
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	return &Configuration{
		Port:               *port,
		SkipNameProcessing: *names}
}
