// Command llm-filesystem-mcp is a thin MCP stdio server over the
// llm-filesystem CLI. Each tool call runs the CLI binary; see
// internal/filesystem/mcpserver.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/commands"
	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/mcpserver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "llm-filesystem-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// --version answers from the CLI's shared stamp and exits before the
	// server resolves the CLI binary or reads stdin, so it works even on a
	// machine with only the server installed.
	for _, arg := range os.Args[1:] {
		if arg == "--version" {
			fmt.Println(commands.DisplayVersion())
			return nil
		}
	}

	binary, err := mcpserver.ResolveBinary()
	if err != nil {
		return err
	}
	timeout, err := mcpserver.TimeoutFromEnv(os.Getenv)
	if err != nil {
		return err
	}

	server, err := mcpserver.NewServer(commands.DisplayVersion(),
		mcpserver.Runner{Binary: binary, Timeout: timeout})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "llm-filesystem-mcp started: running %s, %s per call\n", binary, timeout)
	return server.Run(context.Background(), &mcp.StdioTransport{})
}
