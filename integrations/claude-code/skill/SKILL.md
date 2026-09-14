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
lives in `integrations/cli/AGENTS.md` and is not repeated here. This file is the
detail you load on demand: the argument shapes that are hard to guess.

Every command takes `--help`, and `--help` is authoritative.

## JSON-valued flags

These four take a JSON array, and the field names are **not** interchangeable
with the obvious guesses. Go ignores unknown JSON fields, so on the two edit
commands a payload with wrong keys decodes as an empty old string — and that
is now a hard error naming the edit ordinal, not a silent success with
`changes: 0`. A run where no edit matches is likewise an error, and leaves
the file untouched.

**Replace several blocks in one file** — keys are `old_string` / `new_string`:

```bash
llm-filesystem edit-blocks --path file.go \
  --edits '[{"old_string":"oldName","new_string":"newName"}]'
```

**Edit several parts with modes** — keys are `old_text` / `new_text`, plus a
`mode` of `replace`, `insert_before`, `insert_after` or `delete_line`:

```bash
llm-filesystem edit-multiple-blocks --path file.go \
  --edits '[{"old_text":"a","new_text":"b","mode":"replace"}]'
```

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

# Archives
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

`delete-file` requires `--confirm`, and so does any `batch-file-operations`
entry that destroys something. Without it nothing is touched and the command
exits `2` — which always means nothing happened, so fix the command rather than
retrying it.

Pass `--allowed-dirs /path/a,/path/b` to restrict the tool to specific
directories.
