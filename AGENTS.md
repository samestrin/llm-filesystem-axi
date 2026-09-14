# Working on llm-filesystem

House rules for an agent making changes **to this repository**.

> Not to be confused with `integrations/cli/AGENTS.md` and `integrations/mcp/AGENTS.md`. Those are shipped artefacts telling an agent how to *use* the tool. This file is about *building* it.

## What this is

An agent-ergonomic filesystem CLI built to the [AXI](https://axi.md) principles. One rule sits above the rest, and most of the bugs found here were violations of it:

**Nothing may report success when it did not succeed.**

A refusal that exits 0. A read that returns part of a file and calls it whole. An edit that matches nothing and says "applied successfully". A sync that copies nothing and reports success. Every one of those shipped at some point. If you are weighing a design choice, that is the tiebreaker.

## Commands

```bash
make test        # go test ./...
make test-race   # race detector — CI runs this
make lint        # go vet + gofmt check
make build       # both binaries into ./build/ — REQUIRED before the MCP e2e tests run
```

CI runs `gofmt -s -l`, `go vet`, and `go test -race`. It builds into `./build/` first, because four MCP end-to-end tests exec the real binary and skip without it. They **fail** rather than skip when `CI` is set, so that gap cannot reopen silently.

## Testing

Write the failing test first. Every test carries a comment naming what it defends and why — usually the bug it prevents. Match that.

Five things this codebase has taught the hard way:

1. **Assert side effects, not exit codes.** A delete test that only checks for exit 2 passes even if the file was deleted and *then* something errored. Check the file. A JSON payload with wrong keys exits 0 having done nothing — only the file contents reveal it.
2. **`exitFunc` does not terminate under test.** It is swapped for a recorder, so execution continues past `OutputError`. Always `return` after it, or the next line runs with a nil result.
3. **Tests are not parallel-safe.** They swap package globals (`activeFmt`, `outWriter`, `exitFunc`, …). Never add `t.Parallel()`. `runCLI` restores those globals; if you add new package state, restore it there too.
4. **Match TOON fields at line start, not by substring.** `t.TempDir()` puts the *test name* in the path, and the output prints paths. An assertion on `"dry_run"` once matched its own temp directory. Use the `hasTOONField` helper.
5. **Prove a test can fail.** Break the code deliberately and confirm it goes red. Several tests here passed for the wrong reason until sabotaged.

## Documentation is under test

`integrations/cli/AGENTS.md` and `integrations/mcp/AGENTS.md` list every command and every tool, and tests compare those lists to the binary **in both directions**. Add a command without documenting it and CI fails; document one that does not exist and CI fails.

The command count in `docs/llm-filesystem-commands.md` is checked the same way. These guards exist because that number drifted to 26, 27 and 28 across three files simultaneously.

Before changing a documented example, run it. A flag sweep validates flag *names*, but nothing validates a JSON *payload* — `docs/` shipped an `edit-blocks` example with wrong keys for months, and it silently did nothing.

## Technical debt

`.planning/technical-debt/README.md` is the record.

**When you resolve an item, update it in the same change that fixes it.** Mark the row `[x]`, rewrite the entry to say what was done while keeping the original report readable, and correct the counts in the Stats table. A debt file that lags reality is worse than none, because it is trusted.

When you find something you are not fixing, file it: severity, `file:line`, what is wrong, and how to reproduce. Severity is about consequence — **HIGH** means a caller can be misled about what happened.

That applies to automated review findings too. Anything a review or CI gate raises that is judged out of scope, deferred, or not worth fixing gets a row here before the run ends — a finding that is dismissed in a pipeline log and nowhere else is lost the moment the run is archived.

A partial fix does not earn a `[x]`. An item naming two commands, two call sites or two formats stays open until all of them are done, or it is split into separate rows so the unfinished half is still visible. Ticking the box for half the work is the same failure this repo exists to prevent: reporting success for something that did not fully succeed.

## Scope

Prefer the smallest change that solves the problem. If a fix starts pulling in adjacent work, say so and ask rather than expanding quietly — scope here has a habit of growing severalfold once it starts.
