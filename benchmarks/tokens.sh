#!/usr/bin/env bash
#
# Measure what AXI actually claims: tokens, not milliseconds.
#
# The README states a token reduction versus full JSON. Nothing measured it
# until this script existed, which made the boldest number in the README the
# only one with no evidence behind it. This produces that number.
#
# Baseline is `--full --format json`, because that is byte-identical to the
# pre-AXI output and is therefore the honest "before" case. Every other mode is
# compared against it on identical input.
#
# Counting uses tiktoken's o200k_base through `uv run`, so nothing is installed
# globally. That is a GPT-family tokenizer, not Claude's; exact counts differ by
# a few percent between tokenizers, but the RATIO between two encodings of the
# same data is what this reports and that is stable across both.
#
# Usage:  ./benchmarks/tokens.sh [target-directory]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/build/llm-filesystem"

# Every command echoes the path it was given, and for search-code that path is
# repeated once per match - 569 times on this repository - so the absolute
# location of the checkout can move the total by a third. A published number
# nobody else can reproduce is worse than no number. Making a repo-internal
# target relative is not enough on its own: the binary absolutizes --path
# (NormalizePath) and prints the absolute checkout path anyway. So the run
# happens from $ROOT AND every captured output has the checkout prefix replaced
# with the fixed placeholder /repo before counting, which makes the counted
# figure identical for a reader no matter where their clone lives.
# A target outside the repository (/usr/bin, /usr/share) keeps its absolute
# path, which is short and identical on every machine.
TARGET="${1:-$ROOT/internal}"
TARGET="$(cd "$TARGET" 2>/dev/null && pwd || echo "$TARGET")"
cd "$ROOT"
case "$TARGET" in
  "$ROOT")   TARGET="." ;;
  "$ROOT"/*) TARGET="${TARGET#"$ROOT"/}" ;;
esac

# $ROOT escaped for use as a sed regex, for the placeholder substitution above.
ROOT_RE=$(printf '%s' "$ROOT" | sed 's/[][\.*^$/]/\\&/g')

if [ ! -x "$BIN" ]; then
  echo "building $BIN first..." >&2
  make -C "$ROOT" build >/dev/null
fi

command -v uv >/dev/null || { echo "uv is required (brew install uv)" >&2; exit 1; }

# One python process counts every file, so the BPE table is loaded once rather
# than once per measurement.
count_tokens() {
  uv run --quiet --with tiktoken python3 -c '
import sys, tiktoken
enc = tiktoken.get_encoding("o200k_base")
for path in sys.argv[1:]:
    with open(path, "rb") as fh:
        print(len(enc.encode(fh.read().decode("utf-8", "replace"))))
' "$@"
}

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Each row is: label, then the command's flags. The command itself is held
# constant within a row group so only the output mode varies.
declare -a CASES=(
  "list-directory|--path $TARGET"
  "get-directory-tree|--path $TARGET --depth 3"
  "search-code|--path $TARGET --pattern func"
  # Relative, and run from $ROOT. read-file echoes the path it was given
  # verbatim rather than absolutizing it, so this row never depended on
  # where the repository happens to live.
  "read-file|--path go.mod"
)

declare -a MODES=(
  "json-full|--full --format json"
  "json-min|--format json"
  "toon-full|--full"
  "toon-min|"
)

echo "target:    $TARGET"
echo "tokenizer: tiktoken o200k_base"
echo "baseline:  json-full (byte-identical to the pre-AXI --json output)"
echo
printf "%-20s %10s %10s %10s %10s %12s\n" "command" "json-full" "json-min" "toon-full" "toon-min" "reduction"

for case in "${CASES[@]}"; do
  cmd="${case%%|*}"
  args="${case#*|}"

  files=()
  for mode in "${MODES[@]}"; do
    label="${mode%%|*}"
    flags="${mode#*|}"
    out="$WORK/$cmd.$label"
    # word-splitting on $flags and $args is intended: they are flag lists
    # shellcheck disable=SC2086
    if ! "$BIN" "$cmd" $args $flags > "$out" 2>/dev/null; then
      echo "error: $cmd ($label) exited non-zero; refusing to count a failed invocation" >&2
      exit 1
    fi
    if [ ! -s "$out" ]; then
      echo "error: $cmd ($label) produced no output; refusing to count an empty file" >&2
      exit 1
    fi
    # Normalize the absolute checkout prefix (the binary absolutizes --path
    # and echoes it) to a fixed placeholder, so the count does not depend on
    # where this clone lives. Portable: no sed -i, which differs BSD vs GNU.
    sed "s|$ROOT_RE|/repo|g" "$out" > "$out.norm"
    mv "$out.norm" "$out"
    files+=("$out")
  done

  # while-read rather than mapfile, which stock macOS bash 3.2 does not have.
  counts=()
  while IFS= read -r line; do
    counts+=("$line")
  done < <(count_tokens "${files[@]}")
  base="${counts[0]}"
  min="${counts[3]}"
  if [ "$base" -gt 0 ]; then
    pct=$(awk -v b="$base" -v m="$min" 'BEGIN{printf "%.1f", (1 - m/b) * 100}')
  else
    pct="n/a"
  fi
  printf "%-20s %10s %10s %10s %10s %11s%%\n" \
    "$cmd" "${counts[0]}" "${counts[1]}" "${counts[2]}" "${counts[3]}" "$pct"
done
