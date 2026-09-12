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
