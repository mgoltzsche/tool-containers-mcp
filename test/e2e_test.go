package test

import (
	//"path/filepath"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestMCP(t *testing.T) {
	mcpClient, err := client.NewStdioMCPClient(
		// TODO: make this work without having to rely on externally built binary
		//"go", nil, "run", filepath.Join("..", "cmd", "tool-containers-mcp", "main.go", "--config=tools.yaml"),
		"../build/dist/tool-containers-mcp", nil, "--config=../tools.yaml",
	)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}

	defer mcpClient.Close()

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "test-client",
		Version: "0.0.1",
	}

	_, err = mcpClient.Initialize(t.Context(), initRequest)
	require.NoError(t, err, "initialize mcp server")

	t.Run("list tools", func(t *testing.T) {
		listResult, err := mcpClient.ListTools(t.Context(), mcp.ListToolsRequest{})
		require.NoError(t, err)
		require.NotNil(t, listResult)

		expectedToolNames := []string{"websearch", "wikipedia"}
		toolNames := make([]string, len(listResult.Tools))
		for i, tool := range listResult.Tools {
			toolNames[i] = tool.Name
		}
		require.Equal(t, expectedToolNames, toolNames, "tool names")
	})
}
