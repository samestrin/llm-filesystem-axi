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

- **Token-efficient TOON output by default** (`--format toon|json|text`). Measured
  with `benchmarks/tokens.sh` against `--full --format json`: a directory listing
  costs **58-94% fewer tokens** (92-94% on directories of 41-921 entries, about 60%
  on a handful), a tree 37-57% on directories with subdirectories to expand, and
  content-dominated commands such as `search-code` and `read-file` 3-45% —
  `read-file` is 3.4% on every target, while `search-code` runs 19.4% on this
  repository's `internal/` up to 45.3% on `/usr/share` — since no schema choice
  shrinks the bytes of a file you asked to read. TOON encoding alone accounts for
  0-46% of that; on large listings most of the saving comes from the minimal field
  set rather than the format.
  The MCP server requests TOON.
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

### Fixed — found by review, before release

Two independent reviewers read the full diff. Everything below was reproduced against the built binary.

- **`sync-directories` could destroy destination data and report success.** `copyFile` called `os.Create`, which truncates, before copying a byte; a copy failing part-way — EISDIR, ENOSPC, a permission change — left the destination empty and the walk discarded the error. Copies are now written to a temporary file and renamed into place, so a destination is either replaced completely or untouched. Failures are counted and reported, and a destination that cannot be created no longer has its subtree attempted.
- **`batch-file-operations` let a `move` or `copy` overwrite a file with no confirmation.** The gate matched only `delete`, while `os.Rename` and `os.Create` both replace silently. Overwriting operations now require `--confirm`; ones that create something new do not.
- **`search-code` and `search-files` reported a missing path as an empty result** at exit 0, because the path was normalized and sandbox-checked but never stat-ed. A mistyped path is now a failure, not an absence.
- **`--fields` on an unsupported command performed the operation and then reported a usage error.** The check ran at render time, after the command body; `delete-file --confirm --fields path` deleted the file and exited 2, while exit 2 promises nothing was touched. It now runs before the command.
- **A binary file could produce an endless resume loop.** The rune-boundary backoff stripped every byte of an invalid-UTF-8 prefix, returning empty content with `next_offset` equal to the offset given. The backoff is now bounded.
- **The advertised resume read the whole file.** `--start-offset` reached the reader with no byte limit and called `os.ReadFile`: a 300 MB file peaked at 609 MB resident. It now seeks and streams — 8 MB.
- **`read-multiple-files` counters did not account for every file**, so `success + failed` could be less than the number of files requested, and its budget was spent in raw bytes while measured in encoded characters, overrunning the cap several times over on escape-heavy content.
- **Tool failures carried no `help[]`**, and the landing view exited non-zero when the working directory was unknowable. Both are acceptance criteria this release claims.
- **25 `OutputError` call sites were missing a `return`**, which became a nil-pointer panic once a search could fail.

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
