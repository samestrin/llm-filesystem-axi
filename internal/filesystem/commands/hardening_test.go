package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// AC2: a file name or file content is text this tool did not author, and it
// reaches stdout. toon-go rejects control bytes below 0x20 with an error but
// passes U+2028, U+2029, lone C1 bytes and invalid UTF-8 straight through, so
// without a sanitizer a payload carrying a raw escape sequence reaches whatever
// terminal renders it.
//
// Scope is the TOON path. The JSON path is deliberately excluded: --full
// --format json is contractually byte-identical, and sanitizing changes bytes.
func TestTOONOutputIsSanitized(t *testing.T) {
	dirty := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "plain\x1b[31mred.txt"},
			map[string]interface{}{"name": "line\u2028sep.txt"},
			map[string]interface{}{"name": "para\u2029sep.txt"},
			map[string]interface{}{"name": "c1\u009bhere.txt"},
		},
		"total": 4,
	}

	out, err := renderGeneric(FormatTOON, false, dirty)
	if err != nil {
		t.Fatalf("a sanitizable payload must still encode, got %v", err)
	}

	for _, bad := range []struct {
		label string
		s     string
	}{
		{"ANSI escape", "\x1b"},
		{"U+2028 line separator", "\u2028"},
		{"U+2029 paragraph separator", "\u2029"},
		{"C1 CSI", "\u009b"},
	} {
		if strings.Contains(out, bad.s) {
			t.Errorf("%s survived into TOON output: %q", bad.label, out)
		}
	}

	// Stripping must join the surrounding text, not drop the whole field. Only
	// the escape byte itself goes; the "[31m" that followed it is ordinary text.
	for _, want := range []string{"plain[31mred.txt", "linesep.txt", "parasep.txt", "c1here.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("visible text around a stripped byte was lost, want %q in %q", want, out)
		}
	}
}

// Invalid UTF-8 is a separate case: not a character to strip but a byte sequence
// no decoder can read. Output must always be valid UTF-8.
func TestTOONOutputIsAlwaysValidUTF8(t *testing.T) {
	payload := map[string]interface{}{"name": "bad\xff\xfe.txt"}

	out, err := renderGeneric(FormatTOON, false, payload)
	if err != nil {
		t.Fatalf("renderGeneric: %v", err)
	}
	if !utf8.ValidString(out) {
		t.Errorf("output is not valid UTF-8: %q", out)
	}
}

// Legitimate whitespace must survive. A sanitizer that ate tabs and newlines
// would corrupt every multi-line file this tool reads.
func TestTOONOutputPreservesRealWhitespace(t *testing.T) {
	payload := map[string]interface{}{"content": "a\tb\nc"}

	out, err := renderGeneric(FormatTOON, false, payload)
	if err != nil {
		t.Fatalf("renderGeneric: %v", err)
	}
	// toon-go escapes these rather than emitting them raw, so accept either the
	// escaped form or the literal byte.
	if !strings.Contains(out, "\\t") && !strings.Contains(out, "\t") {
		t.Errorf("tab was stripped: %q", out)
	}
	if !strings.Contains(out, "\\n") && !strings.Contains(out, "\n") {
		t.Errorf("newline was stripped: %q", out)
	}
}

// AC6: toon-go supports neither defined string types nor encoding.TextMarshaler,
// and a violating type does not error — it produces EMPTY output. The command
// then prints nothing and exits zero.
//
// Production reaches renderGeneric through toGeneric, which flattens everything
// to JSON primitives and so cannot carry such a type. This asserts the guard
// itself, so the protection does not quietly disappear if that ever changes.
func TestRenderGenericRefusesALossyValue(t *testing.T) {
	type row struct {
		At lossyStamp `json:"at"`
	}

	out, err := renderGeneric(FormatTOON, false, row{})
	if err == nil {
		t.Fatalf("a value TOON cannot carry must be refused, got output %q", out)
	}
	if out != "" {
		t.Errorf("nothing may be returned on refusal, got %q", out)
	}
}

// A value that cannot be rendered must not be silently downgraded to another
// format either. The command fails with a diagnostic rather than emitting a
// payload that parses as something the caller did not ask for.
func TestOutputResultAXIFailsLoudOnAnUnencodableValue(t *testing.T) {
	withFormat(t, FormatTOON, false, false)

	stdout, stderr, code := captureOutput(t, func() {
		OutputResultAXI(unmarshalable{}, nil, nil, func() string { return "TEXT" })
	})

	if code == 0 {
		t.Errorf("an unencodable value must exit non-zero, got %d", code)
	}
	if stderr == "" {
		t.Error("an unencodable value must explain itself on stderr")
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("nothing may be written to stdout on refusal, got %q", stdout)
	}
}

// lossyStamp implements encoding.TextMarshaler, which toon-go ignores.
type lossyStamp struct{ t time.Time }

func (lossyStamp) MarshalText() ([]byte, error) { return []byte("stamp"), nil }

// unmarshalable fails json.Marshal, so toGeneric cannot produce a payload.
type unmarshalable struct{}

func (unmarshalable) MarshalJSON() ([]byte, error) { return nil, errNoJSON{} }

type errNoJSON struct{}

func (errNoJSON) Error() string { return "cannot marshal" }

var _ json.Marshaler = unmarshalable{}
