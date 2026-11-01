#!/bin/bash
# Benchmark: llm-filesystem (Go) vs fast-filesystem-mcp (TypeScript)
#
# Measures:
# 1. Cold start time (time to first output)
# 2. File read operation
# 3. Directory tree operation
#
# llm-filesystem is a CLI, so each operation is one process invocation.
# fast-filesystem-mcp is only reachable over MCP, so each of its operations is
# a server start, an initialize and one tools/call. That is what an agent pays
# per operation with each tool.
#
# Usage: ./benchmark.sh [warmup_runs] [benchmark_runs]
#
# BENCH_DIR selects the test directory; it must contain a package.json.

set -e

WARMUP=${1:-3}
RUNS=${2:-10}
TEST_DIR="${BENCH_DIR:-$HOME/Documents/GitHub/llm-interface}"
BENCHMARK_DIR="$(dirname "$0")"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
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
for cmd in llm-filesystem fast-filesystem-mcp hyperfine python3; do
    if ! command -v $cmd &> /dev/null; then
        echo "Error: $cmd not found"
        exit 1
    fi
done

# Versions
echo -e "${GREEN}Versions:${NC}"
echo "  llm-filesystem: $(llm-filesystem --version 2>&1 | head -1)"
echo "  fast-filesystem-mcp: $(fast-filesystem-mcp --version 2>&1 | head -1)"
echo ""

# ============================================================================
# Create Python MCP test client (fast-filesystem-mcp only)
# ============================================================================

MCP_CLIENT="$(mktemp)"
trap 'rm -f "$MCP_CLIENT"' EXIT

# MCP client that sends requests and reads responses
cat > "$MCP_CLIENT" << 'PYSCRIPT'
#!/usr/bin/env python3
"""MCP Benchmark Client - sends requests and measures response time."""
import subprocess
import json
import sys
import threading
import time

def run_mcp_test(server_cmd, requests, timeout=5):
    """Run MCP server, send requests, return the responses."""
    proc = subprocess.Popen(
        server_cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
        bufsize=1
    )

    responses = []

    def reader():
        while len(responses) < len(requests):
            try:
                line = proc.stdout.readline()
                if line.strip():
                    responses.append(json.loads(line))
            except:
                break

    t = threading.Thread(target=reader, daemon=True)
    t.start()

    # Send all requests
    for req in requests:
        proc.stdin.write(json.dumps(req) + "\n")
        proc.stdin.flush()

    # Wait for responses
    t.join(timeout=timeout)
    proc.terminate()

    return responses

if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("--test", required=True, choices=["read", "tree"])
    parser.add_argument("--dir", required=True)
    args = parser.parse_args()

    server_cmd = ["fast-filesystem-mcp", "--allow", args.dir]

    # Build requests based on test type
    init_req = {
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                   "clientInfo": {"name": "bench", "version": "1.0"}}
    }

    if args.test == "read":
        requests = [
            init_req,
            {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
             "params": {"name": "fast_read_file", "arguments": {"path": f"{args.dir}/package.json"}}}
        ]
    elif args.test == "tree":
        requests = [
            init_req,
            {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
             "params": {"name": "fast_get_directory_tree", "arguments": {"path": args.dir, "max_depth": 3}}}
        ]

    responses = run_mcp_test(server_cmd, requests)
    print(f"Responses: {len(responses)}/{len(requests)}")
    # A refused or failed call still produces a response; timing it would
    # benchmark the error path, so it fails the run instead.
    failed = [r for r in responses
              if "error" in r or r.get("result", {}).get("isError")]
    if failed:
        print(f"Error response: {json.dumps(failed[0])[:300]}", file=sys.stderr)
    sys.exit(0 if len(responses) == len(requests) and not failed else 1)
PYSCRIPT

# ============================================================================
# Benchmark 1: Cold Start Time
# ============================================================================
echo -e "${GREEN}Benchmark 1: Cold Start Time${NC}"
echo "Measures time for the process to start and print its first line."
echo ""

hyperfine \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$BENCHMARK_DIR/results-coldstart.json" \
    --command-name "llm-filesystem (Go)" \
    "llm-filesystem --version 2>&1 | head -1" \
    --command-name "fast-filesystem-mcp (TS)" \
    "fast-filesystem-mcp < /dev/null 2>&1 | head -1"

echo ""

# ============================================================================
# Benchmark 2: File Read
# ============================================================================
echo -e "${GREEN}Benchmark 2: Read File${NC}"
echo "Measures one read of package.json (TS: server start + init + read)."
echo ""

hyperfine \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$BENCHMARK_DIR/results-read.json" \
    --command-name "llm-filesystem (Go)" \
    "llm-filesystem read-file --path '$TEST_DIR/package.json' > /dev/null" \
    --command-name "fast-filesystem-mcp (TS)" \
    "python3 '$MCP_CLIENT' --test read --dir '$TEST_DIR'"

echo ""

# ============================================================================
# Benchmark 3: Directory Tree
# ============================================================================
echo -e "${GREEN}Benchmark 3: Directory Tree (depth 3)${NC}"
echo "Measures one directory tree (TS: server start + init + tree)."
echo ""

hyperfine \
    --warmup "$WARMUP" \
    --runs "$RUNS" \
    --export-json "$BENCHMARK_DIR/results-tree.json" \
    --command-name "llm-filesystem (Go)" \
    "llm-filesystem get-directory-tree --path '$TEST_DIR' --depth 3 > /dev/null" \
    --command-name "fast-filesystem-mcp (TS)" \
    "python3 '$MCP_CLIENT' --test tree --dir '$TEST_DIR'"

echo ""

# ============================================================================
# Summary
# ============================================================================
echo -e "${BLUE}=== Benchmark Complete ===${NC}"
echo ""
echo "Results saved to:"
ls -1 "$BENCHMARK_DIR"/results-*.json 2>/dev/null | while read f; do echo "  - $f"; done
echo ""
echo -e "${YELLOW}Quick Summary:${NC}"

# Parse and display results
python3 << PYSUMMARY
import json
import os
import glob

benchmark_dir = "$BENCHMARK_DIR"
results = {}

for f in sorted(glob.glob(os.path.join(benchmark_dir, "results-*.json"))):
    name = os.path.basename(f).replace("results-", "").replace(".json", "")
    with open(f) as fp:
        data = json.load(fp)

    go_time = ts_time = None
    for r in data["results"]:
        if "Go" in r["command"]:
            go_time = r["mean"] * 1000
            go_std = r["stddev"] * 1000
        else:
            ts_time = r["mean"] * 1000
            ts_std = r["stddev"] * 1000

    if go_time and ts_time:
        speedup = ts_time / go_time
        print(f"\n{name}:")
        print(f"  llm-filesystem (Go):    {go_time:7.1f}ms ± {go_std:.1f}ms")
        print(f"  fast-filesystem (TS):   {ts_time:7.1f}ms ± {ts_std:.1f}ms")
        print(f"  Go is {speedup:.1f}x faster")
PYSUMMARY

echo ""
echo "Done!"
