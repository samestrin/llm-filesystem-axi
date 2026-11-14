# llm-filesystem Commands

An agent-ergonomic filesystem CLI with 29 commands for reading, writing, editing, and managing files.

Every count, flag and example in this file is checked against the binary. The command count is enforced in-repo by `TestDocumentedCommandCountMatchesReality` — this number once drifted to 26 against a real 28.

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash
# downloads the release binaries (CLI and MCP server), verifies their SHA256 checksums, installs to ~/.local/bin (no sudo)
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

`safe-edit --dry-run` and `search-and-replace --dry-run` write nothing, and their output carries `dry_run: true` in every format, not only in the human text. A real run omits the field.

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
| `compress-files` | Create archive (type from `--output` extension, or `--archive-format` with `zip`, `tar.gz` or `tgz`) | `llm-filesystem compress-files --paths file1.txt --paths file2.txt --output archive.zip` |
| `extract-archive` | Extract archive | `llm-filesystem extract-archive --archive file.zip --dest /tmp/extracted` |
| `sync-directories` | Sync directories | `llm-filesystem sync-directories --source /src --dest /backup --dry-run` |
| `list-allowed-directories` | Show allowed dirs | `llm-filesystem list-allowed-directories` |

`sync-directories --dry-run` reports what it would write and writes nothing. The preview carries `dry_run: true` and a `planned[]` sample in every format, not only in the human text, and it reports the same counts the real run produces. `dirs_created` counts the directories it creates, not the directories it walks past: a destination directory that already exists is not counted. A copy that fails is counted in `failed` with the reason in `failures[]`, `success` is `false`, and the run exits `1`.

`compress-files` and `extract-archive` account for everything they were asked to handle. A skipped input or a refused entry is counted in `failed` with the reason in `failures[]`, `success` is `false`, and the run exits `1` — a partial run never reports success. Files that replaced existing ones are counted in `overwritten`. An archive entry whose target in `--dest` is already a symlink, or whose parent directory resolves through a symlink to somewhere outside `--dest`, is refused rather than written through it, and a symlink entry inside an archive is refused as an unsupported entry type. An `--output` that sits inside one of the `--paths` directories is skipped while walking, so an archive cannot contain a partial copy of itself. An `--output` whose extension is not `.zip`, `.tar.gz` or `.tgz`, or an `--archive-format` that contradicts the extension, is a usage error (exit `2`) and writes nothing.

### Project Setup

| Command | Description | Example |
|---------|-------------|---------|
| `setup` | Register llm-filesystem with Claude Code, Codex and OpenCode in the working directory | `llm-filesystem setup --dry-run` |

`setup` merges a SessionStart hook that runs `llm-filesystem` into `.claude/settings.json`, keeping every other key and hook, writes the Agent Skill to `.claude/skills/llm-filesystem/SKILL.md`, merges a SessionStart hook into `.codex/hooks.json` for Codex, and writes `.opencode/plugins/llm-filesystem.js` for OpenCode. The Codex hook runs `llm-filesystem` by name when `PATH` finds this same binary and by its absolute path otherwise; a later run rewrites an existing llm-filesystem hook there to the binary's current path, so a moved binary is repaired, not registered twice. The OpenCode plugin (checked on 1.19.5) names the binary the same way, runs it once when OpenCode loads the plugin, and adds what it prints to the system context through the `experimental.chat.system.transform` hook; it adds no tool, and a failed run adds nothing. setup owns that file: a later run rewrites it when the binary has moved, and a file at that path that setup did not write is refused and left as it is. Codex (checked on 0.166.0, hooks on by default) runs a project's hooks only once the project is trusted and the hook reviewed. It writes only under the working directory and never touches `~/.claude`, `~/.codex` or `~/.config/opencode`: it refuses to run in the home directory, outside `--allowed-dirs`, or through a `.claude`, `.codex` or `.opencode` symlink that leads out of the project. A `settings.json` or `hooks.json` that is not a JSON object, or whose `hooks` or `hooks.SessionStart` has the wrong type, is refused and left as it is. Each path is reported as `written`, `unchanged` or `would-write` (with `--dry-run`, which writes nothing). A second run changes no bytes and reports every file `unchanged`. If a file cannot be written, those before it are still reported `written`, it is reported `failed`, the rest `skipped`, and the run exits `1`.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | The tool failed — the operation was attempted and did not fully work (including a partial run, such as an archive command that skipped some entries) |
| `2` | Usage error — unknown subcommand, unknown flag, missing required flag, invalid `--format`, or a refused confirmation |

Errors are structured documents on **stdout** in the active format, each carrying a `help[]` block naming a concrete next command. `--format text` keeps its `Error: <msg>` shape on stderr.

## MCP

`llm-filesystem-mcp` is a thin MCP stdio server beside the CLI, for hosts that cannot run a command directly. The CLI remains the primary surface; the server is the fallback. Each tool call runs the `llm-filesystem` binary and returns what it printed — nothing is reimplemented, so a tool cannot drift from the command it runs.

- **Tools** are generated from the CLI's command tree: one per operational command, named with the prefix llm_filesystem_ and the command name with dashes turned to underscores (read-file becomes llm_filesystem_read_file). Flags become parameters the same way (--line-start becomes line_start). `completion`, `help`, `setup` and the bare root get no tool. The output switches (`--format`, `--json`, `--min`, `--help`) are never parameters: the server always asks for TOON.
- **Results** are the CLI's TOON output. A non-zero exit is an MCP error result (`isError: true`) carrying the CLI's own error and exit code — 1 failed, 2 usage (nothing was attempted) — never a success. Destructive commands act only when the caller passes `confirm: true`, the same gate as `--confirm` on the CLI.
- **Binary lookup:** `LLM_FILESYSTEM_BINARY` (full path to the CLI), then `PATH`, then the directory of the running server. A failed lookup names every path tried and the variable.
- **Timeout:** each call gets 120 seconds by default. `LLM_FILESYSTEM_MCP_TIMEOUT` sets it (a Go duration such as `90s` or `2m`, or a whole number of seconds). On expiry the child is killed and the error says a write may be partial.
- **Version:** `llm-filesystem-mcp --version` prints the same version as the CLI and exits before reading stdin.

The installer installs and upgrades the server beside the CLI; the README's Install and Usage sections show the client configuration for Claude Code, Codex and OpenCode. For Claude Code's hook and skill setup, see [`integrations/claude-code/`](../integrations/claude-code/).
