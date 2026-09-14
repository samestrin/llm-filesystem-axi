# llm-filesystem Commands

An agent-ergonomic filesystem CLI with 28 commands for reading, writing, editing, and managing files.

Every count, flag and example in this file is checked against the binary. The command count is enforced in-repo by `TestDocumentedCommandCountMatchesReality` — this number once drifted to 26 against a real 28.

## Installation

```bash
git clone https://github.com/samestrin/llm-filesystem-axi.git
cd llm-filesystem-axi
sudo ./install.sh          # builds both binaries, installs to /usr/local/bin
```

Or directly:

```bash
go install github.com/samestrin/llm-filesystem-axi/cmd/llm-filesystem@latest
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--format` | Output format: `toon` (default, token-efficient), `json`, or `text` |
| `--full` | Emit all fields instead of the minimal set, and all bytes instead of a truncated read |
| `--fields` | Comma-separated item fields to emit instead of the minimal set. Mutually exclusive with `--full`; an unknown name exits `2` and lists the valid ones |
| `--allowed-dirs` | Restrict the tool to the given directories |
| `--json` | *Deprecated* — use `--format json` |
| `--min` | *Deprecated* — compact output |

Running `llm-filesystem` with no arguments prints the current directory and what is in it, not a usage screen. Use `--help` for the full reference.

## Commands

### Reading Files

| Command | Description | Example |
|---------|-------------|---------|
| `read-file` | Read a file | `llm-filesystem read-file --path /tmp/test.txt` |
| `read-multiple-files` | Read multiple files | `llm-filesystem read-multiple-files --paths file1.txt,file2.txt` |
| `extract-lines` | Extract specific lines | `llm-filesystem extract-lines --path file.txt --start 10 --end 20` |

A read larger than the size budget is **truncated, not refused**. The result carries `truncated`, `total_size` and `next_offset`, and exits `0`. Resume with `--start-offset <next_offset>`, or take the whole file with `--full`. `--max-size -1` also disables the budget.

`--full` returns everything, which for a large file means holding a multiple of it in memory while it is read, marshalled and encoded. Above roughly 10 MB the guidance stops suggesting `--full` and offers the windowed resume instead — reading in windows is bounded, whatever the file size.

`read-multiple-files` shares one budget across the files requested, allocating it in order, so the files asked for first come back whole. Anything past the budget is never opened and is counted in `skipped`; `success + failed + skipped` always accounts for every path given.

### Writing Files

| Command | Description | Example |
|---------|-------------|---------|
| `write-file` | Write content to file | `llm-filesystem write-file --path /tmp/out.txt --content "hello"` |
| `write-multiple-files` | Write several files at once | `llm-filesystem write-multiple-files --files '[{"path":"a.txt","content":"x"}]'` |
| `large-write-file` | Write large files with backup | `llm-filesystem large-write-file --path large.txt --content "..."` |
| `get-file-info` | Get file metadata | `llm-filesystem get-file-info --path /tmp/test.txt` |
| `create-directory` | Create a directory | `llm-filesystem create-directory --path /tmp/newdir` |

### Editing Files

| Command | Description | Example |
|---------|-------------|---------|
| `edit-block` | Replace text block | `llm-filesystem edit-block --path file.txt --old "foo" --new "bar"` |
| `edit-blocks` | Multiple replacements | `llm-filesystem edit-blocks --path file.txt --edits '[{"old_string":"a","new_string":"b"}]'` |
| `edit-multiple-blocks` | Edit several parts with modes | `llm-filesystem edit-multiple-blocks --path file.txt --edits '[...]'` |
| `safe-edit` | Edit with backup | `llm-filesystem safe-edit --path file.txt --old "x" --new "y" --backup` |
| `edit-file` | Line-based editing | `llm-filesystem edit-file --path file.txt --operation insert --line 5 --content "new line"` |
| `search-and-replace` | Regex replace across files | `llm-filesystem search-and-replace --path ./src --pattern "old" --replacement "new"` |

### Directory Operations

| Command | Description | Example |
|---------|-------------|---------|
| `list-directory` | List directory contents | `llm-filesystem list-directory --path /tmp` |
| `get-directory-tree` | Get directory tree | `llm-filesystem get-directory-tree --path /tmp --depth 3` |

### Search Operations

| Command | Description | Example |
|---------|-------------|---------|
| `search-files` | Search files by name | `llm-filesystem search-files --path ./src --pattern "*.go"` |
| `search-code` | Search file contents | `llm-filesystem search-code --path ./src --pattern "TODO"` |

A search with no matches says so explicitly and suggests how to widen it, rather than offering to open a result that does not exist.

A search path that does not exist, or that is a file rather than a directory, is a **failure** — not an empty result. "I searched and found nothing" and "that directory is not there" are different answers, and an agent has to be able to tell them apart.

### File Operations

| Command | Description | Example |
|---------|-------------|---------|
| `copy-file` | Copy file/directory | `llm-filesystem copy-file --source a.txt --dest b.txt` |
| `move-file` | Move/rename file | `llm-filesystem move-file --source old.txt --dest new.txt` |
| `delete-file` | Delete file/directory | `llm-filesystem delete-file --path /tmp/old --recursive --confirm` |
| `batch-file-operations` | Batch operations | `llm-filesystem batch-file-operations --operations '[...]' --confirm` |

`delete-file` **requires `--confirm`**. `batch-file-operations` requires it for any operation that destroys something: a `delete`, or a `move`/`copy` whose destination already exists — `os.Rename` and `os.Create` both replace silently, so gating only `delete` would just move the hole one operation across. Without confirmation nothing is touched and the command exits `2`. An operation that only creates something new needs no confirmation.

### Advanced Operations

| Command | Description | Example |
|---------|-------------|---------|
| `get-disk-usage` | Get disk usage stats | `llm-filesystem get-disk-usage --path /tmp` |
| `find-large-files` | Find files over size | `llm-filesystem find-large-files --path ./src --min-size "1MB"` |
| `compress-files` | Create archive | `llm-filesystem compress-files --paths file1.txt --paths file2.txt --output archive.zip` |
| `extract-archive` | Extract archive | `llm-filesystem extract-archive --archive file.zip --dest /tmp/extracted` |
| `sync-directories` | Sync directories | `llm-filesystem sync-directories --source /src --dest /backup --dry-run` |
| `list-allowed-directories` | Show allowed dirs | `llm-filesystem list-allowed-directories` |

`sync-directories --dry-run` reports what it would write and writes nothing. The preview carries `dry_run: true` and a `planned[]` sample in every format, not only in the human text, and it reports the same counts the real run produces.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | The tool failed — the operation was attempted and did not work |
| `2` | Usage error — unknown subcommand, unknown flag, missing required flag, invalid `--format`, or a refused confirmation |

Errors are structured documents on **stdout** in the active format, each carrying a `help[]` block naming a concrete next command. `--format text` keeps its `Error: <msg>` shape on stderr.

## MCP Integration

The MCP wrapper (`llm-filesystem-mcp`) exposes **17 tools** with the `llm_filesystem_` prefix, shelling out to this same CLI:

`batch_file_operations`, `compress_files`, `copy_file`, `delete_file`, `edit_blocks`, `extract_archive`, `extract_lines`, `get_directory_tree`, `list_directory`, `move_file`, `read_file`, `read_multiple_files`, `search_and_replace`, `search_code`, `search_files`, `write_file`, `write_multiple_files`

The server passes `--confirm` itself for deletions, since the tool call is the confirmation.

**Note:** the CLI exposes all 28 commands. For setup, see [`integrations/claude-code/`](../integrations/claude-code/).

## API Parity with fast-filesystem

llm-filesystem began as a drop-in replacement for the fast-filesystem MCP:

- **Output Structure**: uses `items` instead of `entries`, `tree` instead of `root`
- **File Info**: includes `type` ("file"/"directory"), `size_readable`, `permissions`, `extension`, `mime_type`
- **Access Checks**: provides `is_readable` and `is_writable` fields
- **Search Results**: includes `context_before`, `context_after`, `ripgrep_used`, `search_time_ms`
- **Pagination**: supports `continuation_token` for large results
- **Filtering**: respects `.gitignore` patterns in `find-large-files`

See the [Migration Guide](llm-filesystem-migration.md) for details on migrating from fast-filesystem.
