---
name: llm-filesystem
description: >-
  Fast batch and specialized filesystem operations that complement Claude's
  native Read/Write/Edit. Use for multi-file reads/writes, ripgrep content
  search, directory trees, cross-file find-and-replace, and archive
  create/extract. Do NOT use for single-file read/write/edit — the native tools
  are faster for those. Triggers: "read all these files", "search the codebase
  for", "find files matching", "replace X across the repo", "show the directory
  tree", "zip/unzip".
---

# llm-filesystem

`llm-filesystem` is a native Go CLI + MCP server for filesystem work that is
awkward or slow with single-file tools. It **complements** the built-in
`Read`/`Write`/`Edit` tools — reach for it only when the task is multi-file or
filesystem-specialized.

## When to use this vs native tools

- **Single file** (read/write/edit one path): use native `Read`/`Write`/`Edit`.
- **Multiple files, search, trees, archives**: use llm-filesystem (below).

## Common operations

**Read several files in one call:**
```bash
llm-filesystem read-multiple-files --files a.go,b.go,c.go
```

**Search file contents (ripgrep speed):**
```bash
llm-filesystem search-code --path . --pattern "func NewServer" --context 2
```

**Find files by glob:**
```bash
llm-filesystem search-files --path . --pattern "*_test.go"
```

**Replace across many files:**
```bash
llm-filesystem search-and-replace --path ./src --pattern "OldName" --replacement "NewName"
```

**Directory tree:**
```bash
llm-filesystem get-directory-tree --path . --depth 3
```

**Archives:**
```bash
llm-filesystem compress-files --paths dist --output release.tar.gz
llm-filesystem extract-archive --archive release.tar.gz --destination ./out
```

## Output format

Output defaults to compact TOON with a minimal field set. Add `--full` for all
fields, `--format json` for machine parsing, or `--format text` for a readable
view. Every command lists its flags with `--help`.

## Safety

Pass `--allowed-dirs /path/a,/path/b` (or configure it on the MCP server) to
restrict the tool to specific directories.
