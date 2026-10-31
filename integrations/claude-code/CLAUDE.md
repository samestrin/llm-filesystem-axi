# llm-filesystem — Claude Code

The routing guidance lives in [`../cli/AGENTS.md`](../cli/AGENTS.md). It is tool-agnostic and applies to Claude Code unchanged, so it is kept in one place rather than copied here — a second copy is a second thing to forget to update.

Install it by appending that file to your project or user `CLAUDE.md`:

```bash
cat integrations/cli/AGENTS.md >> ./CLAUDE.md          # this project only
cat integrations/cli/AGENTS.md >> ~/.claude/CLAUDE.md  # every project
```

In short: native `Read`/`Write`/`Edit` for a single file, `llm-filesystem` for batch and filesystem-specialized work, large reads truncate rather than fail, and deletes need `--confirm`.
