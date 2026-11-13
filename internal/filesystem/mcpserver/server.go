package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerName is the name the server reports in initialize.
const ServerName = "llm-filesystem-mcp"

const instructions = "llm-filesystem-mcp runs the llm-filesystem CLI: each tool is one CLI command, " +
	"each parameter one of its flags, and the result is the CLI's TOON output. " +
	"A failing command returns isError with the CLI's error and its exit code (1 failed, 2 usage: nothing was attempted). " +
	"Destructive commands act only when you pass confirm: true. The CLI is the primary interface; this server is a fallback for hosts that cannot run it."

// NewServer returns an MCP server with one tool per operational CLI command,
// each running binary under runner's timeout.
func NewServer(version string, runner Runner) (*mcp.Server, error) {
	tools, err := Tools(CommandTree())
	if err != nil {
		return nil, err
	}

	server := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	for _, tool := range tools {
		tool := tool
		server.AddTool(&mcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			var args map[string]any
			if len(req.Params.Arguments) > 0 {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return errorResult(nil, "arguments are not a JSON object: "+err.Error()), nil
				}
			}
			argv, err := tool.Args(args)
			if err != nil {
				// Nothing ran, so this is the CLI's usage class.
				return errorResult(nil, err.Error()+"\nexit_code: 2"), nil
			}
			return runner.Run(ctx, argv), nil
		})
	}
	return server, nil
}
