# tool-containers-mcp

An MCP server to define tools as short-lived containers.
Each tool call is run within a separate Linux container.

Do you want your AI app to use a tool for which there isn't an MCP server yet?
Do you want to prototype or experiment with custom tools?
Then this MCP server is for you.

# Installation

## Build from source

```sh
go install github.com/mgoltzsche/tool-containers-mcp/cmd/tool-containers-mcp@latest
```

# Usage

## With Claude

1. Define your tools within /etc/tool-containers-mcp/tools.yaml as within the [example](./tools.yaml).
2. Configure your `~/.claude.json` as follows:
```json
{
  "mcpServers": {
    "tool-containers": {
      "command": "tool-containers-mcp",
      "args": [
        "--config=/etc/tool-containers-mcp/tools.yaml"
      ]
    }
  }
}
```

## Via HTTP

MCP can also be served via HTTP by specifying e.g. `--address=:9090`.
There are two HTTP endpoints, one for each transport variant:
* `/sse` - MCP via SSE.
* `/mcp` - MCP via streamable HTTP.

# Development

To build the binary for the host architecture using Go, run:
```sh
make tool-containers-mcp
```

Run the tests:
```sh
make test
```

Run the linter:
```sh
make lint
```

Build a snapshot release (without publishing it):
```sh
make snapshot
```

Run the docker compose example (using [LocalAI](https://github.com/mudler/localai) for inference and [MCP-Bridge](https://github.com/SecretiveShell/MCP-Bridge) as agent):
```sh
make compose
```

Run an example inference query against MCP-Bridge's OpenAI-compatible Chat Completion API (within another terminal):
```sh
curl -fsS http://localhost:9000/v1/chat/completions -H "Content-Type: application/json" -d '{
	"model": "qwen3-4b",
	"messages": [
		{"role": "user", "content": "Which tools do you have access to?"}
	]
}' | jq .
```
