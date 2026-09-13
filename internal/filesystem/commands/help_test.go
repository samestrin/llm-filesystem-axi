package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

var testSteps = []string{
	"Read a listed file: llm-filesystem read-file --path <path>",
	"Add --full for all fields (path, mode, timestamps, ...).",
}

// testStepsFn is testSteps in the closure form OutputResultAXI now takes. The
// slice stays, because several assertions still compare against its contents.
var testStepsFn = func() []string { return testSteps }

// AC3: contextual disclosure (AXI principle 9) is a trailing help[] block, not a
// field buried in the payload. The inline array form is the only one of the
// three in circulation that survives its own codec, so the body and the block
// together decode as a single document.
func TestTOONOutputEndsWithAHelpBlock(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), nil, testStepsFn, func() string { return "TEXT" })
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
		OutputResultAXI(sample(), nil, testStepsFn, func() string { return "TEXT" })
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
			func() []string { return []string{"open \x1b[31mred\u2028.txt"} },
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
		OutputResultAXI(sample(), nil, testStepsFn, func() string { return "TEXT" })
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
		OutputResultAXI(sample(), nil, testStepsFn, func() string { return "body" })
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

	OutputResultAXI(sample(), nil, testStepsFn, func() string { return "TEXT" })

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

	OutputResultAXI(sample(), nil, testStepsFn, func() string { return "TEXT" })

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

// AC7: an error said what went wrong and never what to try, which is the moment
// contextual disclosure is worth the most because it is the moment the agent is
// stuck. The guidance takes the same three-way split the success path uses — a
// trailing help[] block for TOON, a next_steps field for JSON, the human
// "Next steps:" form for text — so an error document has the same shape as every
// other document this tool emits, rather than a fourth one.
//
// The command named in the step comes from cobra's own resolution rather than
// from scanning argv, because argv cannot tell a subcommand from a flag value.
func TestUsageErrorsCarryRecoveryGuidance(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantStep string
	}{
		{"unknown flag points at the command that owns it",
			[]string{"list-directory", "--path", ".", "--bogus"},
			"llm-filesystem list-directory --help"},
		{"unknown command points at the root",
			[]string{"bogus-command"},
			"llm-filesystem --help"},
		{"missing required flag points at the command",
			[]string{"list-directory"},
			"llm-filesystem list-directory --help"},
	}

	for _, c := range cases {
		for _, f := range []Format{FormatTOON, FormatJSON, FormatText} {
			t.Run(c.name+"/"+string(f), func(t *testing.T) {
				args := append([]string{"--format", string(f)}, c.args...)
				stdout, stderr, _ := runCLI(t, args...)

				body := stdout
				if f == FormatText {
					body = stderr
				}

				switch f {
				case FormatTOON:
					if !strings.Contains(body, "help[") {
						t.Fatalf("no help block on a TOON error: %q", body)
					}
					if strings.Contains(body, "next_steps") {
						t.Errorf("TOON error gained a next_steps field: %q", body)
					}
				case FormatJSON:
					if strings.Contains(body, "help[") {
						t.Errorf("a TOON block was appended to a JSON error: %q", body)
					}
					var got map[string]interface{}
					if err := json.Unmarshal([]byte(body), &got); err != nil {
						t.Fatalf("a JSON error must parse as one document: %v for %q", err, body)
					}
					if _, ok := got["next_steps"].([]interface{}); !ok {
						t.Fatalf("next_steps missing from a JSON error: %q", body)
					}
				case FormatText:
					if !strings.Contains(body, "Next steps:") {
						t.Fatalf("text error lost its hint header: %q", body)
					}
					if strings.Contains(body, "help[") {
						t.Errorf("a TOON block leaked into a text error: %q", body)
					}
				}

				if !strings.Contains(body, c.wantStep) {
					t.Errorf("body = %q, want a step naming %q", body, c.wantStep)
				}
			})
		}
	}
}

// An error body and its guidance are one document, for exactly the reason the
// payload and its help block are: a consumer must never receive half of one.
func TestErrorBodyAndGuidanceShareOneWrite(t *testing.T) {
	w := &countingWriter{}
	prevOut, prevExit := outWriter, exitFunc
	outWriter = w
	exitFunc = func(int) {}
	t.Cleanup(func() { outWriter, exitFunc = prevOut, prevExit })

	emitDiagnostic(FormatTOON, false, errBoom{}, goaxi.ExitUsage,
		[]string{"Valid flags: llm-filesystem --help"})

	if w.writes != 1 {
		t.Errorf("writes = %d, want 1 for error body + guidance", w.writes)
	}
	if !strings.Contains(w.buf.String(), "help[1]: ") {
		t.Errorf("the single write must carry the guidance: %q", w.buf.String())
	}
	if !strings.Contains(w.buf.String(), "boom") {
		t.Errorf("the single write must carry the error: %q", w.buf.String())
	}
}

// AC10: a zero-match search still offered "Open a match", naming a target that
// does not exist. A step an agent cannot take is worse than no step: it costs
// tokens to read and one wasted turn to discover it was a lie.
//
// The steps were a plain slice evaluated at the call site BEFORE the result
// existed, so no command could vary them by what it found. Driving this through
// the real command rather than the renderer is deliberate — a renderer test with
// a hand-written closure would pass while every command still shipped the fixed
// slice.
func TestHelpLinesAdaptToAnEmptyResult(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name       string
		args       []string
		impossible string
		want       string
	}{
		{"search-code", []string{"search-code", "--path", dir, "--pattern", "zzz-no-such-token"}, "Open a match", "widen"},
		{"search-files", []string{"search-files", "--path", dir, "--pattern", "zzz-no-such-file"}, "Read a match", "widen"},
		{"list-directory", []string{"list-directory", "--path", dir}, "Read a listed file", "--show-hidden"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, _, _ := runCLI(t, c.args...)

			if strings.Contains(stdout, c.impossible) {
				t.Errorf("a zero-result command offered %q: %q", c.impossible, stdout)
			}
			if !strings.Contains(stdout, "help[") {
				t.Fatalf("a zero-result command emitted no guidance at all: %q", stdout)
			}
			if !strings.Contains(strings.ToLower(stdout), strings.ToLower(c.want)) {
				t.Errorf("zero-result guidance should mention %q: %q", c.want, stdout)
			}
		})
	}
}

// The other half of AC10, and the half that stops "suppress the line" from being
// satisfied by deleting it: a search that DID find something must still say how
// to open it.
func TestHelpLinesStillGuideANonEmptyResult(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hit.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, _ := runCLI(t, "search-code", "--path", dir, "--pattern", "needle")

	if !strings.Contains(stdout, "Open a match") {
		t.Errorf("a search with matches lost its open-a-match step: %q", stdout)
	}
	if strings.Contains(strings.ToLower(stdout), "widen") {
		t.Errorf("a search with matches suggested widening it: %q", stdout)
	}
}

// The steps closure must be resolved exactly once per document. Resolving it per
// format branch would let JSON and TOON disagree about what the next step is,
// and a command whose steps are expensive would pay twice.
//
// nil must also stay safe, because 23 of the 27 call sites pass it.
func TestStepsFnIsResolvedOnceAndNilIsSafe(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	calls := 0
	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), nil, func() []string {
			calls++
			return testSteps
		}, func() string { return "TEXT" })
	})

	if calls != 1 {
		t.Errorf("steps closure ran %d times, want exactly 1", calls)
	}
	if !strings.Contains(stdout, "help[2]: ") {
		t.Errorf("the resolved steps did not reach the block: %q", stdout)
	}

	// OutputResult passes a nil closure; it must render rather than panic.
	nilOut, _, _ := captureOutput(t, func() {
		OutputResult(sample(), func() string { return "TEXT" })
	})
	if strings.Contains(nilOut, "help[") {
		t.Errorf("a nil steps closure produced a block: %q", nilOut)
	}
	if !strings.Contains(nilOut, "items[2]") {
		t.Errorf("a nil steps closure lost the payload: %q", nilOut)
	}
}

// AC7 requires EVERY error to say what to try next, and only usage errors did.
// A real tool failure — a missing file, a sandbox refusal, a bad archive — came
// back with a message and nothing else, which is the moment an agent is most
// stuck and least able to guess.
//
// The step has to be one that is always takeable. The command's own --help
// always is, and unlike an invented per-error suggestion it can never point
// somewhere that does not exist, which is what AC10 forbids.
func TestToolFailuresCarryRecoveryGuidance(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"missing file", []string{"read-file", "--path", "/definitely/not/here.txt"}},
		{"missing search path", []string{"search-code", "--path", "/nope/zz", "--pattern", "x"}},
		{"sandbox refusal", []string{"--allowed-dirs", "/tmp", "list-directory", "--path", "/etc"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, _, code := runCLI(t, c.args...)

			if code != int(goaxi.ExitError) {
				t.Errorf("exit = %d, want ExitError (%d)", code, goaxi.ExitError)
			}
			if !strings.Contains(stdout, "error: true") {
				t.Fatalf("no structured error body: %q", stdout)
			}
			if !strings.Contains(stdout, "help[") {
				t.Errorf("a tool failure gave the agent no next step: %q", stdout)
			}
		})
	}
}

// A tool failure with nothing useful to suggest must emit no block at all.
// AC10 forbids a help line an agent cannot act on, and an empty block costs
// tokens to read and teaches nothing.
func TestErrorWithoutGuidanceEmitsNoBlock(t *testing.T) {
	w := &countingWriter{}
	prevOut, prevExit := outWriter, exitFunc
	outWriter = w
	exitFunc = func(int) {}
	t.Cleanup(func() { outWriter, exitFunc = prevOut, prevExit })

	emitDiagnostic(FormatTOON, false, errBoom{}, goaxi.ExitError, nil)

	if strings.Contains(w.buf.String(), "help[") {
		t.Errorf("a stepless error emitted a help block: %q", w.buf.String())
	}
	if !strings.Contains(w.buf.String(), "boom") {
		t.Errorf("the error body is missing: %q", w.buf.String())
	}
}
