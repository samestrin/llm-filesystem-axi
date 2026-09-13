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
- **Contextual disclosure** as a trailing `help[]` block on TOON output, written by go-axi. `--format json` keeps the `next_steps` payload field instead, since an appended TOON line would stop the output being one JSON document.
- **Structured errors that fail loud**, with exit codes from go-axi's shared constants: `0` success, `1` tool failure, `2` usage error. An unknown subcommand, an unknown flag, a missing required flag and an invalid `--format` each exit `2` with a diagnostic — previously all four exited `1` printing nothing at all.
- **Hardened TOON output** via [go-axi](https://github.com/samestrin/go-axi), replacing the raw codec. File names and contents are sanitized of ANSI escapes, `U+2028`/`U+2029`, lone C1 bytes and invalid UTF-8 before they reach a terminal, and a value the codec would emit as empty output is refused rather than printed as nothing with a zero exit.
- Backward compatible: `--full --format json` is byte-identical to the old
  `--json`; `--json`/`--min` remain as deprecated aliases.
- **Ambient context** (`integrations/claude-code/`): a CLAUDE.md routing snippet
  and an on-demand skill that position llm-filesystem as a complement to Claude's
  native Read/Write/Edit — single-file work stays native, batch/specialized work
  routes here.

### Fixed — failures that reported success

- **An over-budget read exited `0` under `--format json`** while printing an
  error body, so an agent checking the status was told the read had succeeded.
  TOON and text exited `1` for the identical condition. The refusal also wrote
  through `fmt.Println` to the real stdout, bypassing the renderer entirely,
  which is why no test could observe it.
- **`sync-directories` had never copied a file.** It called
  `copyFile(dstPath, path)` against a `copyFile(src, dst)` signature, opening a
  destination that did not exist; the walk discarded the error and reported
  `files_copied: 0` with `success: true`. The directories it did create left the
  destination looking populated.

### Changed — reads truncate instead of refusing

- A read over the size budget returns a content prefix with `truncated`,
  `total_size` and `next_offset`, exits `0`, and carries a `help[]` block naming
  the exact resume command. `next_offset` is the literal `--start-offset` that
  continues the read. The cut prefers a line boundary and never splits a rune,
  and the budget is measured in encoded characters rather than raw bytes.
- `--full` extends from "all fields" to "all fields and all bytes". An explicit
  `--max-size` is more specific and still wins.
- `read-multiple-files` allocates its budget greedily, so the files requested
  first come back whole and anything past the budget is never opened.
- **Breaking:** usage errors (unknown flag, unknown command, missing required
  flag, invalid `--format`) now render as a structured document on **stdout** in
  the active format instead of plain text on stderr. Exit codes are unchanged.
  `--format text` keeps its `Error: <msg>` shape on stderr.
- **Breaking:** `SizeExceededError` and `TotalSizeExceededError` were removed.
  Size is no longer an error class.

### Added — the rest of AXI

- **Content-first landing page.** A bare `llm-filesystem` answers with live data
  — the running binary, the working directory, and what is in it — instead of a
  usage screen. `--help` still prints the full reference.
- **`--fields`**, selecting item fields by name. An unknown field exits `2` and
  the help block enumerates every valid one, so a wrong guess teaches the whole
  schema in a single turn. Mutually exclusive with an explicit `--full`.
- **Recovery guidance on errors.** Every diagnostic carries a next step, and
  result-aware `help[]` means a zero-result search no longer offers to open a
  match that does not exist.
- **Confirm gates.** `delete-file` requires `--confirm`, and
  `batch-file-operations` requires it when any operation is a delete — gating
  one without the other would be theatre. `sync-directories` gains `--dry-run`,
  with `dry_run` and `planned[]` visible in TOON and JSON rather than only in
  the human text.

### Included

Everything that shipped as `llm-filesystem` inside `llm-tools` through mid-2026:

- **CLI (`llm-filesystem`)** — 28 commands for reading, writing, editing,
  directory operations, search, file operations, and archive handling.
- **MCP server (`llm-filesystem-mcp`)** — 17 batch/specialized tools under the
  `llm_filesystem_` prefix, wrapping the CLI over stdio.
- Path sandboxing via `--allowed-dirs`.
- Size-aware reads with a `--max-size` budget (`0` = 70000-char default,
  `-1` = no limit). An over-budget read truncates rather than refusing — see
  the entry below.
- Continuation-token pagination for large listings, reads, and searches.
- Static single-binary builds for macOS, Linux, and Windows.

### Notes

- No behavior change from the final `llm-tools` filesystem code; this release is
  the same engine under a new module path (`github.com/samestrin/llm-filesystem-axi`).
- The full pre-extraction history lives in the `llm-tools` CHANGELOG under the
  `#### llm-filesystem` and `#### llm-filesystem-mcp` sections (first introduced
  in `llm-tools` v1.2.0).
