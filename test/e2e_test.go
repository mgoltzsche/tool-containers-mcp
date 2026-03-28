package test

import (
	//"path/filepath"
	"os/exec"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCP(t *testing.T) {
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command(
		// TODO: make this work without having to rely on externally built binary
		//"go", "run", filepath.Join("..", "cmd", "tool-containers-mcp", "main.go", "--config=tools.yaml"),
		"../build/dist/tool-containers-mcp", "--config=../tools.yaml",
	)}
	session, err := mcpClient.Connect(t.Context(), transport, nil)
	require.NoError(t, err)

	defer session.Close()

	require.NotNil(t, session.InitializeResult().Capabilities.Tools, "MCP server should support tools")

	t.Run("list tools", func(t *testing.T) {
		expectedToolNames := []string{"websearch", "wikipedia"}
		toolNames := make([]string, 0, 10)
		for tool, err := range session.Tools(t.Context(), nil) {
			require.NoError(t, err, "Tools()")
			toolNames = append(toolNames, tool.Name)
		}
		require.Equal(t, expectedToolNames, toolNames, "tool names")
	})

	t.Run("call tool", func(t *testing.T) {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "wikipedia",
			Arguments: map[string]any{
				"query":     "Futurama",
				"rationale": "coz I can",
			},
		})
		require.NoError(t, err)
		require.Equal(t, 1, len(result.Content), "result content length")
		_, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "result entry should be of type mcp.TextContent")
		require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "Leela", "result content")
	})
}
