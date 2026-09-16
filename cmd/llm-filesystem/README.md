# llm-filesystem

The `llm-filesystem` CLI — 28 commands for reading, writing, editing, searching and managing files, built to the [AXI](https://axi.md) principles. The command line is the agent interface; output defaults to token-efficient TOON.

This is the primary binary. The optional [`llm-filesystem-mcp`](../llm-filesystem-mcp/) server wraps it over stdio for MCP clients that cannot run a CLI.

## Build and install

From the repository root:

```bash
make build            # outputs ./build/llm-filesystem
sudo ./install.sh     # builds both binaries, installs to /usr/local/bin
```

Or via `go install`:

```bash
go install github.com/samestrin/llm-filesystem-axi/cmd/llm-filesystem@latest
```

## Usage

```bash
llm-filesystem                                  # where am I, and what is here
llm-filesystem read-file --path ./go.mod
llm-filesystem list-directory --path ./internal
llm-filesystem search-code --path . --pattern "func ReadFile"
llm-filesystem --help                           # full command reference
```

To restrict it to specific directories, pass `--allowed-dirs /Users/me/projects,/tmp`.

## Documentation

Install, output modes, exit codes and safety behaviour are documented in the project [README](../../README.md). The full command reference is [`docs/llm-filesystem-commands.md`](../../docs/llm-filesystem-commands.md), and agent routing guidance lives in [`integrations/cli/AGENTS.md`](../../integrations/cli/AGENTS.md). Both command lists are checked against this binary by tests, so they cannot drift.

## License

MIT — see [LICENSE](../../LICENSE).
