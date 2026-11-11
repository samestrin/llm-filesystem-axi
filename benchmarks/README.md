# Benchmarks: llm-filesystem and fast-filesystem-mcp

The current results are in [results-performance.md](results-performance.md): cold start, MCP handshake, file read and directory tree for both tools, with both versions, the date, the machine, the directory's file count and the method stamped above the table. That file is generated from the raw hyperfine exports beside it (`results-coldstart.json`, `results-handshake.json`, `results-read.json`, `results-tree.json`) and the run's stamp (`results-meta.json`); do not edit it by hand.

llm-filesystem is a CLI, so each of its figures is one process invocation, and it has no MCP server to hand-shake with. fast-filesystem-mcp is reachable only over MCP, so its handshake, read and tree figures are each one run of `mcp_client.py`, which starts the server, sends `initialize` and one request, and waits for both answers. Each of those figures therefore includes a server start and the Python client's own start; the script measures that start on every run and the table states it.

The tree row gives both tools the same settings (depth 3, files included, hidden entries left out), but they do not leave out the same names: fast-filesystem-mcp 3.5.2 skips `node_modules`, `.git`, `.venv` and the rest of the `DEFAULT_EXCLUDE_PATTERNS` list in its `dist/index.js`, and its tree call has no option to turn that off. So the run also counts the entries each tool's tree returned, and the table prints both counts under it. In a directory that is mostly `node_modules`, such as the npm scratch directory below, the two figures are not the same work, so point `BENCH_DIR` at a directory without `node_modules`: the published table uses a copy of this repository's source (`git archive HEAD`) plus the `package.json` npm wrote.

## Requirements

`hyperfine` (1.x or 2.x), `python3`, `node`, `llm-filesystem` and `fast-filesystem-mcp` on `PATH`, and a test directory with a `package.json` at its root. One way to get them without a global install:

```bash
cargo install hyperfine --root "$SCRATCH"
npm install --prefix "$SCRATCH" fast-filesystem-mcp
export PATH="$SCRATCH/bin:$SCRATCH/node_modules/.bin:$PATH"
mkdir -p "$SCRATCH/src" && git archive HEAD | tar -x -C "$SCRATCH/src" && cp "$SCRATCH/package.json" "$SCRATCH/src/"
export BENCH_DIR="$SCRATCH/src"   # same files for both tools: no node_modules
```

## Usage

```bash
# Run all four tests and rewrite results-performance.md (default: 3 warmup, 10 runs)
BENCH_DIR=/path/to/project ./benchmarks/benchmark.sh

# Other settings; the table records the ones used
./benchmarks/benchmark.sh <warmup> <runs>

# Write the results somewhere else instead of benchmarks/
BENCH_OUT=/path/to/dir ./benchmarks/benchmark.sh

# Rewrite results-performance.md from the committed JSON without re-measuring
python3 benchmarks/performance.py report
```

The script fails, and leaves the previous results untouched, when any run records no timings, a command exits non-zero, or the MCP server answers with an error (a refused call still produces a response, and timing it would publish the error path as a read). `benchmark_test.go` checks those failures against stand-ins in `testdata/`, since CI runs no benchmark.

For the token benchmark behind the token-reduction figures in [docs/background.md](../docs/background.md) (TOON with a minimal field set vs full JSON), run ./tokens.sh; its generated report is results-tokens.md.
