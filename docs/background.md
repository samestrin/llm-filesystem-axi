# Background: why Go, TOON output, and token cost

This page holds the longer explanations that used to sit in the README: where the tool came from, why it is a Go binary, what the default TOON output saves in tokens, and how the output relates to the older `--json` format. The README links here from its Documentation section.

## Origin

`llm-filesystem` began as a Go port of the TypeScript [`fast-filesystem-mcp`](https://github.com/efforthye/fast-filesystem-mcp), rewritten for startup speed and single-binary deployment. It ships as a CLI, with a thin MCP server (`llm-filesystem-mcp`) beside it for hosts that only speak MCP; an agent that can run a command can run the CLI directly. It follows the [AXI](https://axi.md) design principles, which treat the command line itself as the agent interface rather than something to be wrapped in a protocol.

## Why Go

LLM agents run tight loops. Starting a Node.js process to read one file takes on the order of a hundred milliseconds or more, which breaks the flow. A static Go binary starts in a few milliseconds.

The measured numbers for the current release, with the version, date, machine and method, are in [`benchmarks/results-performance.md`](../benchmarks/results-performance.md), and the README's Performance section copies that table. An earlier run on 2026-10-31 (`fast-filesystem-mcp` 3.5.2, Node 24, the same machine) published speedup ratios for cold start, file read and directory tree. The `v1.0.2` run did not reproduce those ratios, so they are superseded and not repeated here; the `v1.0.2` table is the current measurement.

## Token cost of the default output

Output defaults to TOON (Token-Oriented Object Notation) with a minimal field set.

Measured against `--full --format json`, which is byte-identical to the pre-AXI output for every command this benchmark runs, using tiktoken `o200k_base` ([`benchmarks/tokens.sh`](../benchmarks/tokens.sh), full results in [`benchmarks/results-tokens.md`](../benchmarks/results-tokens.md)):

| Command | Baseline tokens | Default tokens | Reduction |
|---------|-----------------|----------------|-----------|
| `list-directory` (`/usr/bin`, 523 entries) | 78,833 | 5,285 | 93.3% |
| `list-directory` (`/usr/share`, 56 entries) | 8,441 | 729 | 91.4% |
| `get-directory-tree` (`/usr/share`) | 24,453 | 10,714 | 56.2% |
| `search-code` (this repo's `internal/`) | 24,067 | 15,797 | 34.4% |
| `read-file` (this repo's `go.mod`) | 182 | 171 | 6.0% |

Listings are where it pays, because most of a listing is repeated field names. Commands whose output is mostly file content save far less: no schema choice shrinks the bytes of the file you asked for. The reduction also scales with result count: on a directory of only a handful of entries it drops to about 60%, since the fixed part of the document stops being a rounding error. And the tree row needs subdirectories to pay off: the benchmark does not pass `--include-files`, so on a flat directory like `/usr/bin` the tree is a single node and every mode prints the same small document (0%).

## How TOON output is produced

TOON output is produced by [go-axi](https://github.com/samestrin/go-axi), which sanitizes it on the way out. File names and file contents are text this tool did not author and prints verbatim, and the raw codec passes ANSI escapes, `U+2028`/`U+2029`, lone C1 bytes and invalid UTF-8 straight through to whatever terminal renders them. go-axi also refuses a value the codec would silently emit as empty output, and supplies the exit codes.

## Compatibility with the pre-AXI output

`--full --format json` is byte-identical to the pre-AXI `--json` output, with one exception: `read-file` and `read-multiple-files` on over-budget files now return content where the old output was a `SizeExceededError` body. The deprecated `--json` and `--min` flags still work. Set `LLM_FILESYSTEM_FULL=1` to make full output the default for every command, for consumers that expect all fields.

## Why exit code 2 is separate from 1

`2` is distinct from `1` on purpose. A typo and a broken tool are different situations, and an agent cannot judge whether a retry is worthwhile if they share a code. Every non-zero exit prints a diagnostic.
