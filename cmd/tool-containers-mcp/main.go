package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/mgoltzsche/tool-containers-mcp/internal/tools"
)

// Version is overwritten by the build with the actual version.
var Version = "0.0.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	configFile := "/etc/tool-containers-mcp/tools.yaml"
	showVersion := false

	flag.StringVar(&configFile, "config", configFile, "path to the configuration file")
	flag.BoolVar(&showVersion, "version", false, "print the binary version and exit")
	flag.Parse()

	if len(flag.Args()) > 0 {
		return fmt.Errorf("no positional arguments supported but %d provided", len(flag.Args()))
	}

	if showVersion {
		fmt.Println("tool-containers-mcp", Version)
		return nil
	}

	cfg, err := config.ConfigurationFromFile(configFile)
	if err != nil {
		// TODO: make these errors (missing/invalid option) obvious for users
		return err
	}

	tools, err := tools.ToMCPServerTools(cfg.Tools)
	if err != nil {
		return err
	}

	s := server.NewMCPServer(
		"tool-containers-mcp",
		"0.0.1",
		server.WithToolCapabilities(false),
	)

	s.AddTools(tools...)

	return server.ServeStdio(s)
}
