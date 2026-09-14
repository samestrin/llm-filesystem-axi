# llm-filesystem — Claude Code

The routing guidance lives in [`../AGENTS.md`](../AGENTS.md). It is tool-agnostic and applies to Claude Code unchanged, so it is kept in one place rather than copied here — a second copy is a second thing to forget to update.

Install it by appending that file to your project or user `CLAUDE.md`:

```bash
cat integrations/AGENTS.md >> ./CLAUDE.md          # this project only
cat integrations/AGENTS.md >> ~/.claude/CLAUDE.md  # every project
```

In short: native `Read`/`Write`/`Edit` for a single file, `llm-filesystem` for batch and filesystem-specialized work, large reads truncate rather than fail, and deletes need `--confirm`. `AGENTS.md` explains why, and tells the agent to ask the binary itself for anything else.

> This file used to carry a table of every MCP tool name. It was removed on purpose: a list in a document drifts from the binary, and `llm-filesystem` with no arguments already answers what it is and what is here.
