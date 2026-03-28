package tools

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Tool struct {
	Tool    *mcp.Tool
	Handler mcp.ToolHandler
}

var nameNormalizationRegex = regexp.MustCompile("[^a-zA-Z0-6]+")

func ToMCPServerTools(toolDefs map[string]config.ToolDefinition) ([]Tool, error) {
	tools := make([]Tool, 0, len(toolDefs))
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

		inputSchema := &jsonschema.Schema{
			Type:       "object",
			Properties: map[string]*jsonschema.Schema{},
		}

		paramNames := make(map[string]struct{}, len(t.Parameters))

		for _, paramName := range slices.Sorted(maps.Keys(t.Parameters)) {
			if paramName == "" {
				return nil, fmt.Errorf("tool %s defines a parameter without a name", toolName)
			}

			p := t.Parameters[paramName]

			paramSchema, err := toMCPParameterSchema(p)
			if err != nil {
				return nil, fmt.Errorf("tool %s parameter %s: %w", toolName, paramName, err)
			}

			if p.Required == nil || *p.Required {
				inputSchema.Required = append(inputSchema.Required, paramName)
			}

			if _, exists := paramNames[paramName]; exists {
				return nil, fmt.Errorf("tool %s specifies duplicate parameter %q", toolName, paramName)
			}

			paramNames[paramName] = struct{}{}

			inputSchema.Properties[paramName] = paramSchema
		}

		tools = append(tools, Tool{
			Tool: &mcp.Tool{
				Name:        toolName,
				Description: t.Description,
				InputSchema: inputSchema,
			},
			Handler: toolHandler(toolName, t),
		})
	}

	return tools, nil
}

func toMCPParameterSchema(p config.Parameter) (*jsonschema.Schema, error) {
	if p.Description == "" {
		return nil, errors.New("no parameter description specified")
	}

	schema := &jsonschema.Schema{Description: p.Description}

	if p.MinValue != nil {
		schema.Minimum = p.MinValue
	}

	if p.MaxValue != nil {
		schema.Maximum = p.MaxValue
	}

	switch p.Type {
	case config.ParameterTypeString, "":
		schema.Type = "string"
	case config.ParameterTypeInteger:
		schema.Type = "integer"
	case config.ParameterTypeNumber:
		schema.Type = "number"
	case config.ParameterTypeBoolean:
		schema.Type = "boolean"
	default:
		return nil, fmt.Errorf("unsupported parameter type %q provided, expected one of string, integer, number or boolean", p.Type)
	}

	return schema, nil
}

func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

func toolHandler(toolName string, tool config.ToolDefinition) mcp.ToolHandler {
	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		c := tool.Container

		env := make([]string, 0, len(c.Env)+len(tool.Parameters))
		for k, v := range c.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}

		paramEnvVars, err := paramsToEnvVars(tool.Parameters, request)
		if err != nil {
			return toolError(err.Error()), nil
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
				return toolError(fmt.Sprintf("failed to use %s tool: exited with %d%s", toolName, result.StatusCode, errDetails(ctx, resp.ID, cli))), nil
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

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimSpace(stdout.String())}},
		}, nil
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

func paramsToEnvVars(paramDefinitions map[string]config.Parameter, request *mcp.CallToolRequest) ([]string, error) {
	env := make([]string, len(paramDefinitions))
	args := map[string]any{}
	err := json.Unmarshal(request.Params.Arguments, &args)
	if err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	for i, name := range slices.Sorted(maps.Keys(paramDefinitions)) {
		p := paramDefinitions[name]
		var v string
		arg, ok := args[name]
		if ok {
			switch p.Type {
			case config.ParameterTypeInteger:
				i, ok := arg.(int64)
				if !ok {
					return nil, fmt.Errorf("param %s value of type %T provided, expected integer", name, arg)
				}
				v = strconv.FormatInt(i, 10)
			case config.ParameterTypeNumber:
				f, ok := arg.(float64)
				if !ok {
					return nil, fmt.Errorf("param %s value of type %T provided, expected number", name, arg)
				}
				v = strconv.FormatFloat(f, 'f', -1, 64)
			case config.ParameterTypeBoolean:
				b, ok := arg.(bool)
				if !ok {
					return nil, fmt.Errorf("param %s value of type %T provided, expected number", name, arg)
				}
				v = strconv.FormatBool(b)
			default:
				v = fmt.Sprintf("%v", arg)
			}
		} else {
			if p.Required == nil || *p.Required {
				return nil, fmt.Errorf("required parameter '%s' was not specified", name)
			}
		}

		key := strings.ToUpper(nameNormalizationRegex.ReplaceAllString(name, "_"))
		env[i] = fmt.Sprintf("PARAM_%s=%v", strings.ToUpper(key), v)
	}

	return env, nil
}
