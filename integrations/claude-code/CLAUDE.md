# llm-filesystem — Claude Code

The routing guidance lives in [`../cli/AGENTS.md`](../cli/AGENTS.md). It is tool-agnostic and applies to Claude Code unchanged, so it is kept in one place rather than copied here — a second copy is a second thing to forget to update.

Run `llm-filesystem setup` in your project to register the SessionStart hook and the on-demand skill, and append the routing file to your project or user `CLAUDE.md`. Without a clone of this repo:

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/integrations/cli/AGENTS.md >> ./CLAUDE.md          # this project only
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/integrations/cli/AGENTS.md >> ~/.claude/CLAUDE.md  # every project
```

In short: native `Read`/`Write`/`Edit` for a single file, `llm-filesystem` for batch and filesystem-specialized work, large reads truncate rather than fail, and deletes need `--confirm`.
