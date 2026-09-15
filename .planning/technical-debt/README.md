# Technical Debt

Open items are unchecked. Resolved items are marked `[x]`.

Severity is about consequence, not effort. **HIGH** means a caller can be misled about what happened; **MEDIUM** means a real defect with a narrow blast radius or an internal-only one; **LOW** means cost without correctness risk.

## Stats

| Severity | Open |
|---|---|
| HIGH | 0 |
| MEDIUM | 2 |
| LOW | 6 |
| INFO | 3 |

### [2026-09-13] From Sprint: AXI compliance (`feat/axi-compliance`)

Filed while implementing AC7–AC13. Several were fixed during the review round that followed and are struck through below rather than deleted, so the record shows what was found and what happened to it.

| # | Status | Severity | File:Line | Item |
|---|---|---|---|---|
| 1 | [x] | HIGH | `core/advanced.go` | `sync-directories` against a missing source reported `success: true` at exit 0. **Fixed** in review batch 2: the source is stat-ed and refused up front, and walk errors are recorded instead of discarded. |
| 2 | [x] | MEDIUM | 25 sites across `commands/` | `OutputError` calls with no `return`. **Fixed** in review batch 3 — and it stopped being latent first: making `search-code` reject a missing path meant the call site fell through to `OutputResultAXI` with a nil result and panicked. |
| 3 | [ ] | MEDIUM | `core/edit.go:317` | `SearchReplaceResult` has no `dry_run` field, and `safe-edit` / `search-and-replace` mark a preview only in the TEXT renderer. In TOON and JSON — the default and the machine format — a dry run is byte-identical to a real one. `sync-directories` deliberately did not copy this pattern; these two still have it. |
| 4 | [ ] | MEDIUM | `internal/filesystem/mcpserver/` | The MCP server is a subprocess wrapper around this same CLI that does not parse its output, costing a process hop and 17 tool schemas of per-session context for no capability the CLI lacks. The owner confirmed Claude Code is the only client. Kept working throughout; removal is its own change with its own PR. |
| 5 | [x] | LOW | `core/read.go` | `readFileByBytes` filled its budget with a single `file.Read`, which may return short, and compared against `"EOF"` by string. **Fixed** in review batch 1 — it now seeks and streams through `io.ReadAll` with `io.LimitReader`. |
| 6 | [ ] | LOW | `commands/root.go` | The `goaxi.WriteHelp` failure fallback in `emitDiagnostic` is effectively unreachable — the target is a `bytes.Buffer`, whose writes never fail — so it is the one untested branch in that function. |
| 7 | [ ] | INFO | `core/advanced.go` | `dirs_created` counts directories **visited**, not created; `MkdirAll` returns nil for one that already exists. The `--dry-run` preview reproduces this deliberately so the preview matches the apply. Correcting it must move both numbers in one commit. |

### [2026-09-14] From review: two independent reviewers over the full diff

Every High and Medium from both reviewers was fixed in review batches 1–5. What remains is the Low tail, plus items that are judgement calls rather than defects.

| # | Status | Severity | File:Line | Item |
|---|---|---|---|---|
| 8 | [ ] | LOW | `core/read.go` | The line-boundary preference takes the **last** newline in the kept prefix, so a file of `"a\n"` followed by 200,000 characters returns 2 bytes against a 70,000 budget. Correct and resumable, but 35,000x short. Fall back to the rune cut when the line boundary discards more than about half the budget. |
| 9 | [ ] | LOW | `core/read.go` | `ReadMultipleFilesResult.TotalSize` excludes files that failed to stat, so "combined size on disk" undercounts when any path is missing. |
| 10 | [ ] | LOW | `commands/minimal.go` | `injectNextSteps` drops `next_steps` for a non-map payload while TOON still emits its `help[]` block. Not currently reachable — every result is a struct — but it is a latent format disagreement in code whose comment promises the two never disagree. |
| 11 | [ ] | LOW | `commands/directory.go` | A zero-result listing offers "remove `--pattern`" even when no `--pattern` was given. Harmless, but it is a help line that does not apply, which is the class AC10 exists to prevent. |
| 12 | [ ] | LOW | `commands/docs_test.go` | The command-count guard checks only `docs/llm-filesystem-commands.md`. README and `root.go` are no longer checked because neither states a count any more — but nothing stops a count being reintroduced there and drifting again. |
| 13 | [ ] | LOW | deployment | An already-installed `/usr/local/bin/llm-filesystem` predating this branch rejects `--confirm` and fails every delete routed through it. `binpath.go` prefers the sibling binary, so a freshly built pair is fine; an install resolving via `$PATH` needs a reinstall. Worth a release note. |
| 15 | [x] | MEDIUM | `internal/filesystem/core/edit.go` | **Fixed.** Both commands it names reported a success having changed nothing: `edit-blocks` said `"Applied 0 edits successfully"` and `edit-multiple-blocks` said `"Safe multiple blocks edited successfully"` with `successful_edits: 0`, each at exit 0, each rewriting the file anyway. `EditBlocks` and `EditMultipleBlocks` now refuse an empty old string with the edit's 1-based ordinal, error when no edit applied, skip the write — and the backup — on a no-op run, and a partial run succeeds but reports `unmatched` as a field rather than only in prose; all mirroring the singular `EditBlock`, which already refused both. Original report: **An edit that matches nothing reports `"Applied 0 edits successfully"` at exit 0.** `edit-blocks` decodes `--edits` into `EditPair{old_string,new_string}`; a payload using any other key names decodes to an empty edit, changes nothing, and returns `success: true` with `changes: 0`. Same for `edit-multiple-blocks` (`old_text`/`new_text`/`mode`), and for a correctly-keyed payload whose text simply does not match. The count is present, so a careful caller can detect it — but the message asserts the opposite, and `docs/llm-filesystem-commands.md` shipped a wrong-keyed example for months, meaning anyone copying our own documentation got a silent no-op. Found while writing the skill's argument shapes; the doc example is fixed, the behaviour is not. Suggested fix: report zero applied edits as a failure, or at minimum stop calling it success. Related to #3, which covers the same two commands' dry-run reporting. |
| 14 | [ ] | INFO | AC12 | `--full --format json` remains byte-identical to the pre-AXI `--json` output for every command **except** `read-file` and `read-multiple-files` on over-budget files, where the old output was a `SizeExceededError` body and the new one is content. That divergence is AC8 deliberately removing the refusal, not a regression — recorded because AC12 claims identity without exception. |
| 16 | [ ] | INFO | `commands/search.go` | `search-code --format json` costs **more** in minimal mode than with `--full`: its minimal field set is already its full field set (`content`, `file`, `line`, identical for all 569 items on a sample run), so the projection removes nothing while minimal adds the 101-byte `next_steps` payload that `--full` omits to preserve AC12. Not a defect — fixing it means either dropping contextual disclosure from minimal JSON or breaking the legacy guarantee — but it means principle 2 buys nothing for this command, and the token reduction measured for `search-code` comes entirely from TOON encoding. Found by `benchmarks/tokens.sh`; explained at the foot of `benchmarks/results-tokens.md`. |
