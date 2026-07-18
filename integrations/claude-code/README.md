# Claude Code integration

Two opt-in integrations that make `llm-filesystem` available ambiently in Claude
Code, following the AXI "ambient context" principle: a session integration first,
then an on-demand skill.

Both are optional and independent. They position llm-filesystem as a **complement**
to Claude's native `Read`/`Write`/`Edit` — single-file operations stay on the
native tools; llm-filesystem handles batch and specialized work.

## 1. MCP server (session integration)

Register the MCP server in your Claude Code config so the `llm_filesystem_*`
tools are available every session:

```json
{
  "mcpServers": {
    "llm-filesystem": {
      "command": "/usr/local/bin/llm-filesystem-mcp"
    }
  }
}
```

Both binaries must be installed (the MCP server shells out to the CLI). See the
project [README](../../README.md) for install steps. To restrict access, add
`"args": ["--allowed-dirs", "/path/a,/path/b"]`.

## 2. Routing rules (ambient guidance)

Append [`CLAUDE.md`](CLAUDE.md) to your project or user `CLAUDE.md`. It tells
Claude when to prefer llm-filesystem (batch/specialized) versus the native tools
(single-file), so the right tool is chosen without being asked.

## 3. On-demand skill

Copy the skill into your skills directory so Claude can load detailed usage on
demand:

```bash
mkdir -p ~/.claude/skills/llm-filesystem
cp skill/SKILL.md ~/.claude/skills/llm-filesystem/SKILL.md
```

(Use a project-level `.claude/skills/` instead of `~/.claude/` to scope it to one
project.)
