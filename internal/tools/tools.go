package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

var nameNormalizationRegex = regexp.MustCompile("[^a-zA-Z0-6]+")

func ToMCPServerTools(toolDefs map[string]config.ToolDefinition) ([]server.ServerTool, error) {
	tools := make([]server.ServerTool, 0, len(toolDefs))
	toolNames := slices.Sorted(maps.Keys(toolDefs))

	if len(toolDefs) == 0 {
		return nil, errors.New("no tool definitions specified")
	}

	sort.Strings(toolNames)

	for _, toolName := range toolNames {
		t := toolDefs[toolName]

		if toolName == "" {
			return nil, errors.New("no name specified for tool definition")
		}

		if t.Description == "" {
			return nil, errors.New("no description specified for tool definition")
		}

		params := make([]mcp.ToolOption, len(t.Parameters)+1)
		params[0] = mcp.WithDescription(t.Description)
		paramNames := make(map[string]struct{}, len(t.Parameters))

		for j, p := range t.Parameters {
			if p.Name == "" {
				return nil, fmt.Errorf("tool %s defines a parameter without a name", toolName)
			}

			param, err := toMCPParameter(p)
			if err != nil {
				return nil, fmt.Errorf("tool %s parameter %s: %w", toolName, p.Name, err)
			}

			if _, exists := paramNames[p.Name]; exists {
				return nil, fmt.Errorf("tool %s specifies duplicate parameter %q", toolName, p.Name)
			}

			paramNames[p.Name] = struct{}{}

			params[j+1] = param
		}

		tools = append(tools, server.ServerTool{
			Tool:    mcp.NewTool(toolName, params...),
			Handler: toolHandler(toolName, t),
		})
	}

	return tools, nil
}

func toMCPParameter(p config.Parameter) (mcp.ToolOption, error) {
	if p.Description == "" {
		return nil, errors.New("no parameter description specified")
	}

	opts := make([]mcp.PropertyOption, 1, 4)
	opts[0] = mcp.Description(p.Description)

	if p.Required == nil || *p.Required {
		opts = append(opts, mcp.Required())
	}

	if p.MinValue != nil {
		opts = append(opts, mcp.Min(*p.MinValue))
	}

	if p.MaxValue != nil {
		opts = append(opts, mcp.Max(*p.MaxValue))
	}

	switch p.Type {
	case config.ParameterTypeString, "":
		return mcp.WithString(p.Name, opts...), nil
	case config.ParameterTypeNumber:
		return mcp.WithNumber(p.Name, opts...), nil
	case config.ParameterTypeBoolean:
		return mcp.WithBoolean(p.Name, opts...), nil
	default:
		return nil, fmt.Errorf("unsupported parameter type %q provided, expected one of string, number or boolean", p.Type)
	}
}

func toolHandler(toolName string, tool config.ToolDefinition) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c := tool.Container

		env := make([]string, 0, len(c.Env)+len(tool.Parameters))
		for k, v := range c.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}

		paramEnvVars, err := paramsToEnvVars(tool.Parameters, request)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		env = append(env, paramEnvVars...)

		timeout := c.Timeout
		if timeout == 0 {
			timeout = 60 * time.Second
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cli, err := client.New(client.FromEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to use %s tool: create docker client: %w", toolName, err)
		}
		defer cli.Close()

		reader, err := cli.ImagePull(ctx, c.Image, client.ImagePullOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to use %s tool: pull image: %w", toolName, err)
		}

		defer reader.Close()
		_, _ = io.Copy(io.Discard, reader)

		containerConfig := &container.Config{
			Image: c.Image,
			Cmd:   c.Args,
			Env:   env,
		}

		if c.Command != "" {
			containerConfig.Entrypoint = []string{c.Command}
		}

		resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
			Config: containerConfig,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to use %s tool: failed to create container: %w", toolName, err)
		}

		defer func() {
			_, err := cli.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{
				Force:         true,
				RemoveVolumes: true,
			})
			if err != nil {
				slog.Warn(fmt.Sprintf("failed to remove tool container: %s", err))
			}
		}()

		if _, err := cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
			return nil, fmt.Errorf("failed to use %s tool: failed to start container: %w", toolName, err)
		}

		waitResult := cli.ContainerWait(ctx, resp.ID, client.ContainerWaitOptions{
			Condition: container.WaitConditionNotRunning,
		})
		select {
		case err := <-waitResult.Error:
			if err != nil {
				return nil, fmt.Errorf("failed to use %s tool: %w%s", toolName, err, errDetails(ctx, resp.ID, cli))
			}
		case result := <-waitResult.Result:
			if result.StatusCode != 0 {
				return mcp.NewToolResultError(fmt.Sprintf("failed to use %s tool: exited with %d%s", toolName, result.StatusCode, errDetails(ctx, resp.ID, cli))), nil
			}
		}

		out, err := cli.ContainerLogs(ctx, resp.ID, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true})
		if err != nil {
			return nil, fmt.Errorf("failed to read the output of %s tool: %w", toolName, err)
		}

		defer out.Close()

		var stdout, stderr bytes.Buffer

		_, err = stdcopy.StdCopy(&stdout, &stderr, out)
		if err != nil {
			return nil, fmt.Errorf("failed to read the output of %s tool: %w", toolName, err)
		}

		for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
			if line != "" {
				slog.Warn(fmt.Sprintf("%s tool: %s", toolName, line))
			}
		}

		return mcp.NewToolResultText(strings.TrimSpace(stdout.String())), nil
	}
}

func errDetails(ctx context.Context, containerID string, c *client.Client) string {
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

func paramsToEnvVars(paramDefinitions []config.Parameter, request mcp.CallToolRequest) ([]string, error) {
	env := make([]string, len(paramDefinitions))
	args := request.GetArguments()

	for i, p := range paramDefinitions {
		var v string

		switch p.Type {
		case config.ParameterTypeNumber:
			if _, ok := args[p.Name]; ok {
				f, err := request.RequireFloat(p.Name)
				if err != nil {
					return nil, err
				}
				v = strconv.FormatFloat(f, 'f', -1, 64)
			}
		case config.ParameterTypeBoolean:
			if _, ok := args[p.Name]; ok {
				b, err := request.RequireBool(p.Name)
				if err != nil {
					return nil, err
				}
				v = strconv.FormatBool(b)
			}
		default:
			if s, ok := args[p.Name]; ok {
				v = fmt.Sprintf("%v", s)
			}
		}

		if v == "" && (p.Required == nil || *p.Required) {
			return nil, fmt.Errorf("required parameter '%s' was not specified", p.Name)
		}

		name := strings.ToUpper(nameNormalizationRegex.ReplaceAllString(p.Name, "_"))
		env[i] = fmt.Sprintf("PARAMETER_%s=%v", strings.ToUpper(name), v)
	}

	return env, nil
}
