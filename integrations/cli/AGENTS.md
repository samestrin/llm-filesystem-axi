# llm-filesystem

Fast filesystem operations for an AI agent. One static Go binary, built to the [AXI](https://axi.md) principles — the command line itself is the interface.

## When to use it

Use your built-in single-file tools for reading, writing or editing **one** file. They are faster in-loop and integrate with your harness's own file tracking.

Reach for `llm-filesystem` when the work is multi-file or filesystem-specialized: several files at once, searching file *contents*, directory trees, cross-file find-and-replace, archives, or bulk copy/move/delete.

## Commands

Every command takes `--help`. Run `llm-filesystem` with no arguments to see the working directory and what is in it.

**Reading**

| Command | Use it for |
|---|---|
| `read-file` | Read one file, optionally a line range or byte window |
| `read-multiple-files` | Read several files in one call |
| `extract-lines` | Pull specific lines, a range, or lines matching a pattern |

**Writing**

| Command | Use it for |
|---|---|
| `write-file` | Write or create a file |
| `write-multiple-files` | Write several files in one call, creating parent directories |
| `large-write-file` | Write a large file with backup and verification |
| `create-directory` | Create a directory |
| `get-file-info` | Size, mode, timestamps and type for one path |

**Editing**

| Command | Use it for |
|---|---|
| `edit-block` | Replace one block of text |
| `edit-blocks` | Apply several replacements to one file |
| `edit-multiple-blocks` | Edit several parts of a file with different modes |
| `edit-file` | Line-based insert, replace or delete |
| `safe-edit` | Edit with a backup, and `--dry-run` to preview |
| `search-and-replace` | Regex replace across many files |

**Directories**

| Command | Use it for |
|---|---|
| `list-directory` | List a directory, with filtering, sorting and pagination |
| `get-directory-tree` | Tree overview to a given `--depth` |

**Search**

| Command | Use it for |
|---|---|
| `search-files` | Find files by name or glob |
| `search-code` | Search file contents by pattern, with optional context lines |

**Moving and deleting**

| Command | Use it for |
|---|---|
| `copy-file` | Copy a file or directory |
| `move-file` | Move or rename |
| `delete-file` | Delete — requires `--confirm` |
| `batch-file-operations` | Several copy/move/delete in one call |

**Disk and archives**

| Command | Use it for |
|---|---|
| `get-disk-usage` | Total size, file and directory counts for a path |
| `find-large-files` | Files at or above a `--min-size` in bytes |
| `compress-files` | Create a zip or tar.gz |
| `extract-archive` | Extract a zip or tar.gz |
| `sync-directories` | Copy a tree to a destination, with `--dry-run` to preview |
| `list-allowed-directories` | Show the sandbox, if one is set |

## Two behaviours that will surprise you

**Large reads truncate. They do not fail.** A file over the size budget comes back as a leading *prefix*, with `truncated`, `total_size` and `next_offset`, at exit 0. Resume with `--start-offset <next_offset>`. Do not treat a truncated read as the whole file, and do not avoid the tool because a file is large — reading in windows is bounded whatever the size.

**Destructive operations need `--confirm`.** `delete-file` requires it, and so does any `batch-file-operations` entry that destroys something: a delete, or a move or copy onto a path that already exists. Without it nothing is touched and the command exits `2`.

## Reading the output

TOON by default with a minimal field set — measured at 62-94% fewer tokens than full JSON on directory listings, far less on file content. A trailing `help[]` block suggests the next command.

- `--format json` for machine parsing, `--format text` for a human.
- `--fields name,size` to pick exact fields. An unknown name exits `2` and lists the valid ones, so one wrong guess teaches you the schema.
- `--full` for every field — and on a large file, every byte, which costs a multiple of the file in memory.
- `--allowed-dirs a,b` to restrict it to given directories.

Exit codes: `0` success, `1` the tool tried and failed, `2` the invocation was malformed. **`2` means nothing was touched** — fix the command rather than retrying it. Every error is a structured document on stdout carrying a next step.

## If your client cannot run a CLI

An optional MCP server wraps this same binary over stdio. Its guidance is in `integrations/mcp/AGENTS.md`, and the tool names differ from the commands above. If you can run the CLI, prefer it.
