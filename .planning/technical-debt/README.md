# Technical Debt

Open items are unchecked. Resolved items are marked `[x]`. Deferred items are marked `[/]`.

Severity is about consequence, not effort. **HIGH** means a caller can be misled about what happened; **MEDIUM** means a real defect with a narrow blast radius or an internal-only one; **LOW** means cost without correctness risk.

## Stats

| Severity | Open |
|---|---|
| HIGH | 1 |
| MEDIUM | 3 |
| LOW | 2 |
| INFO | 1 |

### [2026-09-13] From Sprint: AXI compliance (`feat/axi-compliance`)

Found while implementing AC7–AC13. None of these blocked an acceptance criterion, which is why they were filed rather than fixed — with one exception noted below.

| # | Status | Severity | Category | File:Line | Problem | Suggested fix |
|---|---|---|---|---|---|---|
| 1 | [ ] | HIGH | correctness | `internal/filesystem/core/advanced.go:571` | `sync-directories` against a **missing source** returns `success: true`, `files_copied: 0`, exit 0. The `filepath.Walk` callback opens with `if err != nil { return nil }`, so the walk error for an unreadable root is swallowed. Verified on the built binary: `--source /definitely/not/here` reports success. Same defect family as the `copyFile` argument bug fixed in `3cdd87d`, and the same root cause. | Stat the source before walking and refuse a missing or non-directory path; or capture the walk error instead of discarding it. Needs a test asserting a missing source exits non-zero. |
| 2 | [ ] | MEDIUM | correctness | `internal/filesystem/commands/{write,advanced,search,fileops,directory,edit}.go` (~30 sites) | Every `OutputError(err)` call is followed by `}` with no `return`. Production is saved only because `exitFunc` is `os.Exit`, which never returns. Under the test-injected `exitFunc` execution falls through into `OutputResult` with a nil or zero result, so any future test driving an error path gets two documents on one stream. `read.go` and the sites touched in this sprint already have their `return`. | Add `return` after every `OutputError` call. Mechanical, but it touches six files, which is why it was not folded into an unrelated task. |
| 3 | [ ] | MEDIUM | correctness | `internal/filesystem/core/edit.go:317` | `SearchReplaceResult` has no `dry_run` field, and `safe-edit` / `search-and-replace` mark a preview **only** in the text renderer (`commands/edit.go:154`, `:234`). In TOON and JSON — the default format and the one a script parses — a dry run is byte-identical to a real one. An agent cannot tell whether the edit was applied. `sync-directories` deliberately did not copy this pattern (see `3cdd87d`). | Add `DryRun bool` to `SearchReplaceResult` and the safe-edit result, set from options, and assert it in both machine formats. |
| 4 | [ ] | MEDIUM | design | `internal/filesystem/mcpserver/` | The MCP server is a subprocess wrapper that shells out to the same CLI (`handlers.go:125`) and does not parse its output, so it adds a process hop and 17 tool schemas of per-session context for no capability the CLI lacks. The owner confirmed Claude Code is the only client, and Claude Code has shell access. Kept working throughout this sprint (`--confirm` is passed in both arg builders) rather than removed, because removal is its own change with its own PR. | Decide whether to delete `cmd/llm-filesystem-mcp` and `internal/filesystem/mcpserver` outright. If kept, document which clients justify it. |
| 5 | [ ] | LOW | robustness | `internal/filesystem/core/read.go:295` | `readFileByBytes` fills its budget with a single `file.Read(buffer)`, which is permitted to return fewer bytes than requested. A short read silently returns less content than the budget allowed. Harmless today — `next_offset` is computed from `len(content)`, so a resume is still correct — but it makes truncation non-deterministic in size. | Use `io.ReadFull` with `io.ErrUnexpectedEOF` treated as success. |
| 6 | [ ] | LOW | testability | `internal/filesystem/commands/root.go:232` | The `goaxi.WriteHelp` failure fallback in `emitDiagnostic` is effectively unreachable: the target is a `bytes.Buffer`, whose writes never fail, so it triggers only if goaxi's internal marshal fails on an already-sanitized string. It is correct handling of a documented error return, but it is the one untested branch in that function (82.4% coverage). | Either accept it as defensive and annotate, or inject the help writer so the failure can be exercised. |
| 7 | [ ] | INFO | docs | `internal/filesystem/core/advanced.go:583` | `dirs_created` counts directories **visited**, not created — `MkdirAll` returns nil for an existing directory and the counter increments regardless. The `--dry-run` preview deliberately reproduces this so the preview matches the apply. Correcting the count would move the numbers for real runs too. | If corrected, change both paths in the same commit and update the preview test, which asserts preview counts equal applied counts. |

**Exception noted above:** item 1's sibling — `SyncDirectories` calling `copyFile(dstPath, path)` with reversed arguments — *was* fixed in `3cdd87d` rather than filed, because it made AC11's "the preview must match the apply" criterion unsatisfiable. `sync-directories` had never copied a file.
