package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/mgoltzsche/tool-containers-mcp/internal/engine/docker"
	"github.com/mgoltzsche/tool-containers-mcp/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Version is overwritten by the build with the actual version.
var Version = "0.0.0-dev"

var errLogged = false

const serverModeStdin = "stdin"

// Flags
var configFile = "/etc/tool-containers-mcp/tools.yaml"
var showVersion = false
var listenAddress = serverModeStdin
var pullPolicy = string(docker.ImagePullPolicyAlways)

func main() {
	if err := run(); err != nil {
		if !errLogged {
			slog.Error(err.Error())
		}
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	server := mcp.NewServer(&mcp.Implementation{Name: "tool-containers-mcp", Version: Version}, nil)

	toolImpls, closer, err := initMCPTools(ctx)
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

	defer closer()

	for _, tool := range toolImpls {
		server.AddTool(tool.Tool, tool.Handler)
	}

	switch listenAddress {
	case serverModeStdin:
		if err == nil {
			slog.Info("serving MCP via stdin/stdout")
		}
		return server.Run(ctx, &mcp.StdioTransport{})
	default:
		if err != nil {
			return err
		}

		slog.Info("serving MCP via HTTP", "address", listenAddress)

		srv := &http.Server{
			Addr: listenAddress,
			Handler: mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
				return server
			}, nil),
		}
		go func() {
			<-ctx.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			slog.Info("terminating")
			if e := srv.Shutdown(ctx); e != nil {
				slog.Error(fmt.Sprintf("shutdown: %s", e))
			}
		}()
		err = srv.ListenAndServe()
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func parseFlags() error {
	f := flag.CommandLine
	f.StringVar(&configFile, "config", configFile, "path to the configuration file")
	f.StringVar(&listenAddress, "address", listenAddress, "address to listen on: stdin or (IP and) port, e.g. :8080")
	f.StringVar(&pullPolicy, "pull", pullPolicy, "container image pull policy: never or always")
	f.BoolVar(&showVersion, "version", false, "print the binary version and exit")

	err := f.Parse(os.Args[1:])
	if err != nil {
		return err
	}

	if f.NArg() > 0 {
		return fmt.Errorf("no positional arguments supported but %d provided", len(f.Args()))
	}

	if showVersion {
		fmt.Println("tool-containers-mcp", Version)
		os.Exit(0)
	}

	return nil
}

func initMCPTools(ctx context.Context) ([]tools.Tool, func() error, error) {
	err := parseFlags()
	if err != nil {
		return nil, noopClose, err
	}

	cfg, err := config.ConfigurationFromFile(configFile)
	if err != nil {
		return nil, noopClose, err
	}

	factory, err := docker.New(docker.ImagePullPolicy(pullPolicy))
	if err != nil {
		return nil, noopClose, err
	}

	toolImpls, err := tools.ToMCPServerTools(ctx, cfg.Tools, factory.NewHandler)
	if err != nil {
		_ = factory.Close()

		return nil, noopClose, err
	}

	return toolImpls, factory.Close, nil
}

func noopClose() error {
	return nil
}

// failingMiddleware return the given error for every MCP request.
// This is particularly to make the MCP client initialization fail in a way that the user can see the error message.
// Otherwise, when terminating the process immediately, the root cause is not visible for MCP client users since the log messages are not shown.
func failingMiddleware(err error) mcp.Middleware {
	slog.Error("failed to initialize tools", "error", err)
	errLogged = true
	err = fmt.Errorf("tool-containers-mcp: %w", err)

	return func(h mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			return nil, err
		}
	}
}
