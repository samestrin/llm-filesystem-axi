# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.0.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **AXI catalog entry and audit re-pinned to v1.1.2.** The catalog entry's
  `reviewed_revision` now names the commit tag v1.1.2 points at, every
  `file:line` it cites is moved to where that code sits at the tag, and it
  cites `delete-file`'s refusal of the root, HOME and the working directory.
  `docs/axi-audit.md` states its verdicts at v1.1.2: principle 6 lists
  `write-multiple-files`, `read-multiple-files` and `batch-file-operations`
  among the commands that exit `1` on a partial run, with the new
  `--confirm` gates, and principles 9 and 10 say what v1.1.2 added. Every
  observation was re-run on a build of the tag.

### Fixed

- **`large-write-file` keeps the file it replaces.** An overwrite reset the
  file to `0644`, so a `0600` file became world-readable and a `0755` script
  lost its execute bits; it now keeps the original's mode, and a new file gets
  `0666` less the umask. `--append` replaced the file with the new content and
  exited `0`; it now keeps the existing bytes. Every write went through one
  fixed `<path>.tmp`, so two writes to one path could fail or collide; each
  now uses its own temp file. Under `--allowed-dirs`, a file with more than
  one hard link is refused with exit `2`, as `write-file` does. A requested
  backup that cannot be written now refuses the write with exit `1` and leaves
  the file unchanged, and a backup keeps the original's mode. A write that
  cannot be atomic (a hard-linked file, or a directory that is not writable)
  is written in place and reported as `in_place`.
- **`safe-edit` and `edit-multiple-blocks` refuse an edit when a requested
  backup cannot be written.** Both commands exit `1` with the backup error and
  leave the original file unchanged instead of discarding the error, applying
  the edit and implying that the requested safety copy exists.
- **`write-file` no longer writes through a hard link under `--allowed-dirs`.**
  With `A` allowed and `A/hl` a hard link to a file outside it, `write-file`
  (with or without `--append`) and `write-multiple-files` changed the outside
  file and exited `0`: a hard link has no target to resolve, so the
  allowed-dirs check passed it. Under `--allowed-dirs`, an existing file with
  more than one hard link is now left unchanged: `write-file` refuses it with
  exit `2`, and `write-multiple-files` reports the entry as failed and exits
  `1`, like any failed entry.
  Without `--allowed-dirs`, writes through hard links work as before.

## [1.1.2] - 2026-11-23

### Fixed

- **`--allowed-dirs` follows symlinks.** It compared text prefixes, so with
  `A` allowed and `A/link` pointing outside it, `write-file`,
  `large-write-file`, `write-multiple-files`, `copy-file`, `create-directory`,
  `delete-file`, `compress-files`, `extract-archive` and `sync-directories`
  wrote or deleted outside `A` with exit `0`, and a linked source copied
  outside data in. The path (its deepest existing parent, and a final or
  dangling link) and each allowed directory are now resolved before
  comparing. A directory being copied, synced or archived may not hold a
  link that leads outside, and a tree copy or sync may not write through a
  link already in the destination that leads outside. Removing or renaming a link itself is still
  allowed.
- **A directory cannot be copied into itself.** `copy-file --source d --dest
  d/sub/inner` (and the batch form) recursed until the path was too long,
  left hundreds of files and exited `1`; `sync-directories` with the
  destination inside the source ran too. A destination that is the source
  directory or inside it, for copy, move, batch copy or move, and sync, is
  now refused with exit `2` before anything is written.
- **`delete-file` refuses the filesystem root, HOME and the working
  directory.** `--path ~ --recursive --confirm` removed all of HOME and
  `--path .` the working directory. Those, and any directory holding one of
  them, are refused with exit `2`, alone or as a batch entry, as `setup`
  already refused HOME.
- **`edit-block` and `safe-edit` require `--new`; `edit-file` insert and
  replace require `--content`.** Left out, each read as `""`: `--old x`
  alone deleted the match with exit `0` (`safe-edit` too, leaving a `.bak`),
  and `edit-file` blanked the line or inserted an empty one. The flag is now
  required, exit `2`; `--new ""` and `--content ""` still work.
- **`compress-files` checks every input before touching `--output`, and
  replacing an existing `--output` needs `--confirm`.** An existing archive
  plus one missing input was truncated and left partial, exit `1`; an
  existing `--output` was replaced unasked. A missing input now exits `1`
  with nothing written, and an existing `--output` without `--confirm` exits
  `2`. A file that cannot be read inside a directory input is still a
  reported failure of a partial run.
- **`sync-directories` needs `--confirm` to replace files in the
  destination.** It replaced them, a newer file with older content
  included, without asking. A real run that would replace any exits `2`
  with nothing written unless given `--confirm`; `--dry-run` needs none.
- **More malformed invocations exit `2` rather than `1`:** an empty
  `--files`, `--edits` or `--operations` array, an unknown `edit-file`
  operation, an empty `--old`, and an empty required path, source,
  destination or pattern. `copy-file --dest ''` no longer draws a "would
  overwrite" refusal. A `write-file`, `large-write-file` or
  `write-multiple-files` path ending in a separator is refused rather than
  written as a file of that name, and two `write-multiple-files` entries for
  one path are refused rather than the last silently winning.
  `search-and-replace` skips `.git` directories, and `extract-archive`
  checks the archive before it creates `--dest`.
- **An edit without a replacement no longer deletes text.** `edit-blocks
  --edits '[{"old_string":"src"}]'` decoded the missing `new_string` to `""`,
  removed the matched text and exited `0`; `edit-multiple-blocks` did the same
  for `new_text`, and inserted an empty line for an `insert_before` or
  `insert_after` without it. The replacement key is now required (exit `2`,
  file untouched); `""` still works when given by name. An unknown edit `mode`
  is a usage error (exit `2`) rather than "none of 1 edits applied" (exit `1`).
- **`batch-file-operations` checks every entry before running any.** A
  missing key was not refused up front: a valid entry followed by a copy with
  no `destination` ran the first entry and then failed, an entry with no
  `operation` failed as `unknown operation:`, and a copy with no destination
  drew the wrong "overwrites an existing file" refusal. Each entry now needs
  `operation` (`copy`, `move` or `delete`), `source`, and for a move or copy
  `destination`, or the batch exits `2` with nothing run. Operations are
  numbered from 1 in messages, as edits are.
- **A dangling symlink at a move or copy destination needs `--confirm`.** The
  destination check followed the link, saw nothing, and let `copy-file`,
  `move-file` or a batch entry replace the link with a regular file, exit `0`.
  It now checks the link itself, as `delete-file` does.
- **A partial run names the failed entries and how to retry them.**
  `read-multiple-files`, `write-multiple-files` and `batch-file-operations`
  exited `1` with no `help[]`; they now list each failed entry with its reason
  and give a retry of only those, as `compress-files` does.
- **A bad key in a JSON payload names its entry.** `--edits`, `--files` and
  `--operations` printed the decoder's `json: unknown field "x"`; the message
  is now `edit 2: unknown key "x"`. Keys are matched case-sensitively, so
  `{"Path","Content"}` is refused with the expected spelling rather than
  silently accepted.
- **`write-multiple-files` no longer truncates a file on a wrong key.** An
  entry keyed `{"path","text"}` decoded to empty content, so the file it named
  was cut to 0 bytes and the command reported success with exit `0`. The JSON
  flags of `write-multiple-files`, `batch-file-operations`, `edit-blocks` and
  `edit-multiple-blocks` are now decoded strictly: an unknown key, a missing
  `content` or `path`, an edit with no `old_string`/`old_text`, or a line mode
  with no `line_number` is refused with exit `2` before anything runs, and the
  diagnostic shows the expected shape. Invalid JSON in those flags also exits
  `2` now (it was `1`).
- **`batch-file-operations` no longer lets `~` or a trailing slash past
  `--confirm`.** The gate checked the destination as typed while the copy or
  move used the normalized path, so a copy onto an existing `~/victim` or
  `victim/` overwrote it with exit `0`. The gate now checks the normalized
  path, as `move-file` and `copy-file` already did.
- **A run with failed entries exits `1`.** `write-multiple-files`,
  `batch-file-operations` and `read-multiple-files` printed `failed: N` and
  exited `0`, so the MCP server, which takes `isError` from the exit code,
  reported success. They now print the result and exit `1`, as
  `compress-files`, `extract-archive` and `sync-directories` already did.
- **Errors suggest the fixing command for the common failures.** A path that
  does not exist suggests `list-directory` on its parent, an edit whose old
  text matched nothing suggests `read-file` on the file, and a malformed JSON
  payload shows the expected shape. Other failures still point at the
  command's `--help`.
- **Release binaries are static.** The CLI imports `net` and `os/user`, so
  with cgo available the Linux binaries were dynamically linked although the
  docs said static. `make build`, `make build-all` and the release workflow
  set `CGO_ENABLED=0`, and the release check fails if the linux-amd64 pair is
  not statically linked.

### Changed

- **Every command's `--help` has an Examples block** of two or three
  invocations (AXI principle 10; only `setup` had one), each checked by a test
  against the command's flags and payload decoder. The skill says which
  commands apply the entries that work and then exit `1`, and that
  `edit-multiple-blocks` leaves a `<file>.bak.<timestamp>` backup unless
  passed `--backup=false`. The batch `--confirm` refusal tells an MCP caller
  to pass `confirm: true`. `make lint` now fails when `gofmt` would change a
  file; it printed the list and passed. The catalog entry's description is
  shortened and says "size-capped" (the budget is in characters), and its
  `write-file` exception is labelled principle 6, and it now lists the
  `compress-files` and `sync-directories` gates. The description is trimmed
  again, to 202 characters. The token table is
  re-measured on this release's final tree.
- **Docs from the first axi.md review round.** The skill documents
  `line_number` for the line modes of `edit-multiple-blocks`, links the routing
  guide by URL, and gives an install line for when the binary is not on
  `PATH`. The README pins v1.1.2, adds the `PATH` line to Quick start, gives the
  non-interactive `npx skills add ... --agent claude-code -y` form, and states
  which commands exit `1` on a partial run and that a partial `edit-blocks` or
  `edit-multiple-blocks` exits `0` with `unmatched`. Every example in
  `docs/llm-filesystem-commands.md` is now parsed by a test, which caught
  `find-large-files --min-size "1MB"` (the flag takes bytes). The token table
  in `docs/background.md` is re-measured on this release, with `search-code`
  no longer capped at 1000 matches.
- **AXI catalog entry and audit re-pinned to v1.1.1.** The catalog entry's
  `reviewed_revision` now names the commit tag v1.1.1 points at, and
  `docs/axi-audit.md` states its verdicts at v1.1.1. Every observation was
  re-run on a build of the tag; no cited line moved and no result changed.

## [1.1.1] - 2026-11-20

### Changed

- **Docs re-pinned to v1.1.0.** The AXI catalog entry's `reviewed_revision`
  now names the commit tag v1.1.0 points at, with every observation re-run on
  a build of it. `docs/axi-audit.md` restates one verdict per principle at
  v1.1.0, with the v1.0.0 audit kept under History. The README names v1.1.0
  as the first release with `llm-filesystem-mcp`, and its Performance table is
  re-measured on v1.1.0 (on a Linux x86_64 host, not the Apple M4 Pro the
  v1.0.2 table used).

### Fixed

- **`batch-file-operations --help` names every case `--confirm` gates.** The
  help text and the `--confirm` flag said only deletions need it, but a move or
  copy onto an existing path is refused without it too, as the docs already
  said. The wording now names both. No behaviour change.
- **`--json=false` and `--min=false` no longer pick JSON or text for a usage
  error.** When a flag error stops parsing, the output format is read from the
  raw arguments, and that scan counted any `--json` or `--min` as set, joined
  value or not. `read-file --path x --json=false --bogus` printed its
  diagnostic as JSON. The scan now reads the value the way the flag parser
  does, so that diagnostic is TOON.
- **The `[1.1.0]` entry no longer says errors go to stderr.** It described a
  README sentence that the same release replaced: errors go to stdout in every
  format. The entry now names only the benchmark script fix.
- **`docs/llm-filesystem-commands.md` names the `extract-archive` caps,**
  `--max-bytes` (1 GiB) and `--max-entries` (100,000), each `-1` for no limit.

## [1.1.0] - 2026-11-17

### Added

- **A thin MCP stdio server, `llm-filesystem-mcp`.** It exposes the CLI's
  operational commands as MCP tools by running the `llm-filesystem` binary,
  rather than maintaining a second filesystem implementation. Binary lookup
  uses `LLM_FILESYSTEM_BINARY`, then `PATH`, then the server's own directory.
- **The installer installs `llm-filesystem-mcp` beside the CLI.** Both release
  binaries are checksum- and version-checked before either is installed. A
  release without a server asset still installs the verified CLI with a
  visible skip notice; an invalid server asset installs neither binary.
- **Release assets include the MCP server for every supported platform.**
  Releases now build and version-stamp both `llm-filesystem` and
  `llm-filesystem-mcp` for Linux, macOS and Windows, and list all ten binaries
  in `SHA256SUMS.txt`.
- **`setup` also registers Codex.** It merges a SessionStart hook that runs
  `llm-filesystem` into the project's `.codex/hooks.json`, keeping every other
  hook. The hook names the binary by its absolute path unless `PATH` finds
  this same binary, and a later run repairs that path after the binary moves
  instead of adding a second hook. Nothing is written under `~/.codex`. Codex
  runs a project's hooks only once the project is trusted and the hook
  reviewed.
- **`setup` also registers OpenCode.** It writes a plugin to the project's
  `.opencode/plugins/llm-filesystem.js` that runs `llm-filesystem` once when
  OpenCode loads it and adds the home view to the system context; it adds no
  tool. The plugin names the binary the same way the Codex hook does, a later
  run rewrites it after the binary moves, and a file at that path that `setup`
  did not write is refused and left alone. Nothing is written under
  `~/.config/opencode`.

- **`VISION.md` states the project's scope and non-goals.** It gives proposed
  changes one place to check what belongs in `llm-filesystem-axi`.

- **`extract-archive --max-bytes` and `--max-entries`.** Extraction stops at
  1 GiB written in total or 100,000 entries by default; pass a larger value,
  or `-1` for no limit.

### Changed

- **The docs state the behaviour epic 10 shipped, and the one remaining gap
  carries its reason.** The audit page gains an epic 10 re-check listing the
  four closed gaps with their fixes and guarding tests; the README, the
  command reference, the agent routing guide and the Claude Code skill now
  state the `move-file`/`copy-file` overwrite gate and the exact search totals
  past `--max-results`; the session-end gap in the README and the catalog
  entry says why it stays (this CLI keeps no session history), and `setup
  --help` says the same. New tests pin each of those claims.

- **`move-file` and `copy-file` refuse to replace an existing destination
  without `--confirm`.** They used to overwrite silently and report success,
  while `delete-file` and destructive `batch-file-operations` entries already
  required confirmation for the same destruction. The refusal is a structured
  usage error naming `--confirm`, with a next step showing the confirmed
  command, exit 2, and nothing touched. A move or copy to a path that does not
  exist yet needs no confirmation, and `write-file` keeps replacing. The MCP
  tools take the same gate as `confirm: true`.

- **Errors go to stdout in every format, whatever the flag order.** With
  `--format text`, usage and tool errors used to go to stderr and leave stdout
  empty; they now print their `Error: <msg>` text on stdout, and nothing goes
  to stderr. A flag error listed before `--format`, as in
  `read-file --bogus --format text`, used to render in the default TOON; it
  now renders in the format asked for. Exit codes are unchanged: 1 for a
  failure, 2 for a usage error.

- **`VISION.md` now states the tool's role in the AXI family.** It names
  the file and archive operations and sibling boundaries, records the truth
  and edit-safety guarantees already present in the CLI, and rules out a
  daemon and file watcher.

- **The README is titled `llm-filesystem-axi`, after the repository,** and
  keeps `llm-filesystem` wherever it means the command. A new Origin section
  says where the project came from, and a `NOTICE` file credits
  fast-filesystem-mcp by efforthye (Apache-2.0).

- **The README's one-line description names AXI.** It now says the output
  is [AXI](https://axi.md)-compliant, and the GitHub repository description
  says the same in plain text.

- **Edits refuse an old string that matches more than once.** `edit-block`,
  `safe-edit`, `edit-blocks` and `edit-multiple-blocks` used to change the
  first occurrence silently and report success. They now exit 1 with
  `old_string matches N times` (for the batch commands, `edit K:` names the
  edit, checked against the content the earlier edits leave), change nothing,
  and their help names `--replace-all` or a longer old string. The new
  `--replace-all` flag replaces every occurrence and reports `replacements`.
  **Migration:** a call that relied on first-match replacement now fails;
  lengthen the old string with surrounding lines until it is unique, or pass
  `--replace-all` if every occurrence should change.

### Fixed

- **Search totals are exact when `--max-results` cuts the list.**
  `search-code` and `search-files` used to stop at the cap (default 1000) and
  report it as `total_matches` or `total`, so 1,500 matches read as 1000 with
  no marker. They now keep counting without storing the extra matches, and
  report the exact `total_matches`, `total_files` or `total`, plus `shown`,
  `truncated: true` and a help line naming `--max-results`. A search the cap
  did not cut carries no marker and no hint.

- **The release checklist's benchmark script name now matches the code.**
  `release/CHECKLIST.md` step 2 names `benchmarks/benchmark.sh` instead of a
  script that does not exist.

- **Empty search results are an empty list, not `null`.** `search-code` and
  `search-files` with no match printed `matches: null` (JSON
  `"matches": null`), and `find-large-files` with nothing to report printed
  `files: null` with no next step. They now print `matches[0]:` / `files[0]:`
  (JSON `[]`) beside a total of 0, as `list-directory` does, and
  `find-large-files` adds a `help[]` line.

- **The AXI catalog entry describes the current release.** Its
  `source_observations` in `docs/axi-catalog-entry.yaml` described v1.0.3,
  and four of them (Claude Code-only setup, `--format text` errors on stderr,
  a capped search reporting the cap as its total, and move/copy replacing a
  destination without `--confirm`) were no longer true. Every line is
  rewritten against the v1.1.0 code, and the MCP server is now among the
  reviewed components.

- **The demo no longer shows the hidden `--axi` flag.** The tape and
  `docs/demo/README.md` used `list-directory --axi`, a flag `--help` does not
  list. The committed GIF is re-rendered after the v1.1.0 tag.

- **`setup` PATH-verifies the Claude Code hook.** It wrote the bare command
  `llm-filesystem` into `.claude/settings.json` even when `PATH` did not find
  this binary, so the hook ran nothing or a different copy. It now names the
  binary the way the Codex hook does: by name only when `PATH` finds this same
  binary, by its absolute path otherwise. A later run repairs an existing
  llm-filesystem hook after the binary moves instead of adding a second one.

- **`delete-file` on a path that is already gone exits 0.** It used to print
  `error: true` / `path does not exist` and exit 1. The state asked for
  already holds, so it now reports `deleted: false`, `existed: false` and a
  message, and exits 0; a real delete reports `deleted: true`,
  `existed: true`. A missing parent directory, or a path outside
  `--allowed-dirs`, is still an error. A `batch-file-operations` delete entry
  follows the same rule. A dangling symlink is deleted, not reported missing.

- **An empty directory lists as an empty list.** `list-directory` printed
  `items: null`; it now prints `items[0]:` in TOON and `"items": []` in JSON,
  beside `total: 0`.

- **`extract-archive` had no decompression-bomb limit.** Each entry was
  copied with an unbounded `io.Copy`. The entry that crosses a cap now fails,
  is counted in `failed`, and its partial file is removed; the run stops,
  reports `truncated: true`, and exits 1.

- **Edits are atomic and keep backups' modes.** Every edit command wrote with
  a truncate-then-write, so a crash or full disk mid-write could leave a
  truncated file; they now write a temp file beside the target, fsync it,
  give it the original mode and rename it into place. A symlinked path is
  edited at its target, as before. `safe-edit` and `edit-multiple-blocks`
  backups used to be created `0644` whatever the original was, so a `0600`
  file's backup was world-readable; they now take the original's mode.

- **`-V` is shorthand-only.** Its hidden long name `--version-alias` was
  accepted as an undocumented third spelling of `--version`; it is now an
  unknown flag (exit 2).

- **`read-multiple-files` documents what `total_size` counts:** the files
  that exist. A missing path adds nothing and is counted in `failed`.

## [1.0.3] - 2026-11-11

### Fixed

- **`--version` on a `go install` binary reports the version installed.**
  `go install ...@vX.Y.Z` adds no version stamp, so the binary printed the
  default `1.0.0` whatever tag it was built from. An unstamped binary now
  prints the module version Go recorded for it; a stamped release build is
  unchanged.

### Changed

- **The README is rewritten to the standard's section order, checked against
  the released binary.** The long-form material — why Go, the token cost of
  the default output, and compatibility with the pre-AXI output — moves to
  `docs/background.md`. The README gains a Limits section that names the known
  AXI gaps, and the catalog entry's `exception` text states the same gaps,
  item for item.
- **The docs and the Claude Code integration are aligned with the new
  README.** The audit page re-checks its status lines against the released
  binary, and the Claude Code README gains the `npx skills add` install path.

### Added

- **A demo tape and a release checklist.** `docs/demo/` holds the VHS tape
  and the command that renders the demo GIF; `release/CHECKLIST.md` lists the
  release steps.
- **An AXI catalog entry in `catalog.yaml` form**
  (`docs/axi-catalog-entry.yaml`), pinned to the latest tag with an honest
  `exception` statement; it replaces `release/axi-catalog-entry.md`.
- **A benchmark handshake test and a generated results table.**
  `benchmarks/benchmark.sh` measures the MCP handshake against
  fast-filesystem-mcp and regenerates `results-performance.md`; fixture tests
  guard its failure checks.

### Fixed

- **Two false claims in the known-gaps statements.** The catalog entry's
  `exception` text and the README's Limits section now say that a batch
  `move` or `copy` onto an existing destination also requires `--confirm`
  (not only deletions), and that errors go to stderr only *once* `--format
  text` is parsed — `read-file --bogus --format text` hits the unknown flag
  first and renders in toon on stdout. A test pins both qualifiers, and an
  end-to-end test pins the flag-order behaviour against the real binary.

## [1.0.2] - 2026-11-10

### Added

- **`-v`, `-V` and `--version` print the bare version and exit `0`.** The
  version prints without its `v` prefix on one line, in every output mode.
  `read-file -V` stays a usage error.
- **An unknown-flag error lists the failing command's own flags.** The usage
  error now names that command's visible flags (sorted, with short forms) in
  `help[]`, and keeps the `--help` pointer.

### Fixed

- **`extract-archive` refuses an entry whose parent directory is a symlink
  leading outside `--dest`.** With `dest/sub` linked elsewhere, an entry
  `sub/a.txt` was written through the link, overwriting the outside file, while
  the run reported `failed: 0` and `success: true`. The entry's parent is now
  resolved and must stay inside the resolved `--dest`; otherwise the entry is
  counted in `failed`, named in `failures[]`, and the run exits `1`. No
  directory is created outside `--dest` either. Links that stay inside `--dest`,
  and a `--dest` that is itself a symlink, still work.

## [1.0.1] - 2026-11-09

### Fixed

- **`setup` refuses a symlinked target file and re-checks its write directory
  during the write.** A `.claude/settings.json` or
  `.claude/skills/llm-filesystem/SKILL.md` that is itself a symlink is no
  longer silently replaced by a regular file — the run is refused with exit 1
  and nothing is touched, because the rename would report "written" while the
  file the link pointed to stayed stale. Directory symlinks that stay inside
  the project are still followed. The directory the rename lands in is also
  re-checked after the temporary file is created, so a link swapped in between
  the plan and the write can no longer send a write outside the project.
- **The home view renders `bin` with `~` when the executable sits under the
  home directory**, matching `cwd` and axi.md principle 8.

### Added

- **`llm-filesystem setup` registers the tool with Claude Code in the working
  directory.** It merges a SessionStart hook that runs `llm-filesystem` into
  `.claude/settings.json`, keeping every other key and hook, and writes the
  Agent Skill to `.claude/skills/llm-filesystem/SKILL.md`. It supports
  `--dry-run`, is idempotent, never prompts, and writes only under the working
  directory — never the home directory. A `settings.json` that is not a JSON
  object, or whose `hooks` or `hooks.SessionStart` has the wrong type, is
  refused and left untouched. The agent setup docs (`README.md` and
  `integrations/claude-code/`) now lead with this command, and the routing
  guidance is fetched by URL instead of copied from a clone, so a user who
  installed the binary from the README needs no clone of this repo.
- **`--axi` is accepted as a hidden global flag.** It is a no-op alias for the
  TOON default, so a habit from the sibling AXI tools no longer exits 2. It
  changes no output and does not override `--format` or `--json`.
- **`docs/axi-audit.md`: a hand audit of the v1.0.0 binary against the ten
  principles on axi.md**, in the same format as the sibling tools. Each
  principle has a verdict and one line of evidence; the archive
  partial-failure finding fixed by epic 04 (not in the tag) and two
  unscheduled gaps (a `create-directory` false `created: true`, and the
  home view's `bin` path not tilde-rendered) are recorded with what closes
  them. Principle 7 was met only through the manual copy until `setup` landed
  in this release.

- **`install.sh` runs the binary before installing it, and says what it
  replaced.** The staged binary must answer `--version` within 10 seconds
  (bounded without `timeout`, which macOS lacks); one that fails or hangs is
  refused and nothing is installed, so a binary for the wrong architecture no
  longer passes its checksum and fails on first use. Re-running the installer
  over an existing binary prints the version installed and the one replaced.
  After installing it warns, naming the path, when another `llm-filesystem`
  earlier on `PATH` shadows the new one, and when a leftover
  `llm-filesystem-mcp` is still on `PATH`; it does not delete files it did not
  install. The `shasum` path macOS uses is now tested with `sha256sum` hidden.
  The README gains an **Upgrade** subsection under Install: re-running the
  installer is the upgrade path, `LLM_FILESYSTEM_VERSION` pins a release, and
  the shadowing warning is explained with its fix.

### Fixed — archive commands no longer report success for work that did not happen

- **`extract-archive` reports every entry it did not write.** An entry refused
  by the path-escape guard, an unsupported tar entry type such as a symlink, or
  a mkdir, open, create, copy or close failure is now counted in the new
  `failed` field with the entry name and reason in `failures[]`, and `success`
  is `false`. Files that replaced existing destination files are counted in
  `overwritten`. A partial run still prints the full result, then exits `1`.
- **`compress-files` reports every input it did not add.** A missing input
  path, a walk error or a per-file add error is counted in `failed` with the
  reason in `failures[]`, `success` is `false`, and the run exits `1`.
  `overwritten` reports whether `--output` already existed. The zip, gzip and
  tar writers are now closed explicitly and their errors returned, and the
  output file is removed when the archive could not be finished, so a
  truncated archive can no longer pass as a success.

### Fixed — read and edit previews that did not match what happened

- **`search-and-replace --dry-run`** reported each file with
  `message: "Modified"` although nothing was written. The per-file message is
  now `"Would modify"` in a dry run, so a preview no longer claims a write
  happened. The top-level `dry_run: true` marker is unchanged.
- **A truncated read whose last newline sat near the start of the file** could
  return 2 bytes against a 70,000-character budget (a file of `"a\n"` followed
  by a single 200,000-character line). The line-boundary preference in the
  budget cut is now abandoned when it would discard more than about half the
  budget, falling back to the rune cut, so a truncated preview keeps roughly
  the full budget of content. `next_offset` still resumes cleanly; the resume
  may now start mid-line where it previously restarted the same near-empty
  prefix.

### Fixed — three more reports that did not match what happened

- **`create-directory` on a directory that already exists** printed
  `created: true` and exited 0 though nothing was created. It now reports
  `created: false` / `exists: true`. A file at the path is still an error.
- **`compress-files` with an `--output` that is also an input** truncated the
  input before reading it, then reported success with the original gone. It now
  refuses before touching anything, including when the output reaches the input
  through a symlink.
- **`sync` capped `failed` at 100.** 150 failed copies reported `failed: 100`.
  `failed` is now the true count; only the `failures` sample is capped.

### Fixed — a help line that did not apply

- **`list-directory` no longer suggests removing `--pattern` when none was
  given.** A listing that found nothing carried "Drop the filter: remove
  `--pattern`" either way, so an unfiltered invocation was told to drop a
  filter it never applied — a step the caller cannot act on. A listing emptied
  by `--pattern` still offers it, and an unfiltered one still points at
  `--show-hidden`.

### Fixed — archive and sync stop claiming work they did not do

- **`sync-directories` exits `1` on a partial run.** It printed `success: false`
  with the failed paths in `failures[]` and still exited `0`, while
  `compress-files` and `extract-archive` exit `1` in the same case. The three
  commands now agree: the result is printed first, so the failures stay visible,
  then the run exits `1`. The text output also names the failed count and the
  help block names the paths that were not copied.
- **`sync-directories` reports the directories it created, not the ones it
  walked past.** `dirs_created` counted every source directory visited, because
  `MkdirAll` returns nil for one that already exists, so a sync into a populated
  destination claimed work it had not done. The `--dry-run` preview reproduces
  the same rule against the destination, so the two numbers still match.
- **`compress-files` no longer archives its own output.** An `--output` inside
  one of the `--paths` directories was walked into the archive, so the archive
  held a truncated copy of itself and counted it in `files_added`. The output is
  skipped while walking.
- **`extract-archive` refuses a symlink planted at an entry's target.** It wrote
  with `os.Create`, which follows a symlink, so an entry named like a link
  already sitting in `--dest` wrote outside the destination. The entry is now a
  reported failure and the link is left alone.
- **`extract-archive` refuses a zip symlink entry.** An entry whose mode said
  symlink was written as a regular file holding the link target as its text and
  counted in `files_extracted`, while the same tar entry was a reported failure.
  Both are now reported as an unsupported entry type.

### Changed — `compress-files` picks the archive type from `--output`

- The archive type now comes from the `--output` extension (`.zip`, `.tar.gz`,
  `.tgz`), so `compress-files --output release.tar.gz` writes a real gzip
  archive — previously it wrote a zip under that name, and `extract-archive`
  then failed on it. The command's local `--format` flag is renamed to
  `--archive-format`; **`--format zip|tar.gz|tgz` remains as a deprecated
  alias**, so the global `--format json|toon|text` works on this command again.
  An `--archive-format` that contradicts the extension, or an `--output` with
  no recognised extension and no `--archive-format`, is a usage error (exit 2)
  that names the accepted extensions and writes nothing.

### Fixed — documentation and benchmarks that claimed more than they measured

- **`integrations/cli/AGENTS.md` said TOON saved 62-94% of the tokens on a
  directory listing.** A fresh `benchmarks/tokens.sh` run measures
  **59.8-93.3%**, so the guide the tool ships to agents now states that range and
  names `benchmarks/results-tokens.md` as where it comes from.
- **`docs/axi-audit.md` corrections.** Principle 6 said "one gap" while listing
  two (6a and 6b), and credited `#26` — the `compress-files` archive-type
  feature — as docs and tests, when the docs are `#27` and the test `#28`.
  Principle 7 stated in the present tense that there is no `setup` command and
  cited a README line range that has since moved; it now says the tag had no
  `setup` command and names the PR that added it (`#29`, not yet released).
  Principle 10's command count notes the tag's 28 against the current 29.
- **The speed benchmark now reports the Python client's own start.** Its read
  and tree figures each include one Python process start that the Go figures do
  not, which flattered Go by that much; `benchmarks/benchmark.sh` measures it on
  every run and prints the TypeScript figure with it removed beside the raw one
  (a read is 91x, not 103x), and `benchmarks/README.md` says so in the table.
- **`benchmarks/results-tokens.md` explains the `get-directory-tree` case**
  where `json-min` also exceeds `json-full` (186 against 184 on `internal/`, 96
  against 70 on `/usr/bin`), not only the `search-code` one.

## [1.0.0] - 2026-11-01

First standalone release. `llm-filesystem` was extracted from the
[`llm-tools`](https://github.com/samestrin/llm-tools) monorepo into its own
repository so it can be released and versioned independently.

The single `1.0.0` version reconciles the previously divergent internal versions
(CLI `1.5.0`, MCP `1.7.0`) into one ldflags-stamped version. The MCP server does
not ship in this release; see *Removed — the MCP server* below.

### Added — AXI compliance

Adopted the [AXI](https://axi.md) design principles for agent-ergonomic CLIs:

- **Token-efficient TOON output by default** (`--format toon|json|text`). Measured
  with `benchmarks/tokens.sh` against `--full --format json`: a directory listing
  costs **60-93% fewer tokens** (91-93% on directories of 56-523 entries, about 60%
  on a handful), a tree 35-56% on directories with subdirectories to expand, and
  content-dominated commands such as `search-code` and `read-file` 6-38% —
  `read-file` is 6.0% on every target, while `search-code` runs 34.4% on this
  repository's `internal/` up to 38.3% on `/usr/share` — since no schema choice
  shrinks the bytes of a file you asked to read. TOON encoding alone accounts for
  0-48% of that (0% for the flat `/usr/bin` tree, 48% for `list-directory` on
  `/usr/share`); on large listings most of the saving comes from the minimal field
  set rather than the format.
- **Minimal default field sets** with a `--full` escape hatch. Listings, trees,
  and searches emit 3-4 fields by default; `--full` (or `LLM_FILESYSTEM_FULL=1`)
  restores every field.
- **Contextual disclosure** as a trailing `help[]` block on TOON output, written by go-axi. `--format json` keeps the `next_steps` payload field instead, since an appended TOON line would stop the output being one JSON document.
- **Structured errors that fail loud**, with exit codes from go-axi's shared constants: `0` success, `1` tool failure, `2` usage error. An unknown subcommand, an unknown flag, a missing required flag and an invalid `--format` each exit `2` with a diagnostic — previously all four exited `1` printing nothing at all.
- **Hardened TOON output** via [go-axi](https://github.com/samestrin/go-axi), replacing the raw codec. File names and contents are sanitized of ANSI escapes, `U+2028`/`U+2029`, lone C1 bytes and invalid UTF-8 before they reach a terminal, and a value the codec would emit as empty output is refused rather than printed as nothing with a zero exit.
- Backward compatible: `--full --format json` is byte-identical to the old
  `--json`, with one exception: `read-file` and `read-multiple-files` on
  over-budget files now return content where the old output was a
  `SizeExceededError` body; `--json`/`--min` remain as deprecated aliases.
- **Ambient context** (`integrations/claude-code/`): a CLAUDE.md routing snippet
  and an on-demand skill that position llm-filesystem as a complement to Claude's
  native Read/Write/Edit — single-file work stays native, batch/specialized work
  routes here.

### Fixed — failures that reported success

- **An over-budget read exited `0` under `--format json`** while printing an
  error body, so an agent checking the status was told the read had succeeded.
  TOON and text exited `1` for the identical condition. The refusal also wrote
  through `fmt.Println` to the real stdout, bypassing the renderer entirely,
  which is why no test could observe it.
- **`sync-directories` had never copied a file.** It called
  `copyFile(dstPath, path)` against a `copyFile(src, dst)` signature, opening a
  destination that did not exist; the walk discarded the error and reported
  `files_copied: 0` with `success: true`. The directories it did create left the
  destination looking populated.
- **A `safe-edit` or `search-and-replace` dry run looked like a real one in TOON
  and JSON.** Only the human text was prefixed `[DRY RUN]`; in the default and
  machine formats a `search-and-replace` preview was byte-identical to an
  applied run, and `safe-edit` differed only in a prose message. Both now carry
  `dry_run: true` in every format, as `sync-directories` does.

### Changed — reads truncate instead of refusing

- A read over the size budget returns a content prefix with `truncated`,
  `total_size` and `next_offset`, exits `0`, and carries a `help[]` block naming
  the exact resume command. `next_offset` is the literal `--start-offset` that
  continues the read. The cut prefers a line boundary and never splits a rune,
  and the budget is measured in encoded characters rather than raw bytes.
- `--full` extends from "all fields" to "all fields and all bytes". An explicit
  `--max-size` is more specific and still wins.
- `read-multiple-files` allocates its budget greedily, so the files requested
  first come back whole and anything past the budget is never opened.
- **Breaking:** usage errors (unknown flag, unknown command, missing required
  flag, invalid `--format`) now render as a structured document on **stdout** in
  the active format instead of plain text on stderr. Exit codes are unchanged.
  `--format text` keeps its `Error: <msg>` shape on stderr.
- **Breaking:** `SizeExceededError` and `TotalSizeExceededError` were removed.
  Size is no longer an error class.

### Added — the rest of AXI

- **Content-first landing page.** A bare `llm-filesystem` answers with live data
  — the running binary, the working directory, and what is in it — instead of a
  usage screen. `--help` still prints the full reference.
- **`--fields`**, selecting item fields by name. An unknown field exits `2` and
  the help block enumerates every valid one, so a wrong guess teaches the whole
  schema in a single turn. Mutually exclusive with an explicit `--full`.
- **Recovery guidance on errors.** Every diagnostic carries a next step, and
  result-aware `help[]` means a zero-result search no longer offers to open a
  match that does not exist.
- **Confirm gates.** `delete-file` requires `--confirm`, and
  `batch-file-operations` requires it when any operation is a delete — gating
  one without the other would be theatre. `sync-directories` gains `--dry-run`,
  with `dry_run` and `planned[]` visible in TOON and JSON rather than only in
  the human text.

### Fixed — found by review, before release

Two independent reviewers read the full diff. Everything below was reproduced against the built binary.

- **`sync-directories` could destroy destination data and report success.** `copyFile` called `os.Create`, which truncates, before copying a byte; a copy failing part-way — EISDIR, ENOSPC, a permission change — left the destination empty and the walk discarded the error. Copies are now written to a temporary file and renamed into place, so a destination is either replaced completely or untouched. Failures are counted and reported, and a destination that cannot be created no longer has its subtree attempted.
- **`batch-file-operations` let a `move` or `copy` overwrite a file with no confirmation.** The gate matched only `delete`, while `os.Rename` and `os.Create` both replace silently. Overwriting operations now require `--confirm`; ones that create something new do not.
- **`search-code` and `search-files` reported a missing path as an empty result** at exit 0, because the path was normalized and sandbox-checked but never stat-ed. A mistyped path is now a failure, not an absence.
- **`--fields` on an unsupported command performed the operation and then reported a usage error.** The check ran at render time, after the command body; `delete-file --confirm --fields path` deleted the file and exited 2, while exit 2 promises nothing was touched. It now runs before the command.
- **A binary file could produce an endless resume loop.** The rune-boundary backoff stripped every byte of an invalid-UTF-8 prefix, returning empty content with `next_offset` equal to the offset given. The backoff is now bounded.
- **The advertised resume read the whole file.** `--start-offset` reached the reader with no byte limit and called `os.ReadFile`: a 300 MB file peaked at 609 MB resident. It now seeks and streams — 8 MB.
- **`read-multiple-files` counters did not account for every file**, so `success + failed` could be less than the number of files requested, and its budget was spent in raw bytes while measured in encoded characters, overrunning the cap several times over on escape-heavy content.
- **Tool failures carried no `help[]`**, and the landing view exited non-zero when the working directory was unknowable. Both are acceptance criteria this release claims.
- **25 `OutputError` call sites were missing a `return`**, which became a nil-pointer panic once a search could fail.
- **`search-code --context N` silently discarded the context lines it was asked for.** The minimal projection dropped each match's `context` field, so default-mode output was byte-identical to a search with no `--context` at all and still exited `0`. An explicitly requested field is more specific than a default schema, so the projection now keeps `context` when, and only when, `--context` asked for it. The help line read "Add `--full` for surrounding context lines", but `--full` alone returns no context; it now names `--context`, and is omitted for a caller who already passed it.

### Changed — installation

- **`install.sh` no longer builds from source or needs sudo.** It downloads the
  release binary for the platform (Linux/macOS, amd64/arm64) and the release's
  `SHA256SUMS.txt`, verifies the checksum, and installs to `~/.local/bin`
  (override with `INSTALL_DIR`; pin a tag with `LLM_FILESYSTEM_VERSION`). A
  failed or stalled download or a checksum mismatch installs nothing and exits
  non-zero; the previous installer required root, a Go toolchain, and a
  checkout, and wrote to `/usr/local/bin`. The README's Install section
  documents the one-line `curl | bash` invocation, and a Support section
  states that support is best-effort via GitHub issues.
- **Reinstall over any pre-release copy.** An older `llm-filesystem` already on
  your machine — one installed to `/usr/local/bin` from before the AXI work —
  does not know `--confirm` and fails every delete routed through it. The new
  installer does not remove a stale `/usr/local/bin/llm-filesystem`; delete it
  yourself or make sure `$HOME/.local/bin` comes first on your `PATH`.

### Removed — the MCP server

- **`llm-filesystem-mcp` is gone, and so is `integrations/mcp/`.** It was a second front end that shelled out to this CLI, so every command change had to land twice, and the two drifted. AXI treats the command line as the agent interface; an agent that can run a command uses `llm-filesystem` directly. `make build` and `install.sh` now build one binary, and the README, the command reference and the Claude Code integration no longer tell you to install or register the server. Tests fail if the name comes back in the Go source, `go.mod`, the `Makefile`, `install.sh`, the CI workflows, or any Markdown, shell, JSON or YAML file other than this one.
- **The speed benchmark no longer speaks MCP to `llm-filesystem`.** Its read and tree benchmarks had been sending JSON-RPC to a server that no longer exists; they now time the CLI command, the MCP handshake benchmark and `benchmarks/mcp-client.sh` are deleted, and the README's speed and token figures were re-measured on 2026-10-31. The MCP client that still drives `fast-filesystem-mcp` now fails the run on an error response rather than timing a refused call as a read.

### Removed — a migration guide that documented an API this tool does not have

- **`docs/llm-filesystem-migration.md` is deleted.** It opened by calling this a "drop-in replacement with 100% API compatibility" and then listed its own Breaking Changes two lines later, which cannot both be true once `entries` is renamed to `items`. Checking the rest against the binary, about half of it described fields that have never existed here: `context_before`, `context_after`, `ripgrep_used`, `search_time_ms`, `continuation_token`, `auto_chunked`, `chunk_index`, `total_chunks` and `has_more` appear nowhere in the codebase. It also framed the project as a Go MCP server, which stopped being the primary interface with this release.

  A wrong map is worse than no map, and `--help` is generated from the binary and cannot drift. If migration demand appears, a short guide written from verified behaviour can replace it.

### Included

Everything that shipped as `llm-filesystem` inside `llm-tools` through mid-2026:

- **CLI (`llm-filesystem`)** — 28 commands for reading, writing, editing,
  directory operations, search, file operations, and archive handling.
- ~~**MCP server (`llm-filesystem-mcp`)**~~ — 17 batch/specialized tools under the
  `llm_filesystem_` prefix, wrapping the CLI over stdio. Removed before this
  release; see *Removed — the MCP server* above.
- Path sandboxing via `--allowed-dirs`.
- Size-aware reads with a `--max-size` budget (`0` = 70000-char default,
  `-1` = no limit). An over-budget read truncates rather than refusing — see
  the entry below.
- Pagination for large listings via `--page` and `--page-size`, reporting
  `page`, `page_size` and `total_pages`. Truncated reads resume with
  `next_offset`; there is no continuation token anywhere in the tool.
- Static single-binary builds for macOS, Linux, and Windows.

### Notes

- No behavior change from the final `llm-tools` filesystem code; this release is
  the same engine under a new module path (`github.com/samestrin/llm-filesystem-axi`).
- The full pre-extraction history lives in the `llm-tools` CHANGELOG under the
  `#### llm-filesystem` and `#### llm-filesystem-mcp` sections (first introduced
  in `llm-tools` v1.2.0).
