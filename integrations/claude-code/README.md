# Claude Code integration

`llm-filesystem` is a CLI, and Claude Code can run a CLI. That is the whole integration — there is nothing to register.

What follows is optional: telling Claude *when* to reach for it, and giving it somewhere to look up detail on demand. This is the AXI "ambient context" principle — session guidance first, an on-demand skill second.

It is a **complement** to Claude's native `Read`/`Write`/`Edit`, not a replacement. Single-file work stays on the native tools.

## 1. Routing guidance

Append [`../cli/AGENTS.md`](../cli/AGENTS.md) to your project or user `CLAUDE.md`, so Claude reaches for the right tool without being asked:

```bash
cat integrations/cli/AGENTS.md >> ./CLAUDE.md          # this project only
cat integrations/cli/AGENTS.md >> ~/.claude/CLAUDE.md  # every project
```

It lists all 28 commands grouped by task, so an agent can route without first running a discovery command. A test compares that list against the binary in both directions, so it cannot quietly go stale.

`AGENTS.md` is the conventional filename, so the same file also works for any other agent that reads one. [`CLAUDE.md`](CLAUDE.md) here just points at it.

## 2. On-demand skill

Copy the skill in so Claude can load worked examples only when it needs them, rather than paying for them every session:

```bash
mkdir -p ~/.claude/skills/llm-filesystem
cp skill/SKILL.md ~/.claude/skills/llm-filesystem/SKILL.md
```

Use a project-level `.claude/skills/` instead of `~/.claude/` to scope it to one project.
