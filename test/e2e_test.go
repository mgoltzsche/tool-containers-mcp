package test

import (
	//"path/filepath"
	"bytes"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCP_via_stdin(t *testing.T) {
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command(
		// TODO: make this work without having to rely on externally built binary
		//"go", "run", filepath.Join("..", "cmd", "tool-containers-mcp", "main.go", "--config=tools.yaml"),
		"../build/dist/tool-containers-mcp", "--config=fake-tools.yaml",
	)}
	session, err := mcpClient.Connect(t.Context(), transport, nil)
	require.NoError(t, err)

	defer session.Close()

	require.NotNil(t, session.InitializeResult().Capabilities.Tools, "MCP server should support tools")

	t.Run("list tools", func(t *testing.T) {
		expectedToolNames := []string{"fake-tool-1", "fake-tool-2"}
		toolNames := make([]string, 0, 10)
		for tool, err := range session.Tools(t.Context(), nil) {
			require.NoError(t, err, "Tools()")
			toolNames = append(toolNames, tool.Name)
		}
		require.Equal(t, expectedToolNames, toolNames, "tool names")
	})

	t.Run("call tool", func(t *testing.T) {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "fake-tool-1",
			Arguments: map[string]any{
				"string-1":  "string value1",
				"integer-1": 3,
				"number-1":  1.5,
				"boolean-1": true,
			},
		})
		require.NoError(t, err)
		require.Equal(t, 1, len(result.Content), "result content length")
		_, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "result entry should be of type mcp.TextContent")
		require.Equal(t, "fake tool result", result.Content[0].(*mcp.TextContent).Text, "result content")
	})
}

func TestMCP_initialization_error_should_surface_in_client(t *testing.T) {
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command(
		// TODO: make this work without having to rely on externally built binary
		//"go", "run", filepath.Join("..", "cmd", "tool-containers-mcp", "main.go", "--config=tools.yaml"),
		"../build/dist/tool-containers-mcp", "--config=non-existing-file.yaml",
	)}
	_, err := mcpClient.Connect(t.Context(), transport, nil)
	require.ErrorContains(t, err, "tool-containers-mcp: read config: open non-existing-file.yaml: no such file or directory")
}

func TestMCP_via_http(t *testing.T) {
	port := 10000 + rand.IntN(30000)
	cmd := exec.CommandContext(t.Context(), "../build/dist/tool-containers-mcp", "--config=fake-tools.yaml", fmt.Sprintf("--address=127.0.0.1:%d", port), "--pull=never")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Start()
	require.NoError(t, err, "start server")
	time.Sleep(300 * time.Millisecond)

	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: fmt.Sprintf("http://127.0.0.1:%d", port)}
	session, err := mcpClient.Connect(t.Context(), transport, nil)
	require.NoErrorf(t, err, "server logs:\n%s%s", stdout.String(), stderr.String())

	defer session.Close()

	require.NotNil(t, session.InitializeResult().Capabilities.Tools, "MCP server should support tools")

	t.Run("list tools", func(t *testing.T) {
		expectedToolNames := []string{"fake-tool-1", "fake-tool-2"}
		toolNames := make([]string, 0, 10)
		for tool, err := range session.Tools(t.Context(), nil) {
			require.NoError(t, err, "Tools()")
			toolNames = append(toolNames, tool.Name)
		}
		require.Equal(t, expectedToolNames, toolNames, "tool names")
	})

	t.Run("call tool", func(t *testing.T) {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
			Name: "fake-tool-1",
			Arguments: map[string]any{
				"string-1":  "string value1",
				"integer-1": 3,
				"number-1":  1.5,
				"boolean-1": true,
			},
		})
		require.NoError(t, err)
		require.Equal(t, 1, len(result.Content), "result content length")
		_, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok, "result entry should be of type mcp.TextContent")
		require.Equal(t, "fake tool result", result.Content[0].(*mcp.TextContent).Text, "result content")
	})
}
