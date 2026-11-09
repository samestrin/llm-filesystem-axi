Benchmark Results: llm-filesystem (Go) vs fast-filesystem-mcp (TypeScript)

| Benchmark      | Go     | TypeScript | Speedup      | Speedup with the Python client's start removed |
|----------------|--------|------------|--------------|------------------------------------------------|
| Cold Start     | 1.9ms  | 150.1ms    | 78x faster   | 78x faster — no client in this one             |
| File Read      | 1.9ms  | 194.3ms    | 103x faster  | 91x faster — 172.3ms against 1.9ms             |
| Directory Tree | 6.1ms  | 176.7ms    | 29x faster   | 25x faster — 154.7ms against 6.1ms             |

Run on 2026-10-31 on an Intel Xeon W-2295 (36 threads), 141 GB RAM, Debian 13 container on Linux 7.0 (x86_64), Node 24, against fast-filesystem-mcp 3.5.2, with `BENCH_DIR` set to a directory of 3,697 files.

llm-filesystem is a CLI, so each figure is one process invocation. fast-filesystem-mcp is reachable only over MCP, so its read and tree figures are a server start, an initialize and one tool call, driven by a Python client whose own start costs about 22ms of each. Each Go figure is a bare binary invocation with no equivalent cost, so the raw `Speedup` column flatters Go by that 22ms; the last column takes it off, which is the per-operation comparison — 194.3ms becomes 172.3ms, so a read is 91x rather than 103x. `benchmark.sh` measures the client's start on every run and prints both figures, so a reader does not have to trust this number from another machine. The client fails the run if the server answers with an error, so a refused call is never timed as a read.

Usage

# Run full benchmark (default: 3 warmup, 10 runs)
cd benchmarks && ./benchmark.sh

# Quick benchmark
./benchmark.sh 2 5

# Custom runs
./benchmark.sh <warmup> <runs>

# Test directory (must contain a package.json)
BENCH_DIR=/path/to/project ./benchmark.sh

The results are saved as JSON in benchmarks/results-*.json for further analysis.

For the token benchmark behind the README's token-reduction figures (TOON with a minimal field set vs full JSON), run ./tokens.sh; its generated report is results-tokens.md.
