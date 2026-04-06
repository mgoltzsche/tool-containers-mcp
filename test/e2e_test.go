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

var testPort = 10000 + rand.IntN(30000) // nolint:gosec

func TestMCP(t *testing.T) {
	var serverLog bytes.Buffer

	// start server
	// TODO: make this work without having to rely on externally built binary
	// nolint:gosec
	cmd := exec.CommandContext(t.Context(), "../build/dist/tool-containers-mcp", "--config=fake-tools.yaml", fmt.Sprintf("--address=127.0.0.1:%d", testPort), "--pull=never")
	cmd.Stderr = &serverLog
	err := cmd.Start()
	require.NoError(t, err, "start server")
	time.Sleep(300 * time.Millisecond)

	for _, tc := range []struct {
		name      string
		transport mcp.Transport
		init      func(t *testing.T)
	}{
		{
			name: "command/stdin",
			transport: &mcp.CommandTransport{Command: exec.Command(
				// TODO: make this work without having to rely on externally built binary
				//"go", "run", filepath.Join("..", "cmd", "tool-containers-mcp", "main.go", "--config=tools.yaml"),
				"../build/dist/tool-containers-mcp", "--config=fake-tools.yaml",
			)},
			init: func(_ *testing.T) {},
		},
		{
			name:      "HTTP stream",
			transport: &mcp.StreamableClientTransport{Endpoint: fmt.Sprintf("http://127.0.0.1:%d/stream", testPort)},
		},
		{
			name:      "SSE",
			transport: &mcp.SSEClientTransport{Endpoint: fmt.Sprintf("http://127.0.0.1:%d/sse", testPort)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mcpClient := mcp.NewClient(&mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil)
			session, err := mcpClient.Connect(t.Context(), tc.transport, nil)
			require.NoErrorf(t, err, "server log:\n%s", serverLog.String())

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
		})
	}
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
