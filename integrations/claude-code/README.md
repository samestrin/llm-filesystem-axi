# Claude Code integration

`llm-filesystem` is a CLI, and Claude Code can run a CLI. That is the whole integration — there is nothing to register.

What follows is optional: telling Claude *when* to reach for it, and giving it somewhere to look up detail on demand. This is the AXI "ambient context" principle — session guidance first, an on-demand skill second.

It is a **complement** to Claude's native `Read`/`Write`/`Edit`, not a replacement. Single-file work stays on the native tools.

## 1. Setup

Run in your project:

```bash
llm-filesystem setup
```

This merges a SessionStart hook that runs `llm-filesystem` into `.claude/settings.json` (keeping every other key and hook), and writes the on-demand skill to `.claude/skills/llm-filesystem/SKILL.md`, so Claude can load worked examples only when it needs them rather than paying for them every session. It is idempotent, never prompts, writes only under the working directory, and supports `--dry-run` to preview:

```bash
llm-filesystem setup --dry-run
```

## 2. Routing guidance

The hook above prints the working directory at session start. For routing guidance — so Claude reaches for the right tool without being asked — append [`../cli/AGENTS.md`](../cli/AGENTS.md) to your project or user `CLAUDE.md`. Without a clone of this repo, fetch it straight from GitHub:

```bash
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/integrations/cli/AGENTS.md >> ./CLAUDE.md          # this project only
curl -fsSL https://raw.githubusercontent.com/samestrin/llm-filesystem-axi/main/integrations/cli/AGENTS.md >> ~/.claude/CLAUDE.md  # every project
```

It lists all 29 commands grouped by task, so an agent can route without first running a discovery command. A test compares that list against the binary in both directions, so it cannot quietly go stale.

`AGENTS.md` is the conventional filename, so the same file also works for any other agent that reads one. [`CLAUDE.md`](CLAUDE.md) here just points at it.
