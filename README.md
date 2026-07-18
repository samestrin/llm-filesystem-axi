# llm-filesystem

> **High-performance filesystem operations for AI agents.**
> *Native Go. Single static binary. A CLI + an MCP server.*

[![Go Version](https://img.shields.io/github/go-mod/go-version/samestrin/llm-filesystem)](https://go.dev/)
[![License](https://img.shields.io/github/license/samestrin/llm-filesystem)](LICENSE)

`llm-filesystem` gives an AI agent fast, safe "hands" on the filesystem: reading, writing, editing, searching, and managing files. It ships as two statically compiled binaries with no runtime dependencies:

- **`llm-filesystem`** — a CLI with 27 commands.
- **`llm-filesystem-mcp`** — an MCP server exposing 17 batch/specialized tools for Claude Code, Claude Desktop, and other MCP clients.

It began as a Go port of the TypeScript [`fast-filesystem-mcp`](https://github.com/efforthye/fast-filesystem-mcp), rewritten for startup speed and single-binary deployment.

## Why Go

LLM agents run tight loops. Paying 85ms for a Node.js process to cold-start just to read a file breaks the flow. A static Go binary starts in single-digit milliseconds.

| Benchmark | Go (llm-filesystem) | TypeScript (Node) | Speedup |
|-----------|---------------------|-------------------|---------|
| **Cold Start** | **5.2ms** | **85.1ms** | **16.5x** |
| MCP Handshake | 40.8ms | 110.4ms | **2.7x** |
| File Read | 49.5ms | 108.2ms | **2.2x** |
| Directory Tree | 50.9ms | 113.7ms | **2.2x** |

> *Benchmarks run on M4 Pro 64GB macOS (arm64), 2025-12-31. See [`benchmarks/`](benchmarks/).*

## Install

```bash
git clone https://github.com/samestrin/llm-filesystem.git
cd llm-filesystem
sudo ./install.sh          # builds both binaries, installs to /usr/local/bin
```

Or build without installing:

```bash
make build                 # outputs to ./build/
```

## MCP setup (Claude Code / Claude Desktop)

Add the server to your MCP client config:

```json
{
  "mcpServers": {
    "llm-filesystem": {
      "command": "/usr/local/bin/llm-filesystem-mcp"
    }
  }
}
```

The MCP server shells out to the `llm-filesystem` CLI, so both binaries must be installed. All tools are namespaced under the `llm_filesystem_` prefix.

For ambient integration — routing rules that tell Claude when to use llm-filesystem (batch/specialized) versus the native Read/Write/Edit tools (single-file), plus an on-demand skill — see [`integrations/claude-code/`](integrations/claude-code/).

To restrict the tool to specific directories:

```json
{
  "mcpServers": {
    "llm-filesystem": {
      "command": "/usr/local/bin/llm-filesystem-mcp",
      "args": ["--allowed-dirs", "/Users/me/projects,/tmp"]
    }
  }
}
```

## CLI usage

```bash
llm-filesystem read-file --path ./go.mod
llm-filesystem list-directory --path ./internal
llm-filesystem search-code --path . --pattern "func ReadFile"
llm-filesystem --help          # full command reference
```

See [`docs/llm-filesystem-commands.md`](docs/llm-filesystem-commands.md) for the full command reference.

## Output modes (AXI)

`llm-filesystem` follows the [AXI](https://axi.md) design principles for agent-ergonomic CLIs. Output defaults to **TOON** (Token-Oriented Object Notation) with a **minimal field set**, which is roughly a 90% token reduction versus full JSON on a directory listing.

| Flag | Effect |
|------|--------|
| *(default)* | Minimal fields, TOON format, with `next_steps` hints |
| `--format json` | Machine-parseable JSON |
| `--format text` | Human-readable text |
| `--full` | All fields instead of the minimal set |
| `--allowed-dirs` | Restrict access to the given directories |

```bash
llm-filesystem list-directory --path .              # minimal TOON (default)
llm-filesystem list-directory --path . --full       # all fields
llm-filesystem list-directory --path . --format json # JSON for scripts
```

Set `LLM_FILESYSTEM_FULL=1` to make full output the default for every command — useful for legacy consumers that expect all fields. `--full --format json` is byte-identical to the pre-AXI `--json` output. The deprecated `--json` and `--min` flags still work.

## Development

```bash
make test          # go test ./...
make test-race     # race detector
make lint          # go vet + gofmt check
make hooks         # enable the pre-commit hook (gofmt + go vet)
```

## License

MIT — see [LICENSE](LICENSE).
