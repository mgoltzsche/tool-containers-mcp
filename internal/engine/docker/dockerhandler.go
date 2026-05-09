package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/mgoltzsche/tool-containers-mcp/internal/engine"
	"github.com/mgoltzsche/tool-containers-mcp/internal/utils"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ImagePullPolicy string

const (
	ImagePullPolicyNever  ImagePullPolicy = "never"
	ImagePullPolicyAlways ImagePullPolicy = "always"
)

func New(pullPolicy ImagePullPolicy) (engine.HandlerFactory, error) {
	c, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	return &docker{
		client:     c,
		pullPolicy: pullPolicy,
	}, nil
}

type docker struct {
	client     client.APIClient
	pullPolicy ImagePullPolicy
}

func (e *docker) Close() error {
	return e.client.Close()
}

func (e *docker) NewHandler(ctx context.Context, toolName, imageRef string) (engine.ToolHandler, error) {
	switch e.pullPolicy {
	case ImagePullPolicyAlways:
		slog.Info("pulling tool container image", "tool", toolName, "image", imageRef)

		reader, err := e.client.ImagePull(ctx, imageRef, client.ImagePullOptions{})
		if err != nil {
			return nil, fmt.Errorf("pull image: %w", err)
		}

		defer reader.Close()
		_, _ = io.Copy(io.Discard, reader)
	case ImagePullPolicyNever:
	default:
		return nil, fmt.Errorf("unsupported pull policy %q provided, expected one of %s or %s", e.pullPolicy, ImagePullPolicyNever, ImagePullPolicyAlways)
	}

	return func(ctx context.Context, c config.Container) (*mcp.CallToolResult, error) {
		env := make([]string, 0, len(c.Env))
		for k, v := range c.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}

		timeout := c.Timeout
		if timeout == 0 {
			timeout = 60 * time.Second
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		containerConfig := &container.Config{
			Image: c.Image,
			Cmd:   c.Args,
			Env:   env,
		}

		if c.Command != "" {
			containerConfig.Entrypoint = []string{c.Command}
		}

		var hostConfig *container.HostConfig
		if c.Network != "" {
			hostConfig = &container.HostConfig{
				NetworkMode: container.NetworkMode(c.Network),
			}
		}

		resp, err := e.client.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config:     containerConfig,
			HostConfig: hostConfig,
		})
		if err != nil {
			return nil, fmt.Errorf("create tool container: %w", err)
		}

		defer func() {
			_, err := e.client.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{
				Force:         true,
				RemoveVolumes: true,
			})
			if err != nil {
				slog.Warn(fmt.Sprintf("failed to remove tool container: %s", err), "tool", toolName)
			}
		}()

		if _, err := e.client.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
			return nil, fmt.Errorf("failed to start tool container: %w", err)
		}

		waitResult := e.client.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{
			Condition: container.WaitConditionNotRunning,
		})
		select {
		case err := <-waitResult.Error:
			if err != nil {
				return nil, fmt.Errorf("%w%s", err, errDetails(ctx, resp.ID, e.client))
			}
		case result := <-waitResult.Result:
			if result.StatusCode != 0 {
				return utils.ToolError(fmt.Sprintf("exited with code %d%s", result.StatusCode, errDetails(ctx, resp.ID, e.client))), nil
			}
		}

		out, err := e.client.ContainerLogs(ctx, resp.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
		if err != nil {
			return nil, fmt.Errorf("read tool container output: %w", err)
		}

		defer out.Close()

		var stdout, stderr bytes.Buffer

		_, err = stdcopy.StdCopy(&stdout, &stderr, out)
		if err != nil {
			return nil, fmt.Errorf("read tool container output: %w", err)
		}

		for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
			if line != "" {
				slog.Warn(line, "tool", toolName)
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSpace(stdout.String())}},
		}, nil
	}, nil
}

func errDetails(ctx context.Context, containerID string, c client.APIClient) string {
	suffix := ""
	out, e := c.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{ShowStderr: true})
	if e == nil {
		defer out.Close()
		var stdout, stderr bytes.Buffer
		_, _ = stdcopy.StdCopy(&stdout, &stderr, out)
		errLog := strings.TrimSpace(stderr.String())
		if errLog != "" {
			suffix = fmt.Sprintf(", stderr: %s", errLog)
		}
	}
	return suffix
}
