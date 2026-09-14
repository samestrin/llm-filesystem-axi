# llm-filesystem — Claude Code

The routing guidance lives in [`../cli/AGENTS.md`](../cli/AGENTS.md). It is tool-agnostic and applies to Claude Code unchanged, so it is kept in one place rather than copied here — a second copy is a second thing to forget to update.

Install it by appending that file to your project or user `CLAUDE.md`:

```bash
cat integrations/cli/AGENTS.md >> ./CLAUDE.md          # this project only
cat integrations/cli/AGENTS.md >> ~/.claude/CLAUDE.md  # every project
```

If you have registered the MCP server instead of running the binary, use [`../mcp/AGENTS.md`](../mcp/AGENTS.md). The two differ for a real reason: MCP tools take JSON arguments and cannot use `--confirm` or `--start-offset` at all, so flag-based advice is wrong for them rather than merely unhelpful.

In short: native `Read`/`Write`/`Edit` for a single file, `llm-filesystem` for batch and filesystem-specialized work, large reads truncate rather than fail, and deletes need `--confirm`.
