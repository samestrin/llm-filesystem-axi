package benchmarks

// CI runs no benchmark: it has neither hyperfine nor fast-filesystem-mcp, and
// its timings would mean nothing. These tests run the real benchmark.sh and
// performance.py against stand-ins in testdata/ instead, so the parts that
// decide whether a run is believed are checked on every change: a run that
// records no timings, or an MCP server that answers with an error, has to fail
// the script rather than land in the published table.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var resultFiles = []string{
	"results-coldstart.json",
	"results-handshake.json",
	"results-read.json",
	"results-tree.json",
}

func needTools(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("benchmark.sh is a bash script")
	}
	for _, tool := range []string{"bash", "python3", "node"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
}

// runBench runs benchmark.sh with the stand-ins first on PATH, writing into
// outDir, and returns its combined output and whether it exited 0.
func runBench(t *testing.T, outDir string, env ...string) (string, bool) {
	t.Helper()
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	benchDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(benchDir, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(benchDir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("bash", "benchmark.sh", "1", "3")
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(testdata, "bin")+":"+
			filepath.Join(testdata, "fast-filesystem-mcp", "bin")+":"+os.Getenv("PATH"),
		"BENCH_DIR="+benchDir,
		"BENCH_OUT="+outDir,
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func runReport(t *testing.T, dir string) (string, bool) {
	t.Helper()
	out, err := exec.Command("python3", "performance.py", "report", "--dir", dir).CombinedOutput()
	return string(out), err == nil
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// One command runs all four tests and writes the table, and the table carries
// what a reader needs to judge the numbers: both versions with links, the
// date, the machine, the file count and the method. A table without its stamp
// is a number nobody can check.
func TestBenchmarkRunsAllFourTestsAndWritesTheTable(t *testing.T) {
	needTools(t)
	out := t.TempDir()
	before := time.Now().UTC().Format("2006-01-02")
	log, ok := runBench(t, out)
	after := time.Now().UTC().Format("2006-01-02")
	if !ok {
		t.Fatalf("benchmark.sh failed:\n%s", log)
	}

	for _, f := range resultFiles {
		var data struct {
			Results []struct {
				Command string    `json:"command"`
				Times   []float64 `json:"times"`
			} `json:"results"`
		}
		if err := json.Unmarshal([]byte(readFile(t, filepath.Join(out, f))), &data); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if len(data.Results) == 0 {
			t.Errorf("%s has no results", f)
		}
	}

	table := readFile(t, filepath.Join(out, "results-performance.md"))
	for _, want := range []string{
		"[llm-filesystem](https://github.com/samestrin/llm-filesystem-axi) `0.0.0-fixture`",
		"[fast-filesystem-mcp](https://www.npmjs.com/package/fast-filesystem-mcp) `0.0.0-fixture`",
		"- **Machine:** ",
		" threads, ",
		" RAM, ",
		"- **Directory:** 3 files",
		"hyperfine, warmup 1, runs 3",
		"wall-clock per process invocation",
		"server start",
		"Python client's own start",
		"| Cold start |",
		"| MCP handshake (`initialize` + `tools/list`) |",
		"| File read (`package.json`) |",
		"| Directory tree (depth 3) |",
	} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if !strings.Contains(table, "- **Date:** "+before) && !strings.Contains(table, "- **Date:** "+after) {
		t.Errorf("table does not carry today's date:\n%s", table)
	}
	// The table states numbers, not verdicts on them.
	for _, word := range []string{"faster", "slower", "speedup", "Speedup"} {
		if strings.Contains(table, word) {
			t.Errorf("table says %q; it carries no speed adjective:\n%s", word, table)
		}
	}
}

// The table is regenerated from the JSON, not edited: a second run of the
// generator alone rewrites it byte for byte, and a second run of the script
// replaces a hand edit.
func TestTableIsRewrittenFromTheJSON(t *testing.T) {
	needTools(t)
	out := t.TempDir()
	if log, ok := runBench(t, out); !ok {
		t.Fatalf("benchmark.sh failed:\n%s", log)
	}
	path := filepath.Join(out, "results-performance.md")
	first := readFile(t, path)

	if err := os.WriteFile(path, []byte("hand edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if log, ok := runReport(t, out); !ok {
		t.Fatalf("report failed:\n%s", log)
	}
	if got := readFile(t, path); got != first {
		t.Errorf("report did not rewrite the table from the JSON:\n%s", got)
	}

	if err := os.WriteFile(path, []byte("hand edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if log, ok := runBench(t, out); !ok {
		t.Fatalf("second benchmark.sh run failed:\n%s", log)
	}
	if got := readFile(t, path); strings.Contains(got, "hand edited") {
		t.Errorf("a second run did not rewrite the table:\n%s", got)
	}
}

// A run that records no timings has measured nothing. Publishing it would put
// a blank, or a zero, in the table as if it were a figure, so the script
// fails, and it leaves the previous results exactly as they were rather than
// mixing new JSON with an old table.
func TestBenchmarkFailsOnZeroTimings(t *testing.T) {
	needTools(t)
	for _, f := range resultFiles {
		t.Run(f, func(t *testing.T) {
			out := t.TempDir()
			for _, name := range append([]string{"results-performance.md"}, resultFiles...) {
				if err := os.WriteFile(filepath.Join(out, name), []byte("previous\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			log, ok := runBench(t, out, "FAKE_HYPERFINE_EMPTY="+f)
			if ok {
				t.Fatalf("benchmark.sh exited 0 with no timings in %s:\n%s", f, log)
			}
			if !strings.Contains(log, "no timings") {
				t.Errorf("failure does not say why:\n%s", log)
			}
			for _, name := range append([]string{"results-performance.md"}, resultFiles...) {
				if got := readFile(t, filepath.Join(out, name)); got != "previous\n" {
					t.Errorf("failed run replaced %s", name)
				}
			}
		})
	}
}

// The generator run on its own refuses committed JSON with no timings too,
// and keeps the table it had.
func TestReportRefusesJSONWithoutTimings(t *testing.T) {
	needTools(t)
	out := t.TempDir()
	if log, ok := runBench(t, out); !ok {
		t.Fatalf("benchmark.sh failed:\n%s", log)
	}
	table := readFile(t, filepath.Join(out, "results-performance.md"))

	path := filepath.Join(out, "results-read.json")
	var data map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &data); err != nil {
		t.Fatal(err)
	}
	data["results"].([]any)[1].(map[string]any)["times"] = []any{}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	log, ok := runReport(t, out)
	if ok {
		t.Fatalf("report exited 0 on a result with no timings:\n%s", log)
	}
	if !strings.Contains(log, "no timings") {
		t.Errorf("failure does not say why:\n%s", log)
	}
	if got := readFile(t, filepath.Join(out, "results-performance.md")); got != table {
		t.Errorf("failed report rewrote the table")
	}
}

// A refused or failed MCP call still produces a response. Timing it would
// benchmark the error path and publish it as a read, so the client exits
// non-zero, hyperfine stops, and the script fails. A handshake that lists no
// tools has not done what the row says it measures, so it fails the same way.
func TestBenchmarkFailsWhenTheServerAnswersWithAnError(t *testing.T) {
	needTools(t)
	for _, mode := range []string{"error", "iserror", "empty-tools"} {
		t.Run(mode, func(t *testing.T) {
			out := t.TempDir()
			log, ok := runBench(t, out, "FAKE_MCP="+mode)
			if ok {
				t.Fatalf("benchmark.sh exited 0 with the server answering %s:\n%s", mode, log)
			}
			if !strings.Contains(log, "Error response") {
				t.Errorf("failure does not show the server's answer:\n%s", log)
			}
			if _, err := os.Stat(filepath.Join(out, "results-performance.md")); err == nil {
				t.Errorf("a failed run wrote the table")
			}
		})
	}
}

// The committed table is what the committed JSON says. A hand edit to
// results-performance.md, or JSON replaced without regenerating the table,
// shows up here.
func TestCommittedTableMatchesCommittedJSON(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	for _, name := range append([]string{"results-meta.json", "results-performance.md"}, resultFiles...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(readFile(t, name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if log, ok := runReport(t, dir); !ok {
		t.Fatalf("report refused the committed results:\n%s", log)
	}
	got := readFile(t, filepath.Join(dir, "results-performance.md"))
	if want := readFile(t, "results-performance.md"); !bytes.Equal([]byte(got), []byte(want)) {
		t.Errorf("committed results-performance.md differs from the table its JSON generates; run python3 benchmarks/performance.py report")
	}
}
