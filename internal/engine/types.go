package engine

import (
	"context"

	"github.com/mgoltzsche/tool-containers-mcp/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type HandlerFactory interface {
	NewHandler(ctx context.Context, toolName, imageRef string) (ToolHandler, error)
	Close() error
}

type ToolHandler = func(context.Context, config.Container) (*mcp.CallToolResult, error)
