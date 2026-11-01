# AXI catalog entry for llm-filesystem

One row, in the catalog's table shape, for the `kunchenguid/axi` community catalog:

```
| [`llm-filesystem-axi`](https://github.com/samestrin/llm-filesystem-axi) | samestrin | Filesystem | Agent-ergonomic filesystem CLI for AI agents: 28 commands for reading, writing, editing and searching files, one static Go binary, TOON output, AXI 10/10. |
```

Hand-off: in the `kunchenguid/axi` repo, the owner adds an entry to the `community` list in `catalog.yaml` (name: `llm-filesystem-axi`, url: `https://github.com/samestrin/llm-filesystem-axi`, author: `samestrin`, domain: `Filesystem`, description as above) and runs `pnpm run docs:gen` to regenerate the tables — `catalog.yaml` is the single source of truth there, the README/docs tables are generated, and `docs-check` fails on hand-edited generated regions. The owner commits `catalog.yaml`, `README.md` and `docs/index.html` together in that PR. No CI, tag or release action follows from this file; that PR is the whole task.
