package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// captureOutput redirects the package output sinks for the duration of fn and
// returns what was written to each. The sinks exist because OutputResultAXI and
// OutputError previously wrote straight to os.Stdout/os.Stderr and called
// os.Exit, which made the only code path that actually ships untestable.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string, exitCode int) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	prevOut, prevErr, prevExit := outWriter, errWriter, exitFunc

	code := -1
	outWriter, errWriter = &outBuf, &errBuf
	exitFunc = func(c int) { code = c }
	t.Cleanup(func() { outWriter, errWriter, exitFunc = prevOut, prevErr, prevExit })

	fn()

	return outBuf.String(), errBuf.String(), code
}

// withFormat sets the resolved output mode that PersistentPreRunE normally
// establishes, and restores it afterwards.
func withFormat(t *testing.T, f Format, compact, full bool) {
	t.Helper()

	prevFmt, prevCompact, prevFull := activeFmt, activeCompact, activeFull
	activeFmt, activeCompact, activeFull = f, compact, full
	t.Cleanup(func() { activeFmt, activeCompact, activeFull = prevFmt, prevCompact, prevFull })
}

type sampleItem struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

type sampleResult struct {
	Path  string       `json:"path"`
	Total int          `json:"total"`
	Items []sampleItem `json:"items"`
}

func sample() sampleResult {
	return sampleResult{
		Path:  "/tmp",
		Total: 2,
		Items: []sampleItem{{Name: "a.txt", Size: 10}, {Name: "b.txt", Size: 20}},
	}
}

func TestRenderGenericJSONCompactAndPretty(t *testing.T) {
	payload, err := toGeneric(sample())
	if err != nil {
		t.Fatalf("toGeneric: %v", err)
	}

	pretty, err := renderGeneric(FormatJSON, false, payload)
	if err != nil {
		t.Fatalf("renderGeneric pretty: %v", err)
	}
	if !strings.Contains(pretty, "\n  ") {
		t.Errorf("pretty JSON must be indented, got %q", pretty)
	}

	compact, err := renderGeneric(FormatJSON, true, payload)
	if err != nil {
		t.Fatalf("renderGeneric compact: %v", err)
	}
	if strings.Contains(compact, "\n") {
		t.Errorf("compact JSON must be one line, got %q", compact)
	}
}

func TestOutputResultAXIWritesTOONToStdout(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, stderr, _ := captureOutput(t, func() {
		OutputResult(sample(), func() string { return "unused" })
	})

	if stderr != "" {
		t.Errorf("payload must not go to stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "items[2]") {
		t.Errorf("expected TOON payload on stdout, got %q", stdout)
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Errorf("output must be newline-terminated, got %q", stdout)
	}
	if strings.HasSuffix(stdout, "\n\n") {
		t.Errorf("output must not end in a blank line, got %q", stdout)
	}
}

func TestOutputResultAXITextGoesThroughTextFn(t *testing.T) {
	withFormat(t, FormatText, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResult(sample(), func() string { return "hello text" })
	})

	if strings.TrimSpace(stdout) != "hello text" {
		t.Errorf("stdout = %q, want %q", stdout, "hello text")
	}
}

func TestOutputErrorExitsNonZeroAndRendersBody(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, _, code := captureOutput(t, func() {
		OutputError(errBoom{})
	})

	if code == 0 {
		t.Errorf("a tool failure must exit non-zero, got %d", code)
	}
	if !strings.Contains(stdout, "boom") {
		t.Errorf("structured error body must reach stdout, got %q", stdout)
	}
}

func TestOutputErrorTextGoesToStderr(t *testing.T) {
	withFormat(t, FormatText, false, false)

	stdout, stderr, code := captureOutput(t, func() {
		OutputError(errBoom{})
	})

	if stdout != "" {
		t.Errorf("text errors must not pollute stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "boom") {
		t.Errorf("stderr = %q, want it to mention boom", stderr)
	}
	if code == 0 {
		t.Error("a tool failure must exit non-zero")
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

// AC5: --full --format json must stay byte-identical to the pre-AXI --json
// output. That guarantee is documented in README.md and CHANGELOG.md, and it is
// the reason the JSON path is deliberately left unsanitized — sanitizing would
// change bytes. Asserted at OutputResultAXI, because the legacy path is an early
// return there and never reaches the renderer.
func TestOutputResultAXIFullJSONStaysByteIdentical(t *testing.T) {
	result := sample()
	spec := map[string][]string{"items": {"name"}}
	steps := []string{"do X"}

	for _, compact := range []bool{false, true} {
		withFormat(t, FormatJSON, compact, true)

		stdout, _, _ := captureOutput(t, func() {
			OutputResultAXI(result, spec, steps, func() string { return "TEXT" })
		})

		var want []byte
		if compact {
			want, _ = json.Marshal(result)
		} else {
			want, _ = json.MarshalIndent(result, "", "  ")
		}

		if strings.TrimRight(stdout, "\n") != string(want) {
			t.Errorf("compact=%v: full+json = %q, want %q", compact, stdout, want)
		}
		// Neither the minimal projection nor the hints may touch this path.
		if !strings.Contains(stdout, "\"size\"") {
			t.Errorf("compact=%v: projection leaked into the legacy path: %q", compact, stdout)
		}
	}
}
