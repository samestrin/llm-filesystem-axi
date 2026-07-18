# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - Unreleased

First standalone release. `llm-filesystem` was extracted from the
[`llm-tools`](https://github.com/samestrin/llm-tools) monorepo into its own
repository so it can be released and versioned independently.

The single `1.0.0` version reconciles the previously divergent internal versions
(CLI `1.5.0`, MCP `1.7.0`) into one ldflags-stamped version shared by both binaries.

### Added — AXI compliance

Adopted the [AXI](https://axi.md) design principles for agent-ergonomic CLIs:

- **Token-efficient TOON output by default** (`--format toon|json|text`). TOON is
  ~50% smaller than JSON on its own; combined with minimal schemas it is roughly
  a 90% token reduction on directory listings. The MCP server requests TOON.
- **Minimal default field sets** with a `--full` escape hatch. Listings, trees,
  and searches emit 3-4 fields by default; `--full` (or `LLM_FILESYSTEM_FULL=1`)
  restores every field.
- **Next-step hints** (`next_steps`) appended to list/search output.
- **Structured errors that fail loud**: an unknown `--format` value exits non-zero
  rather than silently defaulting.
- Backward compatible: `--full --format json` is byte-identical to the old
  `--json`; `--json`/`--min` remain as deprecated aliases.

### Included

Everything that shipped as `llm-filesystem` inside `llm-tools` through mid-2026:

- **CLI (`llm-filesystem`)** — 27 commands for reading, writing, editing,
  directory operations, search, file operations, and archive handling.
- **MCP server (`llm-filesystem-mcp`)** — 17 batch/specialized tools under the
  `llm_filesystem_` prefix, wrapping the CLI over stdio.
- Path sandboxing via `--allowed-dirs`.
- Size-aware reads with a structured `SizeExceededError` and a `--max-size`
  escape hatch (`0` = 70000-char default, `-1` = no limit).
- Continuation-token pagination for large listings, reads, and searches.
- Static single-binary builds for macOS, Linux, and Windows.

### Notes

- No behavior change from the final `llm-tools` filesystem code; this release is
  the same engine under a new module path (`github.com/samestrin/llm-filesystem`).
- The full pre-extraction history lives in the `llm-tools` CHANGELOG under the
  `#### llm-filesystem` and `#### llm-filesystem-mcp` sections (first introduced
  in `llm-tools` v1.2.0).
