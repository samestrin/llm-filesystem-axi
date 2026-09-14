# llm-filesystem

Fast filesystem operations for an AI agent: reading, writing, editing, searching and managing files. One static Go binary, built to the [AXI](https://axi.md) principles — the command line itself is the interface.

## When to reach for it

Use your built-in single-file tools for reading, writing or editing **one** file. They are faster in-loop and integrate with your harness's own file tracking.

Reach for `llm-filesystem` when the work is inherently multi-file or filesystem-specialized: reading or writing several files at once, searching file *contents*, directory trees, cross-file find-and-replace, archives, or bulk copy/move/delete.

## How to find out what it does

Run it with no arguments. It answers with what it is, where it is, and what is in the working directory — not a usage screen. `llm-filesystem --help` lists every command; each command's `--help` lists its flags.

This file deliberately does **not** list the commands. A list in a document drifts from the binary, and the binary is what actually runs.

## Two behaviours that will surprise you

**Large reads truncate. They do not fail.** A file over the size budget comes back as a leading *prefix*, with `truncated`, `total_size` and `next_offset`, at exit 0. Resume with `--start-offset <next_offset>`. Do not treat a truncated read as the whole file, and do not avoid the tool because a file is large — reading in windows is bounded whatever the size.

**Destructive operations need `--confirm`.** `delete-file` requires it, and so does any `batch-file-operations` entry that destroys something — a delete, or a move or copy onto a path that already exists. Without it nothing is touched and the command exits `2`.

## Reading the output

Output is TOON by default, with a minimal field set — roughly 90% fewer tokens than full JSON on a directory listing. A trailing `help[]` block suggests the next command.

- `--format json` for machine parsing, `--format text` for a human.
- `--fields name,size` to pick exact fields. An unknown name exits `2` and lists the valid ones, so one wrong guess teaches you the schema.
- `--full` for every field. On a large file that also means every byte, which costs a multiple of the file in memory.

Exit codes are `0` success, `1` the tool tried and failed, `2` the invocation was malformed. `2` means **nothing was touched** — fix the command rather than retrying it. Every error is a structured document on stdout carrying a next step.

## If your client cannot run a CLI

An optional MCP server (`llm-filesystem-mcp`) wraps this same binary over stdio, for clients without shell access. It is a subprocess wrapper, not a second implementation, and its tools load into context every session whether used or not. If you can run the CLI, prefer it.
