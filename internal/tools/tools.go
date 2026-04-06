package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/mgoltzsche/tool-containers-mcp/internal/engine"
	"github.com/mgoltzsche/tool-containers-mcp/internal/utils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type HandlerFactoryFunc = func(ctx context.Context, toolName, imageRef string) (engine.ToolHandler, error)

type Tool struct {
	Tool    *mcp.Tool
	Handler mcp.ToolHandler
}

var nameNormalizationRegex = regexp.MustCompile("[^a-zA-Z0-6]+")

func ToMCPServerTools(ctx context.Context, toolDefs map[string]config.ToolDefinition, newHandler HandlerFactoryFunc) ([]Tool, error) {
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

		handler, err := toolHandler(ctx, toolName, t, newHandler)
		if err != nil {
			return nil, err
		}

		tools = append(tools, Tool{
			Tool: &mcp.Tool{
				Name:        toolName,
				Description: t.Description,
				InputSchema: inputSchema,
			},
			Handler: handler,
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

func toolHandler(ctx context.Context, toolName string, tool config.ToolDefinition, newHandler HandlerFactoryFunc) (mcp.ToolHandler, error) {
	handler, err := newHandler(ctx, toolName, tool.Container.Image)
	if err != nil {
		return nil, fmt.Errorf("create %s tool: %w", toolName, err)
	}

	return func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		paramEnvVars, err := paramsToEnvVars(tool.Parameters, request)
		if err != nil {
			return utils.ToolError(err.Error()), nil
		}

		env := make(map[string]string, len(tool.Container.Env)+len(paramEnvVars))
		for k, v := range tool.Container.Env {
			env[k] = v
		}

		for k, v := range paramEnvVars {
			env[k] = v
		}

		container := tool.Container
		container.Env = env

		result, err := handler(ctx, container)
		if err != nil {
			return nil, fmt.Errorf("failed to call %s tool: %w", toolName, err)
		}

		return result, nil
	}, nil
}

func paramsToEnvVars(paramDefinitions map[string]config.Parameter, request *mcp.CallToolRequest) (map[string]string, error) {
	env := make(map[string]string, len(paramDefinitions))
	args := map[string]any{}
	err := json.Unmarshal(request.Params.Arguments, &args)
	if err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	for _, name := range slices.Sorted(maps.Keys(paramDefinitions)) {
		p := paramDefinitions[name]
		var v string
		arg, ok := args[name]
		if ok {
			switch p.Type {
			case config.ParameterTypeInteger:
				i, ok := arg.(float64)
				if !ok {
					return nil, fmt.Errorf("param %s value of type %T provided, expected integer", name, arg)
				}
				v = strconv.FormatInt(int64(i), 10)
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

		name = strings.ToUpper(nameNormalizationRegex.ReplaceAllString(name, "_"))
		env[fmt.Sprintf("PARAM_%s", name)] = v
	}

	return env, nil
}
