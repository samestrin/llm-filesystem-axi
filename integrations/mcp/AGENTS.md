# llm-filesystem (MCP)

Filesystem operations exposed as MCP tools by `llm-filesystem-mcp`, which wraps the `llm-filesystem` CLI over stdio.

This is the guidance for the **MCP surface**. Tools take JSON arguments, not command-line flags — if you are running the binary directly, use `integrations/cli/AGENTS.md` instead.

## When to use it

Use your built-in single-file tools for reading, writing or editing **one** file. They are faster in-loop and integrate with your harness's own file tracking.

Reach for these tools when the work is multi-file or filesystem-specialized: several files at once, searching file *contents*, directory trees, cross-file find-and-replace, archives, or bulk copy/move/delete.

## Tools

**Reading**

| Tool | Use it for |
|---|---|
| `llm_filesystem_read_file` | Read one file; `line_start`/`line_count` or `start_offset` narrow it |
| `llm_filesystem_read_multiple_files` | Read several files in one call |
| `llm_filesystem_extract_lines` | Pull specific lines, a range, or lines matching a pattern |

**Writing**

| Tool | Use it for |
|---|---|
| `llm_filesystem_write_file` | Write or create a file |
| `llm_filesystem_write_multiple_files` | Write several files in one call |

**Editing**

| Tool | Use it for |
|---|---|
| `llm_filesystem_edit_blocks` | Apply several replacements to one file |
| `llm_filesystem_search_and_replace` | Regex replace across many files |

**Directories**

| Tool | Use it for |
|---|---|
| `llm_filesystem_list_directory` | List a directory, with filtering and pagination |
| `llm_filesystem_get_directory_tree` | Tree overview to a given depth |

**Search**

| Tool | Use it for |
|---|---|
| `llm_filesystem_search_files` | Find files by name or glob |
| `llm_filesystem_search_code` | Search file contents, ripgrep-fast |

**Moving and deleting**

| Tool | Use it for |
|---|---|
| `llm_filesystem_copy_file` | Copy a file or directory |
| `llm_filesystem_move_file` | Move or rename |
| `llm_filesystem_delete_file` | Delete a file or directory |
| `llm_filesystem_batch_file_operations` | Several copy/move/delete in one call |

**Archives**

| Tool | Use it for |
|---|---|
| `llm_filesystem_compress_files` | Create a zip or tar.gz |
| `llm_filesystem_extract_archive` | Extract a zip or tar.gz |

Single-file read and write are served here too, but your native tools are better at them. These exist for clients that have no other way to touch the filesystem.

## Two behaviours that will surprise you

**Large reads truncate. They do not fail.** A file over the size budget comes back as a leading *prefix*, with `truncated`, `total_size` and `next_offset`, and the call succeeds. Continue by passing `start_offset` set to `next_offset`. Do not treat a truncated read as the whole file. `read_multiple_files` shares one budget across the files requested, allocating it in order, so check `truncated` and `skipped` rather than assuming every file came back whole.

**Deletion is confirmation-gated, and the tool call is the confirmation.** The CLI refuses a delete without `--confirm`; the server supplies it for you, because calling the tool *is* the intent. There is no confirmation argument to pass, and no second step — treat `delete_file` and any `batch_file_operations` containing a delete or an overwrite as immediately destructive.

## Reading the output

Tools return TOON with a minimal field set — measured at 62-94% fewer tokens than full JSON on directory listings, far less on file content. Results carry aggregates such as `total`, and an empty result says so explicitly rather than returning nothing.

A failure comes back as a structured body with `error: true`, a message, and a suggested next step — not as an empty response.
