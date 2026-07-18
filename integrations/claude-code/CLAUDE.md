# llm-filesystem routing

`llm-filesystem` complements Claude Code's built-in file tools — it does not
replace them. Route by operation shape:

## Use the native tools for single-file work

For reading, writing, or editing **one** file, use the built-in `Read`,
`Write`, and `Edit` tools. They are faster in-loop and integrate with the
harness's file tracking. Do **not** route single-file operations through
llm-filesystem.

## Use llm-filesystem for batch and specialized work

Reach for the `llm_filesystem_*` MCP tools (or the `llm-filesystem` CLI) when
the operation is inherently multi-file or filesystem-specialized:

| Task | Tool |
|------|------|
| Read 2+ files at once | `llm_filesystem_read_multiple_files` |
| Read a specific line range from a large file | `llm_filesystem_extract_lines` |
| Write 2+ files at once | `llm_filesystem_write_multiple_files` |
| Search file **contents** (ripgrep-fast) | `llm_filesystem_search_code` |
| Find files by name/glob | `llm_filesystem_search_files` |
| Cross-file find-and-replace | `llm_filesystem_search_and_replace` |
| Directory tree overview | `llm_filesystem_get_directory_tree` |
| List a directory with filters/sorting | `llm_filesystem_list_directory` |
| Copy / move / delete, or batch of those | `llm_filesystem_copy_file` / `move_file` / `delete_file` / `batch_file_operations` |
| Create / extract archives | `llm_filesystem_compress_files` / `extract_archive` |

## Output is token-efficient by default

llm-filesystem returns compact TOON with a minimal field set (~90% fewer tokens
than full JSON on a directory listing). If you need every field (mode,
timestamps, permissions), pass `--full` on the CLI or set `LLM_FILESYSTEM_FULL=1`.
For machine parsing, use `--format json`.
