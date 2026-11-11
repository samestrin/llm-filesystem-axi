# llm-filesystem

`llm-filesystem` is a single Go binary that gives an AI agent, or you, commands to read, write, edit, search and manage local files, with output built for agents to parse.

<a href="https://github.com/samestrin/llm-filesystem-axi/actions/workflows/test.yml"><img src="https://github.com/samestrin/llm-filesystem-axi/actions/workflows/test.yml/badge.svg" alt="CI"></a> <a href="https://github.com/samestrin/llm-filesystem-axi/releases/latest"><img src="https://img.shields.io/github/v/release/samestrin/llm-filesystem-axi" alt="Latest release"></a> <a href="LICENSE"><img src="https://img.shields.io/github/license/samestrin/llm-filesystem-axi" alt="License"></a>

## Demo

<img src="docs/demo/llm-filesystem.gif" alt="A terminal session running llm-filesystem: the version check, a file read, a code search and the default TOON output">

The GIF is rendered from [`docs/demo/llm-filesystem.tape`](docs/demo/llm-filesystem.tape); [`docs/demo/README.md`](docs/demo/README.md) gives the command that renders it again.

## Why

- An agent that reads a large file needs to know what it missed. A read over the size budget returns the leading content plus `truncated`, `total_size` and `next_offset`, and exits `0`, so the agent can continue from the exact offset instead of getting a refusal.
- Listings in full JSON spend most of their tokens on repeated field names. Output defaults to TOON with a minimal field set, which removes most of the tokens from a large directory listing (measured in [`docs/background.md`](docs/background.md)).
- A typo and a broken tool need different responses. Usage errors exit `2` and tool failures exit `1`, and every non-zero exit prints a diagnostic, so an agent can tell whether a retry is worthwhile.
- Deleting by mistake is hard to undo. `delete-file` and any destructive `batch-file-operations` entry refuse to run without `--confirm`.
- Agents run in many environments. It ships as one static binary with no runtime and no server: an agent that can run a command can use it.

## Install

### Release binary with the installer

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash
```

The installer downloads the release binary for your platform (Linux or macOS, amd64 or arm64) and `SHA256SUMS.txt`, verifies the binary against it, runs it with `--version`, and installs it to `~/.local/bin`. On a failed download, a checksum mismatch, or a binary that does not run on your machine it installs nothing and exits non-zero. Set `INSTALL_DIR` to install elsewhere, or `LLM_FILESYSTEM_VERSION` to pin a release tag.

### Release binary by hand

Each release publishes `llm-filesystem-linux-amd64`, `llm-filesystem-linux-arm64`, `llm-filesystem-darwin-amd64`, `llm-filesystem-darwin-arm64`, `llm-filesystem-windows-amd64.exe` and `SHA256SUMS.txt`. Download your platform's binary and the checksum file, check the one line for your asset, then install it:

```bash
curl -fsSLO https://github.com/samestrin/llm-filesystem-axi/releases/download/v1.0.2/llm-filesystem-linux-amd64
curl -fsSLO https://github.com/samestrin/llm-filesystem-axi/releases/download/v1.0.2/SHA256SUMS.txt
grep ' llm-filesystem-linux-amd64$' SHA256SUMS.txt | sha256sum -c -    # macOS: shasum -a 256 -c -
mkdir -p ~/.local/bin && install -m 755 llm-filesystem-linux-amd64 ~/.local/bin/llm-filesystem
```

### go install

Requires Go 1.24 or later.

```bash
go install github.com/samestrin/llm-filesystem-axi/cmd/llm-filesystem@v1.0.2
```

This installs to `$(go env GOPATH)/bin`. A binary built this way prints `1.0.0` for `--version`, because only the release builds and `make build` stamp the version; the code is the `v1.0.2` code.

### From source

```bash
git clone https://github.com/samestrin/llm-filesystem-axi.git
cd llm-filesystem-axi
make build                 # outputs to ./build/llm-filesystem
make install               # copies it to /usr/local/bin (may require sudo)
```

### Agent paths

With Claude Code, run one command in your project:

```bash
llm-filesystem setup
```

It merges a SessionStart hook into `.claude/settings.json` (keeping every other key and hook) and writes the Agent Skill to `.claude/skills/llm-filesystem/SKILL.md`. It is idempotent, never prompts, and supports `--dry-run`. See [`integrations/claude-code/`](integrations/claude-code/).

To install only the skill with the skills CLI:

```bash
npx skills add samestrin/llm-filesystem-axi --skill llm-filesystem
```

For any other agent, append the routing guidance to your project's `AGENTS.md` or `CLAUDE.md`. It lists all 29 commands grouped by task, says when to reach for them instead of built-in single-file tools, and covers the two behaviours that surprise agents: large reads truncate rather than fail, and deletes need `--confirm`.

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/integrations/cli/AGENTS.md >> ./AGENTS.md
```

That list is checked against the binary in both directions by tests, so a command cannot be added, renamed or removed without the documentation failing CI.

### Upgrade

Re-run the installer. It replaces the existing binary and prints the version installed and the one it replaced:

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | LLM_FILESYSTEM_VERSION=v1.0.2 bash   # pin a release
llm-filesystem --version
```

If the installer warns that some path shadows the new install, an older `llm-filesystem` sits earlier on your `PATH` and still runs instead of the one just installed; a pre-1.0 copy rejects `--confirm` and fails every delete. Remove the older copy, or put `~/.local/bin` (or your `INSTALL_DIR`) earlier on your `PATH`. The installer also warns about a leftover binary from the removed MCP server, which is no longer part of this tool and is no longer updated; it names the path so you can remove it.

With `go install`, run the same command again with `@latest` or a newer tag.

### Uninstall

Remove the binary from where it was installed:

```bash
rm ~/.local/bin/llm-filesystem                 # installer default; or "$INSTALL_DIR/llm-filesystem"
rm "$(go env GOPATH)/bin/llm-filesystem"       # go install
sudo rm /usr/local/bin/llm-filesystem          # make install
```

`setup` has no undo command. To remove what it wrote in a project, delete `.claude/skills/llm-filesystem/` and remove the SessionStart hook whose command is `llm-filesystem` from `.claude/settings.json`.

## Quick start

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash
llm-filesystem --version
cd ~/hello                                   # any project directory
llm-filesystem
llm-filesystem read-file --path go.mod
```

Output, trimmed to the lines that matter:

```text
Downloading llm-filesystem-linux-amd64 (latest) from https://github.com/samestrin/llm-filesystem-axi/releases/latest/download
Checksum verified.

1.0.2

about: "Fast filesystem operations for an AI agent: read, write, edit, search and manage files."
bin: ~/.local/bin/llm-filesystem
cwd: ~/hello
home: ~
items[3]{name,size_readable,type}:
  README.md,8 B,file
  go.mod,34 B,file
  src,4.1 kB,directory
total: 3
help[3]: "List another directory: llm-filesystem list-directory --path <path>","Search file contents: llm-filesystem search-code --path . --pattern <text>","Full command reference: llm-filesystem --help"

content: "module example.com/hello\n\ngo 1.24\n"
lines: 3
path: go.mod
size: 34
```

Run with no arguments, it answers with live data (the binary in use, the working directory, and what is in it) rather than a usage screen.

## Usage

The binary has 29 commands. `llm-filesystem --help` lists them and `llm-filesystem <command> --help` gives each one's flags.

| Command | What it does |
|---------|--------------|
| `read-file` | Read a file |
| `read-multiple-files` | Read multiple files simultaneously |
| `extract-lines` | Extract specific lines from a file |
| `write-file` | Write content to a file |
| `write-multiple-files` | Write multiple files in a single operation |
| `large-write-file` | Write large files with backup and verification |
| `edit-block` | Replace a block of text in a file |
| `edit-blocks` | Apply multiple edits to a file |
| `edit-file` | Line-based file editing |
| `edit-multiple-blocks` | Edit multiple parts of a file with different modes |
| `safe-edit` | Safe edit with backup and dry-run support |
| `search-and-replace` | Search and replace across files |
| `list-directory` | List directory contents |
| `get-directory-tree` | Get directory tree structure |
| `search-files` | Search for files by name |
| `search-code` | Search for patterns in file contents |
| `find-large-files` | Find large files |
| `get-file-info` | Get detailed file information |
| `get-disk-usage` | Get disk usage for a path |
| `create-directory` | Create a directory |
| `copy-file` | Copy a file or directory |
| `move-file` | Move or rename a file or directory |
| `delete-file` | Delete a file or directory |
| `batch-file-operations` | Perform batch file operations |
| `sync-directories` | Synchronize two directories |
| `compress-files` | Compress files into an archive |
| `extract-archive` | Extract an archive |
| `list-allowed-directories` | List allowed directories |
| `setup` | Register llm-filesystem with Claude Code in this project |

One example per main command:

```bash
llm-filesystem read-file --path go.mod --line-start 1 --line-count 2
llm-filesystem list-directory --path . --sort-by size
llm-filesystem get-directory-tree --path . --depth 2 --include-files
llm-filesystem search-code --path . --pattern "func main" --context 1
llm-filesystem search-files --path . --pattern "*.go"
llm-filesystem write-file --path notes.txt --content "first line"
llm-filesystem edit-block --path notes.txt --old "first" --new "second"
llm-filesystem delete-file --path notes.txt --confirm
llm-filesystem sync-directories --source src --dest backup --dry-run
llm-filesystem setup --dry-run
```

To restrict every command to specific directories, pass `--allowed-dirs`, for example `--allowed-dirs /Users/me/projects,/tmp`. A path outside them fails with exit `1`.

See [`docs/llm-filesystem-commands.md`](docs/llm-filesystem-commands.md) for the full command reference.

## Configuration

| Name | Default | Meaning |
|------|---------|---------|
| `LLM_FILESYSTEM_FULL` | unset | `1`, `true`, `yes` or `on` makes full output the default for every command, as if `--full` were passed. An explicit `--full=false` still wins. |
| `INSTALL_DIR` | `$HOME/.local/bin` | Installer only: the directory the binary is installed to. |
| `LLM_FILESYSTEM_VERSION` | `latest` | Installer only: the release tag to install, for example `v1.0.2`. |
| `LLM_FILESYSTEM_DOWNLOAD_TIMEOUT` | `300` | Installer only: seconds before a download is abandoned. |
| `LLM_FILESYSTEM_BASE_URL` | unset | Installer only: the URL the assets are fetched from, overriding the version. Used by the installer's tests. |

## Output and exit codes

Output defaults to TOON (Token-Oriented Object Notation) with a minimal field set and a trailing `help[]` block of next steps. [`docs/background.md`](docs/background.md) has the token measurements and how the output relates to the older `--json` format.

| Flag | Effect |
|------|--------|
| (default) | `--format toon`: minimal fields, TOON format, with a trailing `help[]` block |
| `--format json` | Machine-parseable JSON |
| `--format text` | Human-readable text |
| `--full` | All fields instead of the minimal set, and all bytes instead of a truncated read |
| `--fields name,size` | Exactly these item fields. An unknown name exits `2` and lists the valid ones |
| `--json` | Deprecated: use `--format json` |
| `--min` | Deprecated: minimal output is the default |

```bash
llm-filesystem list-directory --path .                 # minimal TOON (default)
llm-filesystem list-directory --path . --full          # all fields
llm-filesystem list-directory --path . --format json   # JSON for scripts
```

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | The tool failed: the operation was attempted and did not fully work. This includes a partial run: `compress-files`, `extract-archive` and `sync-directories` exit `1` when any input, archive entry or file was skipped, with the failures named in the output |
| `2` | Usage error: unknown subcommand, unknown flag, missing required flag, invalid `--format`, or a destructive operation without `--confirm` |

With the default TOON and with `--format json`, errors are written to stdout as a structured document; with `--format text` they go to stderr.

## Performance

`llm-filesystem` compared with [`fast-filesystem-mcp`](https://www.npmjs.com/package/fast-filesystem-mcp). The stamp lines and the table are copied from [`benchmarks/results-performance.md`](benchmarks/results-performance.md), which [`benchmarks/benchmark.sh`](benchmarks/benchmark.sh) generates; [`benchmarks/README.md`](benchmarks/README.md) explains how to run it.

- **Tools:** [llm-filesystem](https://github.com/samestrin/llm-filesystem-axi) `1.0.2`, a Go CLI; [fast-filesystem-mcp](https://www.npmjs.com/package/fast-filesystem-mcp) `3.5.2` from npm, an MCP server, on Node.js v24.15.0
- **Date:** 2026-11-11 (UTC)
- **Machine:** Apple M4 Pro, 14 threads, 64.0 GiB RAM, macOS 26.2, Darwin 25.2.0 (arm64)
- **Directory:** 103 files (`BENCH_DIR`, with a `package.json` at its root)
- **Method:** hyperfine, warmup 3, runs 10, wall-clock per process invocation (hyperfine 1.20.0); each command runs through `sh`, whose own start hyperfine measures and subtracts; each cell is the mean ± the standard deviation. llm-filesystem is a CLI, so each of its figures is one invocation of the binary. fast-filesystem-mcp is reached over MCP, so its handshake, read and tree figures are each one run of `mcp_client.py` (Python 3.14.5), which starts the server, sends `initialize` and one request, and waits for both answers: each includes a server start and the Python client's own start, measured by this run at 23.5 ms ± 0.5 ms. Cold start is `llm-filesystem --version`, and `fast-filesystem-mcp` with stdin closed, which prints its start line and exits at the end of input; each is piped to `head -1` and timed until the pipeline exits.

| Test | llm-filesystem `1.0.2` | fast-filesystem-mcp `3.5.2` |
|---|---:|---:|
| Cold start | 2.63 ms ± 0.09 ms | 72.4 ms ± 3.7 ms |
| MCP handshake (`initialize` + `tools/list`) | not applicable: a CLI has no MCP server | 108.0 ms ± 1.9 ms |
| File read (`package.json`) | 2.61 ms ± 0.09 ms | 111.2 ms ± 3.9 ms |
| Directory tree (depth 3, files included) | 3.32 ms ± 0.19 ms | 121.7 ms ± 1.8 ms |

- **Tree entries returned below the root:** llm-filesystem 58, fast-filesystem-mcp 57

The test directory is a copy of this repository's source plus a `package.json`, with no node_modules, so both trees cover the same files, as the entry counts above show (fast-filesystem-mcp's default excludes leave out one more name).

Raw results: [`results-coldstart.json`](benchmarks/results-coldstart.json), [`results-handshake.json`](benchmarks/results-handshake.json), [`results-read.json`](benchmarks/results-read.json), [`results-tree.json`](benchmarks/results-tree.json), and the stamp in [`results-meta.json`](benchmarks/results-meta.json).

## Limits

Big reads truncate rather than refuse. A file over the size budget (70,000 characters by default) returns its leading content plus `truncated`, `total_size` and `next_offset`, and exits `0`. `next_offset` is the exact `--start-offset` that resumes the read; `--full` or `--max-size -1` returns the whole file.

Deletes are gated. `delete-file` requires `--confirm`. So does any `batch-file-operations` entry that destroys something: a delete, or a move or copy onto a path that already exists. Without confirmation nothing is touched and the command exits `2`.

A missing path is a failure, not an empty result. A search against a path that does not exist reports an error rather than zero matches, so a typo cannot read as "the code you are looking for is not here".

Destructive syncs can be previewed. `sync-directories --dry-run` reports what it would write, in every output format, and writes nothing. The preview reports the same counts the real run does: `dirs_created` is directories it creates, not directories it walks, so a directory already present in the destination is not counted.

Known gaps against the [AXI](https://axi.md) principles, the same ones the catalog entry in [`docs/axi-catalog-entry.yaml`](docs/axi-catalog-entry.yaml) states:

- Principle 7: `setup` registers with Claude Code only, as one SessionStart hook in the project's .claude/settings.json plus the skill; there is no hook for other agents and no session-end capture.
- Principle 6: once --format text is parsed, usage and tool errors go to stderr and stdout is empty; a flag error listed before --format (as in `read-file --bogus --format text`) is hit before the format flag is parsed and renders in the default toon on stdout, which is also where the json format puts errors.
- Principles 3 and 4: search-code and search-files stop at --max-results (default 1000) and report that cap as total_matches or total, with no truncation marker or hint.
- write-file, move-file and copy-file replace an existing destination without --confirm; delete-file requires it, and so does any batch-file-operations entry that destroys something: a delete, or a move or copy onto a path that already exists.

## Documentation

- [`docs/llm-filesystem-commands.md`](docs/llm-filesystem-commands.md): every command and its flags.
- [`docs/background.md`](docs/background.md): why Go, the token cost of the default output, how TOON output is produced, and compatibility with the pre-AXI output.
- [`docs/axi-audit.md`](docs/axi-audit.md): the audit against the AXI principles.
- [`docs/axi-catalog-entry.yaml`](docs/axi-catalog-entry.yaml): the draft AXI catalog entry and its known gaps.
- [`docs/demo/README.md`](docs/demo/README.md): how the demo GIF is rendered.
- [`benchmarks/README.md`](benchmarks/README.md): the performance and token benchmarks.
- [`integrations/claude-code/`](integrations/claude-code/) and [`integrations/cli/AGENTS.md`](integrations/cli/AGENTS.md): agent setup and routing guidance.

Support is best-effort. Report bugs or request features by opening an issue at [github.com/samestrin/llm-filesystem-axi/issues](https://github.com/samestrin/llm-filesystem-axi/issues). There is no SLA and no guaranteed response time.

## Development

```bash
make build         # the binary into ./build/; the end-to-end tests need it
make test          # go test ./...
make test-race     # race detector
make lint          # go vet + gofmt check
make hooks         # enable the pre-commit hook (gofmt + go vet)
```

## Changelog

See [`CHANGELOG.md`](CHANGELOG.md).

## License

MIT, see [`LICENSE`](LICENSE).
