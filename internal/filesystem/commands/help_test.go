package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

var testSteps = []string{
	"Read a listed file: llm-filesystem read-file --path <path>",
	"Add --full for all fields (path, mode, timestamps, ...).",
}

// AC3: contextual disclosure (AXI principle 9) is a trailing help[] block, not a
// field buried in the payload. The inline array form is the only one of the
// three in circulation that survives its own codec, so the body and the block
// together decode as a single document.
func TestTOONOutputEndsWithAHelpBlock(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), nil, testSteps, func() string { return "TEXT" })
	})

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	last := lines[len(lines)-1]

	if !strings.HasPrefix(last, "help[2]: ") {
		t.Errorf("last line = %q, want a help[2] block", last)
	}
	if !strings.Contains(last, "read-file --path <path>") {
		t.Errorf("help block lost its content: %q", last)
	}
	// The payload must still be there, above the block.
	if !strings.Contains(stdout, "items[2]") {
		t.Errorf("payload missing: %q", stdout)
	}
}

// The old shape must be gone from TOON. A payload field and a trailing block
// carrying the same strings would cost the tokens twice, which contradicts the
// principle the block exists to serve.
func TestTOONOutputHasNoNextStepsField(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), nil, testSteps, func() string { return "TEXT" })
	})

	if strings.Contains(stdout, "next_steps") {
		t.Errorf("next_steps survived in TOON output: %q", stdout)
	}
}

// A command with no meaningful next step must not emit a stray empty block an
// agent pays tokens to read and learns nothing from.
func TestTOONOutputOmitsTheBlockWhenThereAreNoSteps(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResult(sample(), func() string { return "TEXT" })
	})

	if strings.Contains(stdout, "help[") {
		t.Errorf("a stepless command emitted a help block: %q", stdout)
	}
	if strings.HasSuffix(stdout, "\n\n") {
		t.Errorf("a stepless command left a trailing blank line: %q", stdout)
	}
}

// Step text is interpolated from paths and patterns the caller supplied, so it
// is no more trusted than the payload.
func TestHelpBlockIsSanitized(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), nil,
			[]string{"open \x1b[31mred\u2028.txt"},
			func() string { return "TEXT" })
	})

	// Assert the block exists first, or the checks below pass vacuously.
	if !strings.Contains(stdout, "help[1]: ") {
		t.Fatalf("no help block was emitted: %q", stdout)
	}
	if !strings.Contains(stdout, "red.txt") {
		t.Errorf("visible step text was lost around the stripped bytes: %q", stdout)
	}
	if strings.Contains(stdout, "\x1b") {
		t.Errorf("an ANSI escape survived into the help block: %q", stdout)
	}
	if strings.Contains(stdout, "\u2028") {
		t.Errorf("U+2028 survived into the help block: %q", stdout)
	}
}

// JSON must stay a single JSON document, so it keeps the payload field rather
// than gaining a TOON block appended to it. Dropping hints from JSON entirely
// would be a silent feature regression for --format json consumers.
func TestJSONOutputKeepsNextStepsAndGainsNoTOONBlock(t *testing.T) {
	withFormat(t, FormatJSON, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), nil, testSteps, func() string { return "TEXT" })
	})

	if strings.Contains(stdout, "help[") {
		t.Errorf("a TOON block was appended to JSON output: %q", stdout)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("JSON output must parse as one document, got %v for %q", err, stdout)
	}
	steps, ok := got["next_steps"].([]interface{})
	if !ok {
		t.Fatalf("next_steps missing from JSON output: %q", stdout)
	}
	if len(steps) != 2 {
		t.Errorf("next_steps = %v, want 2 entries", steps)
	}
}

// The text path has always had its own human-readable form, written before the
// payload rendering branch. It is not replaced by the block.
func TestTextOutputKeepsItsOwnNextStepsForm(t *testing.T) {
	withFormat(t, FormatText, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), nil, testSteps, func() string { return "body" })
	})

	if !strings.Contains(stdout, "Next steps:") {
		t.Errorf("text output lost its hint header: %q", stdout)
	}
	if !strings.Contains(stdout, "  - Read a listed file") {
		t.Errorf("text output lost its bullets: %q", stdout)
	}
	if strings.Contains(stdout, "help[") {
		t.Errorf("a TOON block leaked into text output: %q", stdout)
	}
}

// failAfterN is a writer that accepts the first n writes and then fails, which
// is what a consumer closing the pipe mid-document looks like.
type failAfterN struct {
	n       int
	written int
}

func (w *failAfterN) Write(p []byte) (int, error) {
	if w.written >= w.n {
		return 0, errPipeClosed{}
	}
	w.written++
	return len(p), nil
}

type errPipeClosed struct{}

func (errPipeClosed) Error() string { return "pipe closed" }

// The payload and the help block are one document. If the block cannot be
// written, the caller must not additionally receive an error body appended to
// the half-written payload — that is two conflicting documents on one stream.
// A single write for the whole thing is what makes this impossible.
func TestOutputIsOneWritePerDocument(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	w := &failAfterN{n: 0}
	prevOut, prevExit := outWriter, exitFunc
	code := -1
	outWriter = w
	exitFunc = func(c int) { code = c }
	t.Cleanup(func() { outWriter, exitFunc = prevOut, prevExit })

	OutputResultAXI(sample(), nil, testSteps, func() string { return "TEXT" })

	if w.written != 0 {
		t.Errorf("a failing sink accepted %d writes; the document must be attempted once", w.written)
	}
	if code == 0 {
		t.Errorf("a write failure must exit non-zero, got %d", code)
	}
}

// The same document, against a sink that works: exactly one write carries the
// payload and the help block together.
func TestPayloadAndHelpBlockShareOneWrite(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	w := &countingWriter{}
	prevOut := outWriter
	outWriter = w
	t.Cleanup(func() { outWriter = prevOut })

	OutputResultAXI(sample(), nil, testSteps, func() string { return "TEXT" })

	if w.writes != 1 {
		t.Errorf("writes = %d, want 1 for payload + help block", w.writes)
	}
	if !strings.Contains(w.buf.String(), "help[2]: ") {
		t.Errorf("the single write must carry the help block: %q", w.buf.String())
	}
	if !strings.Contains(w.buf.String(), "items[2]") {
		t.Errorf("the single write must carry the payload: %q", w.buf.String())
	}
}

type countingWriter struct {
	buf    strings.Builder
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}
