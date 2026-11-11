# Demo

`llm-filesystem.gif` is rendered from `llm-filesystem.tape` with [VHS](https://github.com/charmbracelet/vhs). The GIF is committed to the repo; it is not hosted anywhere else.

The session runs about 20 seconds: an install check (`--version`), one read, one search and one `--axi` listing.

## Render it again

```bash
vhs docs/demo/llm-filesystem.tape
```

Run it from the repository root, with `llm-filesystem` (v1.0.2 or later) on your `PATH` and `vhs`, `ttyd` and `ffmpeg` installed. It rewrites `docs/demo/llm-filesystem.gif`.

Re-render whenever a command the tape shows changes. The tape builds its fixture in `/tmp/demo`.
