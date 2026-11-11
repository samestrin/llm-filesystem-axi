#!/bin/bash
# Benchmark: llm-filesystem (Go CLI) and fast-filesystem-mcp (MCP server on Node.js)
#
# Measures:
# 1. Cold start time (time to first output)
# 2. MCP handshake (initialize + tools/list; fast-filesystem-mcp only)
# 3. File read operation
# 4. Directory tree operation
#
# llm-filesystem is a CLI, so each operation is one process invocation, and it
# has no MCP server to hand-shake with. fast-filesystem-mcp is only reachable
# over MCP, so each of its operations is a server start, an initialize and one
# request, driven by mcp_client.py. That is what an agent pays per operation
# with each tool.
#
# When all four have run, performance.py writes results-performance.md from
# the JSON. Results are written to a staging directory first and copied into
# place only when every run recorded its timings and the table was written, so
# a failed run leaves the previous results untouched.
#
# Usage: ./benchmark.sh [warmup_runs] [benchmark_runs]
#
# BENCH_DIR selects the test directory; it must contain a package.json.
# BENCH_OUT selects where results go (default: this directory).

set -e

# Every hyperfine call names its shell. hyperfine 2 refuses a command with
# shell syntax unless one is chosen, and sh is what hyperfine 1 used by
# default; hyperfine measures that shell's own start and subtracts it.

WARMUP=${1:-3}
RUNS=${2:-10}
TEST_DIR="${BENCH_DIR:-$HOME/Documents/GitHub/llm-interface}"
BENCHMARK_DIR="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${BENCH_OUT:-$BENCHMARK_DIR}"
MCP_CLIENT="$BENCHMARK_DIR/mcp_client.py"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}=== Filesystem Benchmark ===${NC}"
echo "Test directory: $TEST_DIR"
echo "Files in test dir: $(find "$TEST_DIR" -type f 2>/dev/null | wc -l | tr -d ' ')"
echo "Warmup runs: $WARMUP"
echo "Benchmark runs: $RUNS"
echo ""

if [ ! -f "$TEST_DIR/package.json" ]; then
    echo "Error: $TEST_DIR/package.json not found (set BENCH_DIR)"
    exit 1
fi

# Check tools
for cmd in llm-filesystem fast-filesystem-mcp hyperfine python3 node; do
    if ! command -v $cmd &> /dev/null; then
        echo "Error: $cmd not found"
        exit 1
    fi
done

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

# ============================================================================
# Benchmark 1: Cold Start Time
# ============================================================================
echo -e "${GREEN}Benchmark 1: Cold Start Time${NC}"
echo "Measures time for the process to start, print its first line and exit."
echo ""

hyperfine \
    --shell=sh \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$STAGE/results-coldstart.json" \
    --command-name "llm-filesystem" \
    "llm-filesystem --version 2>&1 | head -1" \
    --command-name "fast-filesystem-mcp" \
    "fast-filesystem-mcp < /dev/null 2>&1 | head -1"

echo ""

# ============================================================================
# Benchmark 2: MCP Handshake
# ============================================================================
echo -e "${GREEN}Benchmark 2: MCP Initialize + Tools List${NC}"
echo "Measures a server start, initialize and tools/list (llm-filesystem has no MCP server)."
echo ""

hyperfine \
    --shell=sh \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$STAGE/results-handshake.json" \
    --command-name "fast-filesystem-mcp" \
    "python3 '$MCP_CLIENT' --test handshake --dir '$TEST_DIR'"

echo ""

# ============================================================================
# Benchmark 3: File Read
# ============================================================================
echo -e "${GREEN}Benchmark 3: Read File${NC}"
echo "Measures one read of package.json (fast-filesystem-mcp: server start + init + read)."
echo ""

hyperfine \
    --shell=sh \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$STAGE/results-read.json" \
    --command-name "llm-filesystem" \
    "llm-filesystem read-file --path '$TEST_DIR/package.json' > /dev/null" \
    --command-name "fast-filesystem-mcp" \
    "python3 '$MCP_CLIENT' --test read --dir '$TEST_DIR'"

echo ""

# ============================================================================
# Benchmark 4: Directory Tree
# ============================================================================
echo -e "${GREEN}Benchmark 4: Directory Tree (depth 3)${NC}"
echo "Measures one directory tree with files (fast-filesystem-mcp: server start + init + tree)."
echo ""

hyperfine \
    --shell=sh \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$STAGE/results-tree.json" \
    --command-name "llm-filesystem" \
    "llm-filesystem get-directory-tree --path '$TEST_DIR' --depth 3 --include-files > /dev/null" \
    --command-name "fast-filesystem-mcp" \
    "python3 '$MCP_CLIENT' --test tree --dir '$TEST_DIR'"

echo ""

# ============================================================================
# The Python client's own start
# ============================================================================
# Every fast-filesystem-mcp figure above but cold start is one Python client
# process, so the interpreter's start and the client's imports are inside each
# of them, while each llm-filesystem figure is a bare binary invocation. The
# table states that cost, measured here on this machine, rather than leaving
# the reader to guess it.
echo -e "${GREEN}Python client start${NC}"
echo ""

hyperfine \
    --shell=sh \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$STAGE/client-start.json" \
    --command-name "python3 client start" \
    "python3 -c 'import argparse, json, subprocess, sys, threading'"

echo ""

# ============================================================================
# Stamp, table, and copy into place
# ============================================================================
python3 "$BENCHMARK_DIR/performance.py" meta \
    --out "$STAGE/results-meta.json" \
    --bench-dir "$TEST_DIR" \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --client-start "$STAGE/client-start.json"
python3 "$BENCHMARK_DIR/performance.py" report --dir "$STAGE"

mkdir -p "$OUT_DIR"
for f in results-coldstart.json results-handshake.json results-read.json \
         results-tree.json results-meta.json results-performance.md; do
    cp "$STAGE/$f" "$OUT_DIR/$f"
done

echo -e "${BLUE}=== Benchmark Complete ===${NC}"
echo ""
cat "$OUT_DIR/results-performance.md"
