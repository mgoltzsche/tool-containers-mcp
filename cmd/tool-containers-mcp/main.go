package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/mgoltzsche/tool-containers-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is overwritten by the build with the actual version.
var Version = "0.0.0-dev"

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	server := mcp.NewServer(&mcp.Implementation{Name: "tool-containers-mcp", Version: Version}, nil)

	toolImpls, err := initMCPTools()
	if err != nil {
		server.AddReceivingMiddleware(failingMiddleware(err))
		go func() {
			// Terminate the server process 1s later.
			// This is to let the MCP client obtain the error message on initialization,
			// allowing it to return it to the user.
			// Otherwise the user has no idea of the root cause since stderr is ignored.
			time.Sleep(time.Second)
			cancel()
		}()
	}

	for _, tool := range toolImpls {
		server.AddTool(tool.Tool, tool.Handler)
	}

	return server.Run(ctx, &mcp.StdioTransport{})
}

func initMCPTools() ([]tools.Tool, error) {
	configFile := "/etc/tool-containers-mcp/tools.yaml"
	showVersion := false

	f := flag.CommandLine
	f.StringVar(&configFile, "config", configFile, "path to the configuration file")
	f.BoolVar(&showVersion, "version", false, "print the binary version and exit")

	err := f.Parse(os.Args[1:])
	if err != nil {
		return nil, err
	}

	if f.NArg() > 0 {
		return nil, fmt.Errorf("no positional arguments supported but %d provided", len(f.Args()))
	}

	if showVersion {
		fmt.Println("tool-containers-mcp", Version)
		os.Exit(0)
	}

	cfg, err := config.ConfigurationFromFile(configFile)
	if err != nil {
		return nil, err
	}

	return tools.ToMCPServerTools(cfg.Tools)
}

// failingMiddleware return the given error for every MCP request.
// This is particularly to make the MCP client initialization fail in a way that the user can see the error message.
// Otherwise, when terminating the process immediately, the root cause is not visible for MCP client users since the log messages are not shown.
func failingMiddleware(err error) mcp.Middleware {
	slog.Error("failed to initialize tools", "error", err)
	err = fmt.Errorf("tool-containers-mcp: %w", err)
	return func(h mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return nil, err
		}
	}
}
