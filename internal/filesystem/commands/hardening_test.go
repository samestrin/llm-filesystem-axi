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

// AC6, first half: a value the encoder genuinely cannot carry must produce an
// error and NO output. A partial body is worse than none, because it parses.
func TestRenderGenericRefusesRatherThanWritePartialOutput(t *testing.T) {
	out, err := renderGeneric(FormatTOON, false, make(chan int))
	if err == nil {
		t.Fatalf("an unencodable value must be refused, got output %q", out)
	}
	if out != "" {
		t.Errorf("nothing may be returned on refusal, got %q", out)
	}
}

// The production path has a second line of defence against the empty-output
// trap, asserted rather than assumed: OutputResultAXI runs every result through
// toGeneric first, and encoding/json DOES honor encoding.TextMarshaler, so the
// type toon-go would drop arrives at the encoder already flattened to a plain
// string.
//
// The guard inside encodeTOON is the first line and does not depend on this. If
// toGeneric ever leaves the path, this test breaking is the signal — not an
// outage, because the guard still catches it.
func TestToGenericFlattensATypeTOONWouldDrop(t *testing.T) {
	payload, err := toGeneric(map[string]interface{}{"at": lossyStamp{}})
	if err != nil {
		t.Fatalf("toGeneric: %v", err)
	}

	m, ok := payload.(map[string]interface{})
	if !ok {
		t.Fatalf("payload = %T, want a map", payload)
	}
	if _, isString := m["at"].(string); !isString {
		t.Fatalf("at = %#v, want a plain string after flattening", m["at"])
	}

	out, err := renderGeneric(FormatTOON, false, payload)
	if err != nil {
		t.Fatalf("a flattened value must encode, got %v", err)
	}
	if !strings.Contains(out, "stamp") {
		t.Errorf("flattened value lost its content: %q", out)
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
