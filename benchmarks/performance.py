#!/usr/bin/env python3
"""Stamp and table for benchmark.sh.

  performance.py meta --out FILE --bench-dir DIR --warmup N --runs N --client-start FILE
      Records what the table's stamp needs and only the run can know: both
      tool versions, the date, the machine, the directory's file count, the
      settings, and the Python client's own start (from a hyperfine export).

  performance.py report [--dir DIR]
      Writes DIR/results-performance.md from results-meta.json and the four
      hyperfine exports in DIR (default: this script's directory). It writes
      nothing and exits 1 if any result has no timings, fewer timings than the
      run asked for, a failed exit code, or a tool missing, so a run that did
      not measure something never reaches the table.
"""
import argparse
import datetime
import json
import os
import platform
import shutil
import subprocess
import sys

TESTS = [
    # (file, row label, measured for llm-filesystem)
    ("results-coldstart.json", "Cold start", True),
    ("results-handshake.json", "MCP handshake (`initialize` + `tools/list`)", False),
    ("results-read.json", "File read (`package.json`)", True),
    ("results-tree.json", "Directory tree (depth 3, files included)", True),
]
LLM = "llm-filesystem"
MCP = "fast-filesystem-mcp"
LLM_LINK = "https://github.com/samestrin/llm-filesystem-axi"
MCP_LINK = "https://www.npmjs.com/package/fast-filesystem-mcp"


class Refused(Exception):
    pass


def first_line(cmd):
    try:
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=10).stdout
    except (OSError, subprocess.TimeoutExpired):
        return ""
    return out.strip().splitlines()[0].strip() if out.strip() else ""


def require(value, what):
    if not value:
        raise Refused(f"could not determine {what}")
    return value


def mcp_version():
    """The version in the package.json of the fast-filesystem-mcp on PATH."""
    path = shutil.which(MCP)
    if not path:
        raise Refused(f"{MCP} not found on PATH")
    d = os.path.dirname(os.path.realpath(path))
    while True:
        pkg = os.path.join(d, "package.json")
        if os.path.isfile(pkg):
            with open(pkg) as fp:
                data = json.load(fp)
            if data.get("name") == MCP:
                return data.get("version")
        parent = os.path.dirname(d)
        if parent == d:
            return None
        d = parent


def machine():
    system = platform.system()
    cpu = ram = os_name = None
    if system == "Linux":
        try:
            with open("/proc/cpuinfo") as fp:
                for line in fp:
                    if line.lower().startswith(("model name", "hardware", "cpu model")):
                        cpu = line.split(":", 1)[1].strip()
                        break
        except OSError:
            pass
        if not cpu:
            line = first_line(["sh", "-c", "lscpu | grep 'Model name'"])
            cpu = line.split(":", 1)[1].strip() if ":" in line else None
        try:
            with open("/proc/meminfo") as fp:
                for line in fp:
                    if line.startswith("MemTotal:"):
                        ram = int(line.split()[1]) * 1024
                        break
        except OSError:
            pass
        try:
            with open("/etc/os-release") as fp:
                for line in fp:
                    if line.startswith("PRETTY_NAME="):
                        os_name = line.split("=", 1)[1].strip().strip('"')
        except OSError:
            pass
        if os_name and (os.path.exists("/.dockerenv") or os.path.exists("/run/.containerenv")):
            os_name += " (container)"
    elif system == "Darwin":
        cpu = first_line(["sysctl", "-n", "machdep.cpu.brand_string"])
        mem = first_line(["sysctl", "-n", "hw.memsize"])
        ram = int(mem) if mem.isdigit() else None
        name = first_line(["sw_vers", "-productName"])
        version = first_line(["sw_vers", "-productVersion"])
        os_name = f"{name} {version}" if name and version else None
    return {
        "cpu": require(cpu, "the CPU model"),
        "threads": require(os.cpu_count(), "the thread count"),
        "ram_bytes": require(ram, "the RAM size"),
        "os": require(os_name, "the OS"),
        "kernel": f"{system} {platform.release()}",
        "arch": platform.machine(),
    }


def count_files(root):
    """Regular files under root, as `find root -type f` counts them."""
    n = 0
    for dirpath, _, files in os.walk(root):
        for f in files:
            p = os.path.join(dirpath, f)
            if os.path.isfile(p) and not os.path.islink(p):
                n += 1
    return n


def normalize(r):
    """One hyperfine result as {name, times, exit_codes, mean, stddev}.

    hyperfine 1 puts the --command-name in "command" and the wall-clock
    timings in "times". hyperfine 2 keeps the command line in "command", the
    --command-name in "name", and one entry per run in "measurements", with
    the mean and standard deviation under summary.time_wall_clock.
    """
    if "measurements" in r:
        runs = r.get("measurements") or []
        wall = (r.get("summary") or {}).get("time_wall_clock") or {}
        return {
            "name": r.get("name") or r.get("command"),
            "times": [m["time_wall_clock"]["value"] for m in runs if "time_wall_clock" in m],
            "exit_codes": [m.get("exit_code") for m in runs],
            "mean": wall.get("mean"),
            "stddev": wall.get("stddev"),
        }
    return {
        "name": r.get("command"),
        "times": r.get("times") or [],
        "exit_codes": r.get("exit_codes") or [],
        "mean": r.get("mean"),
        "stddev": r.get("stddev"),
    }


def tree_entries(node):
    """Entries below a tree node, at any depth."""
    children = node.get("children") or []
    return len(children) + sum(tree_entries(c) for c in children)


def tree_counts(bench_dir):
    """How many entries each tool's timed tree call returns in bench_dir.

    The two tools do not leave out the same names (fast-filesystem-mcp has a
    built-in exclude list), so equal settings can still be unequal work. The
    count is measured, outside the timed runs, so the table can say so.
    """
    def run(cmd, what):
        try:
            out = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
        except (OSError, subprocess.TimeoutExpired) as e:
            raise Refused(f"counting {what} tree entries: {e}")
        if out.returncode != 0:
            raise Refused(f"counting {what} tree entries: exit {out.returncode}: {out.stderr.strip()[:300]}")
        try:
            return tree_entries(json.loads(out.stdout)["tree"])
        except (ValueError, KeyError, TypeError, AttributeError) as e:
            raise Refused(f"counting {what} tree entries: unexpected output: {e}")

    client = os.path.join(os.path.dirname(os.path.abspath(__file__)), "mcp_client.py")
    return {
        LLM: run([LLM, "get-directory-tree", "--path", bench_dir, "--depth", "3",
                  "--include-files", "--format", "json"], LLM),
        MCP: run([sys.executable, client, "--test", "tree", "--dir", bench_dir,
                  "--print-result"], MCP),
    }


def check_result(raw, runs, where):
    """The normalized result, or Refused if it did not measure every run."""
    r = normalize(raw)
    if not r["times"]:
        raise Refused(f"{where}: {r['name']!r} recorded no timings")
    if len(r["times"]) != runs:
        raise Refused(f"{where}: {r['name']!r} recorded {len(r['times'])} timings, not {runs}")
    if any(code != 0 for code in r["exit_codes"]):
        raise Refused(f"{where}: {r['name']!r} has a non-zero exit code")
    if r["mean"] is None or r["stddev"] is None:
        raise Refused(f"{where}: {r['name']!r} has no mean or standard deviation")
    return r


def cmd_meta(args):
    with open(args.client_start) as fp:
        client = json.load(fp)["results"]
    if len(client) != 1:
        raise Refused("client start: expected one result")
    client = check_result(client[0], args.runs, "client start")
    meta = {
        "llm_filesystem_version": require(first_line([LLM, "--version"]), f"the {LLM} version"),
        "fast_filesystem_mcp_version": require(mcp_version(), f"the {MCP} version"),
        "node_version": require(first_line(["node", "--version"]), "the Node.js version"),
        "python_version": platform.python_version(),
        "hyperfine_version": require(first_line(["hyperfine", "--version"]), "the hyperfine version"),
        "date": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d"),
        "machine": machine(),
        "files": count_files(args.bench_dir),
        "tree_entries": tree_counts(args.bench_dir),
        "warmup": args.warmup,
        "runs": args.runs,
        "client_start": {"mean": client["mean"], "stddev": client["stddev"]},
    }
    with open(args.out, "w") as fp:
        json.dump(meta, fp, indent=2)
        fp.write("\n")


def ms(r):
    # Two decimals below 10 ms, so a small spread does not print as 0.0.
    mean, sd = r["mean"] * 1000, r["stddev"] * 1000
    d = 2 if mean < 10 else 1
    return f"{mean:.{d}f} ms ± {sd:.{d}f} ms"


def render(meta, results):
    m = meta["machine"]
    ram_gib = m["ram_bytes"] / 2**30
    client = meta["client_start"]
    llm_v, mcp_v = meta["llm_filesystem_version"], meta["fast_filesystem_mcp_version"]
    lines = [
        f"# Performance: {LLM} and {MCP}",
        "",
        "Generated by `performance.py` from the `results-*.json` files beside it; "
        "do not edit by hand. `benchmark.sh` re-runs the measurements and rewrites this file.",
        "",
        f"- **Tools:** [{LLM}]({LLM_LINK}) `{llm_v}`, a Go CLI; "
        f"[{MCP}]({MCP_LINK}) `{mcp_v}` from npm, an MCP server, on Node.js {meta['node_version']}",
        f"- **Date:** {meta['date']} (UTC)",
        f"- **Machine:** {m['cpu']}, {m['threads']} threads, {ram_gib:.1f} GiB RAM, "
        f"{m['os']}, {m['kernel']} ({m['arch']})",
        f"- **Directory:** {meta['files']:,} files (`BENCH_DIR`, with a `package.json` at its root)",
        f"- **Method:** hyperfine, warmup {meta['warmup']}, runs {meta['runs']}, "
        f"wall-clock per process invocation ({meta['hyperfine_version']}); "
        "each command runs through `sh`, whose own start hyperfine measures and subtracts; "
        "each cell is the mean ± the standard deviation. "
        f"{LLM} is a CLI, so each of its figures is one invocation of the binary. "
        f"{MCP} is reached over MCP, so its handshake, read and tree figures are each one run of "
        f"`mcp_client.py` (Python {meta['python_version']}), which starts the server, sends "
        "`initialize` and one request, and waits for both answers: each includes a server start "
        f"and the Python client's own start, measured by this run at {ms(client)}. "
        f"Cold start is `{LLM} --version`, and `{MCP}` with stdin closed, which prints its start "
        "line and exits at the end of input; each is piped to `head -1` and timed until the "
        "pipeline exits.",
        "",
        f"| Test | {LLM} `{llm_v}` | {MCP} `{mcp_v}` |",
        "|---|---:|---:|",
    ]
    for f, label, llm_measured in TESTS:
        llm = ms(results[f][LLM]) if llm_measured else "not applicable: a CLI has no MCP server"
        lines.append(f"| {label} | {llm} | {ms(results[f][MCP])} |")
    entries = meta["tree_entries"]
    lines += [
        "",
        f"- **Tree entries returned below the root:** {LLM} {entries[LLM]:,}, {MCP} {entries[MCP]:,}",
        "",
        "Raw results: " + ", ".join(f"[`{f}`]({f})" for f, _, _ in TESTS)
        + ", and the stamp above in [`results-meta.json`](results-meta.json).",
        "",
    ]
    return "\n".join(lines)


def cmd_report(args):
    with open(os.path.join(args.dir, "results-meta.json")) as fp:
        meta = json.load(fp)
    results = {}
    for f, _, llm_measured in TESTS:
        path = os.path.join(args.dir, f)
        try:
            with open(path) as fp:
                data = json.load(fp)
        except (OSError, ValueError) as e:
            raise Refused(f"{f}: {e}")
        by_tool = {}
        for raw in data.get("results") or []:
            r = check_result(raw, meta["runs"], f)
            for tool in (MCP, LLM):
                if str(r["name"]).startswith(tool):
                    by_tool[tool] = r
                    break
        for tool in ([LLM] if llm_measured else []) + [MCP]:
            if tool not in by_tool:
                raise Refused(f"{f}: no result for {tool}")
        results[f] = by_tool

    out = os.path.join(args.dir, "results-performance.md")
    tmp = out + ".tmp"
    with open(tmp, "w") as fp:
        fp.write(render(meta, results))
    os.replace(tmp, out)
    print(f"wrote {out}")


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("meta")
    p.add_argument("--out", required=True)
    p.add_argument("--bench-dir", required=True)
    p.add_argument("--warmup", type=int, required=True)
    p.add_argument("--runs", type=int, required=True)
    p.add_argument("--client-start", required=True)
    p = sub.add_parser("report")
    p.add_argument("--dir", default=os.path.dirname(os.path.abspath(__file__)))
    args = parser.parse_args()
    try:
        cmd_meta(args) if args.cmd == "meta" else cmd_report(args)
    except Refused as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
