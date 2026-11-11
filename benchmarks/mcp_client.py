#!/usr/bin/env python3
"""MCP benchmark client for fast-filesystem-mcp, used by benchmark.sh.

Each run starts the server, sends initialize and one request over stdio, waits
for both answers and exits. benchmark.sh times the whole process, so every
MCP figure includes a server start and this client's own start.

  handshake  initialize + tools/list
  read       initialize + tools/call fast_read_file on <dir>/package.json
  tree       initialize + tools/call fast_get_directory_tree, depth 3

Exits 0 only when every request was answered with a result. --print-result
prints the second answer's text, which benchmark.sh uses outside the timed
runs to count the entries the tree returned. A refused or
failed call still produces a response; timing it would benchmark the error
path, so it fails the run instead.
"""
import argparse
import json
import subprocess
import sys
import threading


def run_mcp_test(server_cmd, requests, timeout=5):
    """Run the MCP server, send the requests, return the responses."""
    proc = subprocess.Popen(
        server_cmd,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        text=True,
        bufsize=1,
    )

    responses = []

    def reader():
        while len(responses) < len(requests):
            try:
                line = proc.stdout.readline()
                if not line:
                    break
                if line.strip():
                    responses.append(json.loads(line))
            except Exception:
                break

    t = threading.Thread(target=reader, daemon=True)
    t.start()

    for req in requests:
        proc.stdin.write(json.dumps(req) + "\n")
        proc.stdin.flush()

    t.join(timeout=timeout)
    proc.terminate()

    return responses


def failure(resp, lists_tools=False):
    """Return why a response does not count as a success, or None."""
    if "error" in resp:
        return "error"
    result = resp.get("result")
    if not isinstance(result, dict):
        return "no result"
    if result.get("isError"):
        return "isError"
    if lists_tools and resp.get("id") == 2 and not result.get("tools"):
        return "no tools listed"
    return None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--test", required=True, choices=["handshake", "read", "tree"])
    parser.add_argument("--dir", required=True)
    parser.add_argument("--print-result", action="store_true",
                        help="print the second answer's text instead of a count")
    args = parser.parse_args()

    server_cmd = ["fast-filesystem-mcp", "--allow", args.dir]

    init_req = {
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                   "clientInfo": {"name": "bench", "version": "1.0"}},
    }

    if args.test == "handshake":
        second = {"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}}
    elif args.test == "read":
        second = {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
                  "params": {"name": "fast_read_file",
                             "arguments": {"path": f"{args.dir}/package.json"}}}
    else:
        second = {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
                  "params": {"name": "fast_get_directory_tree",
                             "arguments": {"path": args.dir, "max_depth": 3}}}
    requests = [init_req, second]

    responses = run_mcp_test(server_cmd, requests)
    if not args.print_result:
        print(f"Responses: {len(responses)}/{len(requests)}")
    lists_tools = args.test == "handshake"
    failed = [(r, failure(r, lists_tools)) for r in responses if failure(r, lists_tools)]
    if failed:
        resp, why = failed[0]
        print(f"Error response ({why}): {json.dumps(resp)[:300]}", file=sys.stderr)
    if len(responses) != len(requests) or failed:
        sys.exit(1)
    if args.print_result:
        answer = next(r for r in responses if r.get("id") == 2)["result"]
        print("".join(c.get("text", "") for c in answer.get("content") or []))


if __name__ == "__main__":
    main()
