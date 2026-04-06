package utils

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func ToolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}
