# Claude Code integration

`llm-filesystem` is a CLI, and Claude Code can run a CLI. That is the whole integration — there is nothing to register.

What follows is optional: telling Claude *when* to reach for it, and giving it somewhere to look up details on demand. This is the AXI "ambient context" principle — session guidance first, an on-demand skill second.

It is a **complement** to Claude's native `Read`/`Write`/`Edit`, not a replacement. Single-file work stays on the native tools.

## 1. Routing guidance

Append [`../AGENTS.md`](../AGENTS.md) to your project or user `CLAUDE.md`, so Claude reaches for the right tool without being asked:

```bash
cat integrations/AGENTS.md >> ./CLAUDE.md          # this project only
cat integrations/AGENTS.md >> ~/.claude/CLAUDE.md  # every project
```

`AGENTS.md` is the canonical copy and is tool-agnostic, so the same file works for other agents that read `AGENTS.md`. [`CLAUDE.md`](CLAUDE.md) in this directory just points at it.

## 2. On-demand skill

Copy the skill in so Claude can load detailed usage only when it needs it, rather than paying for it every session:

```bash
mkdir -p ~/.claude/skills/llm-filesystem
cp skill/SKILL.md ~/.claude/skills/llm-filesystem/SKILL.md
```

Use a project-level `.claude/skills/` instead of `~/.claude/` to scope it to one project.

## 3. MCP server (optional, for clients without a shell)

Claude Code does not need this — it can run the binary directly, and the MCP server only shells out to that same binary. Register it if you want the tools available to a client that cannot run a CLI:

```json
{
  "mcpServers": {
    "llm-filesystem": {
      "command": "/usr/local/bin/llm-filesystem-mcp"
    }
  }
}
```

Both binaries must be installed; see the project [README](../../README.md). To restrict access, add `"args": ["--allowed-dirs", "/path/a,/path/b"]`.

Registering it costs a tool schema per session whether the tools get used or not, which is the per-session cost the on-demand skill above exists to avoid. If you can run the CLI, prefer it.
