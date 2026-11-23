---
name: llm-filesystem
description: >-
  Worked invocations for llm-filesystem, the batch and specialized filesystem
  CLI. Load this when you need the exact argument shape for a command — in
  particular the JSON-valued flags on edit-blocks, edit-multiple-blocks,
  write-multiple-files and batch-file-operations, whose field names cannot be
  guessed. Triggers: "edit several blocks", "write multiple files", "batch
  copy/move/delete", "search and replace across files", "extract an archive",
  "read all these files".
---

# llm-filesystem — worked invocations

Routing guidance — *when* to use this instead of your native single-file tools —
lives in [AGENTS.md](https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/integrations/cli/AGENTS.md)
and is not repeated here. This file is the detail you load on demand: the
argument shapes that are hard to guess.

Every command takes `--help`, and `--help` is authoritative.

If `llm-filesystem` is not on `PATH` (this skill was installed on its own, with
`npx skills add`), install the binary first:

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/install.sh | bash && export PATH="$HOME/.local/bin:$PATH"
```

## JSON-valued flags

These four take a JSON array, and the field names are **not** interchangeable
with the obvious guesses. An unknown or missing key is refused before anything
runs: exit `2`, a message naming the entry, and the expected shape. A run where
no edit matches is an error (exit `1`) and leaves the file untouched.

Every edit's old string must match **exactly once** (on the batch commands,
in the content the earlier edits leave). One that matches more often is
refused with its match count and the file is untouched: add surrounding lines
until it is unique, or pass `--replace-all` to change every occurrence.

**Replace several blocks in one file** — keys are `old_string` / `new_string`:

```bash
llm-filesystem edit-blocks --path file.go \
  --edits '[{"old_string":"oldName","new_string":"newName"}]'
```

**Edit several parts with modes** — keys are `old_text` / `new_text`, plus a
`mode` of `replace` (the default), `insert_before`, `insert_after` or
`delete_line`. The three line modes take a 1-based `line_number` instead of
`old_text`:

```bash
llm-filesystem edit-multiple-blocks --path file.go \
  --edits '[{"old_text":"a","new_text":"b","mode":"replace"},
            {"mode":"insert_after","line_number":3,"new_text":"// added"},
            {"mode":"delete_line","line_number":10}]'
```

Edits apply in order, so a later `line_number` counts lines after the earlier
edits. An edit that finds no target is reported per edit (`not_found`,
`line_out_of_range`) in `edit_results`; when some edits applied the command
still exits `0`, so check `unmatched`.

**Write several files** — keys are `path` / `content`:

```bash
llm-filesystem write-multiple-files \
  --files '[{"path":"a.txt","content":"one"},{"path":"b.txt","content":"two"}]'
```

**Batch copy / move / delete** — keys are `operation`, `source`, and
`destination`; the target of a delete goes in `source`:

```bash
llm-filesystem batch-file-operations --confirm \
  --operations '[{"operation":"move","source":"old.txt","destination":"new.txt"}]'
```

`--confirm` is required whenever an entry deletes, or moves or copies onto a
path that already exists.

## Common operations

```bash
# Read several files at once
llm-filesystem read-multiple-files --paths a.go,b.go,c.go

# Search file contents, with surrounding lines
llm-filesystem search-code --path . --pattern "func NewServer" --context 2

# Find files by glob
llm-filesystem search-files --path . --pattern "*_test.go"

# Replace across many files
llm-filesystem search-and-replace --path ./src --pattern "OldName" --replacement "NewName"

# Directory tree to a depth
llm-filesystem get-directory-tree --path . --depth 3

# Archives — the type comes from the --output extension (.zip, .tar.gz, .tgz),
# or pass --archive-format zip|tar.gz|tgz explicitly
llm-filesystem compress-files --paths dist --output release.tar.gz
llm-filesystem extract-archive --archive release.tar.gz --dest ./out

# Delete (confirmation required)
llm-filesystem delete-file --path ./stale.txt --confirm

# Preview a sync without writing anything
llm-filesystem sync-directories --source ./a --dest ./b --dry-run
```

## Picking fields

```bash
llm-filesystem list-directory --path .                      # minimal, TOON
llm-filesystem list-directory --path . --fields name,size   # exactly these
llm-filesystem list-directory --path . --full               # every field
llm-filesystem list-directory --path . --format json        # machine-parseable
```

An unknown `--fields` name exits `2` and lists the valid ones for that command,
so a wrong guess costs one turn and teaches the whole schema.

## Large files

A read over the size budget is **truncated, not refused** — do not route around
the tool because a file is big. The result carries `truncated`, `total_size` and
`next_offset`; continue with `--start-offset <next_offset>`. `--full` returns
everything, which on a large file means holding a multiple of it in memory.

## Safety

`delete-file` requires `--confirm`, and so do `move-file` and `copy-file`
when the destination already exists, and any `batch-file-operations`
entry that destroys something. Without it nothing is touched and the command
exits `2` — which always means nothing happened, so fix the command rather than
retrying it. A move or copy to a new path needs no confirmation, and
`write-file` keeps replacing without it.

Pass `--allowed-dirs /path/a,/path/b` to restrict the tool to specific
directories.
