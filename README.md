# llm-filesystem

> **High-performance filesystem operations for AI agents.**
> *Native Go. A single static binary. An agent-ergonomic CLI.*

[![Go Version](https://img.shields.io/github/go-mod/go-version/samestrin/llm-filesystem-axi)](https://go.dev/)
[![TOON output by go-axi](https://img.shields.io/badge/TOON%20output-go--axi-00ADD8)](https://github.com/samestrin/go-axi)
[![AXI](https://img.shields.io/badge/AXI-10%2F10%20principles-5b5bd6)](https://axi.md)
[![License](https://img.shields.io/github/license/samestrin/llm-filesystem-axi)](LICENSE)

`llm-filesystem` gives an AI agent fast, safe "hands" on the filesystem: reading, writing, editing, searching, and managing files. It follows the [AXI](https://axi.md) design principles, which treat the command line itself as the agent interface rather than something to be wrapped in a protocol.

It ships as one binary, `llm-filesystem`: a CLI with 28 commands. There is no MCP server; an agent that can run a command can run this.

It began as a Go port of the TypeScript [`fast-filesystem-mcp`](https://github.com/efforthye/fast-filesystem-mcp), rewritten for startup speed and single-binary deployment.

## Install

```bash
git clone https://github.com/samestrin/llm-filesystem-axi.git
cd llm-filesystem-axi
sudo ./install.sh          # builds the binary, installs to /usr/local/bin
```

Or build without installing:

```bash
make build                 # outputs to ./build/
```

## Usage

```bash
llm-filesystem                 # where am I, and what is here
llm-filesystem read-file --path ./go.mod
llm-filesystem list-directory --path ./internal
llm-filesystem search-code --path . --pattern "func ReadFile"
llm-filesystem --help          # full command reference
```

Run with no arguments and it answers with live data — the binary in use, the working directory, and what is in it — rather than a usage screen.

To restrict it to specific directories, pass `--allowed-dirs /Users/me/projects,/tmp`.

### Telling your agent about it

Append [`integrations/cli/AGENTS.md`](integrations/cli/AGENTS.md) to your project's `AGENTS.md` or `CLAUDE.md`. It lists all 28 commands grouped by task, says when to reach for them instead of built-in single-file tools, and covers the two behaviours that surprise agents — large reads truncate rather than fail, and deletes need `--confirm`:

```bash
cat integrations/cli/AGENTS.md >> ./AGENTS.md
```

That list is checked against the binary in both directions by tests, so a command cannot be added, renamed or removed without the documentation failing CI. Claude Code setup, including an on-demand skill, lives in [`integrations/claude-code/`](integrations/claude-code/).

See [`docs/llm-filesystem-commands.md`](docs/llm-filesystem-commands.md) for the full command reference.

## Output modes (AXI)

`llm-filesystem` follows the [AXI](https://axi.md) design principles for agent-ergonomic CLIs. Output defaults to **TOON** (Token-Oriented Object Notation) with a **minimal field set**.

Measured against `--full --format json`, which is byte-identical to the pre-AXI output for every command this benchmark runs, using tiktoken `o200k_base` ([`benchmarks/tokens.sh`](benchmarks/tokens.sh), full results in [`benchmarks/results-tokens.md`](benchmarks/results-tokens.md)):

| Command | Baseline tokens | Default tokens | Reduction |
|---------|-----------------|----------------|-----------|
| `list-directory` (`/usr/bin`, 523 entries) | 78,833 | 5,285 | **93.3%** |
| `list-directory` (`/usr/share`, 56 entries) | 8,441 | 729 | **91.4%** |
| `get-directory-tree` (`/usr/share`) | 24,453 | 10,714 | **56.2%** |
| `search-code` (this repo's `internal/`) | 24,067 | 15,797 | 34.4% |
| `read-file` (this repo's `go.mod`) | 182 | 171 | 6.0% |

Listings are where it pays, because most of a listing is repeated field names. Commands whose output is mostly file **content** save far less — no schema choice shrinks the bytes of the file you asked for. The reduction also scales with result count: on a directory of only a handful of entries it drops to about 60%, since the fixed part of the document stops being a rounding error. And the tree row needs subdirectories to pay off — the benchmark does not pass `--include-files`, so on a flat directory like `/usr/bin` the tree is a single node and every mode prints the same small document (0%).

TOON output is produced by [go-axi](https://github.com/samestrin/go-axi), which sanitizes it on the way out. File names and file contents are text this tool did not author and prints verbatim, and the raw codec passes ANSI escapes, `U+2028`/`U+2029`, lone C1 bytes and invalid UTF-8 straight through to whatever terminal renders them. go-axi also refuses a value the codec would silently emit as empty output, and supplies the exit codes below.

| Flag | Effect |
|------|--------|
| *(default)* | Minimal fields, TOON format, with a trailing `help[]` block |
| `--format json` | Machine-parseable JSON |
| `--format text` | Human-readable text |
| `--full` | All fields instead of the minimal set, and all bytes instead of a truncated read |
| `--fields name,size` | Exactly these item fields. An unknown name exits `2` and lists the valid ones |
| `--allowed-dirs` | Restrict access to the given directories |
| `--json` / `--min` | Deprecated aliases, still working |

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | The tool failed — the operation was attempted and did not work |
| `2` | Usage error — unknown subcommand, unknown flag, missing required flag, invalid `--format` |

`2` is distinct from `1` on purpose. A typo and a broken tool are different situations, and an agent cannot judge whether a retry is worthwhile if they share a code. Every non-zero exit prints a diagnostic.

```bash
llm-filesystem list-directory --path .              # minimal TOON (default)
llm-filesystem list-directory --path . --full       # all fields
llm-filesystem list-directory --path . --format json # JSON for scripts
```

Set `LLM_FILESYSTEM_FULL=1` to make full output the default for every command — useful for legacy consumers that expect all fields. `--full --format json` is byte-identical to the pre-AXI `--json` output, with one exception: `read-file` and `read-multiple-files` on over-budget files now return content where the old output was a `SizeExceededError` body. The deprecated `--json` and `--min` flags still work.

## Limits and safety

**Big reads truncate rather than refuse.** A file over the size budget returns its leading content plus `truncated`, `total_size` and `next_offset`, and exits `0`. `next_offset` is the exact `--start-offset` that resumes the read; `--full` or `--max-size -1` returns the whole file. A refusal tells an agent nothing about the file — a prefix and a total tell it everything it needs to decide what to do next.

**Anything destructive is gated.** `delete-file` requires `--confirm`. So does any `batch-file-operations` entry that destroys something — a delete, or a move or copy onto a path that already exists, since both replace silently. Gating one without the others would just move the hole. Without confirmation nothing is touched and the command exits `2`.

**A missing path is a failure, not an empty result.** A search against a path that does not exist reports an error rather than zero matches, so a typo cannot read as "the code you are looking for is not here".

**Destructive syncs can be previewed.** `sync-directories --dry-run` reports what it would write, in every output format rather than only in the human text, and writes nothing.

## Why Go

LLM agents run tight loops. Paying 150ms for a Node.js process to cold-start just to read a file breaks the flow. A static Go binary starts in single-digit milliseconds.

| Benchmark | Go (llm-filesystem) | TypeScript (Node) | Speedup |
|-----------|---------------------|-------------------|---------|
| **Cold Start** | **1.9ms** | **150.1ms** | **78x** |
| File Read | 1.9ms | 194.3ms | **103x** |
| Directory Tree | 6.1ms | 176.7ms | **29x** |

> *Benchmarks run on an Intel Xeon W-2295 (36 threads), 141 GB RAM, Debian 13 container on Linux 7.0 (x86_64), Node 24, 2026-10-31. TypeScript is `fast-filesystem-mcp` 3.5.2, reachable only over MCP, so its read and tree figures are a server start, an initialize and one tool call, and include about 22ms for the Python client that drives it. See [`benchmarks/`](benchmarks/).*

## Development

```bash
make test          # go test ./...
make test-race     # race detector
make lint          # go vet + gofmt check
make hooks         # enable the pre-commit hook (gofmt + go vet)
```

## License

MIT — see [LICENSE](LICENSE).
