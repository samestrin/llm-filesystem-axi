package commands

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    Format
		wantErr bool
	}{
		{"toon", FormatTOON, false},
		{"json", FormatJSON, false},
		{"text", FormatText, false},
		{"TOON", FormatTOON, false}, // case-insensitive
		{"Json", FormatJSON, false},
		{"", FormatTOON, true},     // empty is invalid
		{"yaml", FormatTOON, true}, // unknown must fail loud (AXI #6)
		{"toon ", FormatTOON, true},
	}
	for _, tt := range tests {
		got, err := parseFormat(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseFormat(%q) expected error, got nil (%v)", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseFormat(%q) unexpected error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseFormat(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name        string
		formatFlag  string
		formatSet   bool
		jsonFlag    bool
		minFlag     bool
		wantFormat  Format
		wantCompact bool
	}{
		{"default is toon", "toon", false, false, false, FormatTOON, false},
		{"explicit --format json wins", "json", true, false, false, FormatJSON, false},
		{"explicit --format text wins over --json", "text", true, true, false, FormatText, false},
		{"legacy --json maps to json", "toon", false, true, false, FormatJSON, false},
		{"legacy --json --min is compact json", "toon", false, true, true, FormatJSON, true},
		{"legacy --min alone is text (back-compat)", "toon", false, false, true, FormatText, true},
		{"--format with --min sets compact", "json", true, false, true, FormatJSON, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFmt, gotCompact, err := resolveFormat(tt.formatFlag, tt.formatSet, tt.jsonFlag, tt.minFlag)
			if err != nil {
				t.Fatalf("resolveFormat unexpected error: %v", err)
			}
			if gotFmt != tt.wantFormat {
				t.Errorf("format = %v, want %v", gotFmt, tt.wantFormat)
			}
			if gotCompact != tt.wantCompact {
				t.Errorf("compact = %v, want %v", gotCompact, tt.wantCompact)
			}
		})
	}
}

func TestResolveFormatInvalidFailsLoud(t *testing.T) {
	// An explicit invalid --format must error, never silently default.
	if _, _, err := resolveFormat("nonsense", true, false, false); err == nil {
		t.Error("resolveFormat with invalid explicit format should error")
	}
}

// AC7: formatFromArgs is the usage-error path's only source of truth about the
// requested format. Cobra fails on an unknown flag or subcommand before
// PersistentPreRunE runs, so activeFmt is never assigned and still holds
// whatever the previous invocation left there. Every form the root flags accept
// has to be recognized here, or a diagnostic renders in a format the caller
// never asked for.
func TestFormatFromArgsScansRawArgv(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantFormat  Format
		wantCompact bool
	}{
		{"no flags is toon", []string{"list-directory"}, FormatTOON, false},
		{"--format with a separate value", []string{"--format", "json", "x"}, FormatJSON, false},
		{"--format= joined form", []string{"--format=json"}, FormatJSON, false},
		{"--format= joined text", []string{"--format=text"}, FormatText, false},
		{"legacy --json", []string{"--json", "x"}, FormatJSON, false},
		{"legacy --min alone is text", []string{"--min"}, FormatText, true},
		{"legacy --json --min is compact json", []string{"--json", "--min"}, FormatJSON, true},
		{"explicit --format beats legacy --json", []string{"--json", "--format", "text"}, FormatText, false},
		{"nothing after -- is a flag", []string{"--", "--format", "json"}, FormatTOON, false},
		{"trailing --format with no value", []string{"list-directory", "--format"}, FormatTOON, false},
		// compress-files owns a --format flag naming the ARCHIVE type. A raw argv
		// scan cannot tell that apart from the global flag, so it reads "zip",
		// which resolveFormat rejects, which falls back to TOON. That is the
		// right outcome for the only thing this function decides — how to render
		// a diagnostic — and it is why the returned error is discarded here.
		{"a subcommand's own --format falls back", []string{"compress-files", "--format", "zip"}, FormatTOON, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFmt, gotCompact := formatFromArgs(tt.args)
			if gotFmt != tt.wantFormat {
				t.Errorf("format = %v, want %v", gotFmt, tt.wantFormat)
			}
			if gotCompact != tt.wantCompact {
				t.Errorf("compact = %v, want %v", gotCompact, tt.wantCompact)
			}
		})
	}
}

// renderGeneric is the encoder the CLI actually calls, so the back-compat
// assertions live on it. They used to target renderResult, which had no
// production caller — the suite could stay green through a codec swap that
// changed every shipped byte.
func TestRenderGenericJSONBackCompat(t *testing.T) {
	result := map[string]interface{}{"path": "/x", "total": 2}

	pretty, err := renderGeneric(FormatJSON, false, result)
	if err != nil {
		t.Fatal(err)
	}
	wantPretty, _ := json.MarshalIndent(result, "", "  ")
	if pretty != string(wantPretty) {
		t.Errorf("pretty json = %q, want %q", pretty, wantPretty)
	}

	compact, err := renderGeneric(FormatJSON, true, result)
	if err != nil {
		t.Fatal(err)
	}
	wantCompact, _ := json.Marshal(result)
	if compact != string(wantCompact) {
		t.Errorf("compact json = %q, want %q", compact, wantCompact)
	}
}

func TestRenderGenericTOONIsTabular(t *testing.T) {
	result := map[string]interface{}{
		"items": []map[string]interface{}{
			{"name": "a.go", "type": "file"},
			{"name": "b.go", "type": "file"},
		},
		"total": 2,
	}
	out, err := renderGeneric(FormatTOON, false, result)
	if err != nil {
		t.Fatalf("toon render error: %v", err)
	}
	// TOON must be tabular (header row with field names), not JSON.
	if strings.Contains(out, "{") && strings.Contains(out, "\"name\"") {
		t.Errorf("TOON output looks like JSON, not TOON: %q", out)
	}
	if !strings.Contains(out, "items[2]") {
		t.Errorf("TOON output missing tabular array header: %q", out)
	}
	if !strings.Contains(out, "total: 2") {
		t.Errorf("TOON output missing aggregate: %q", out)
	}
}

// TOON must represent the same data as JSON: struct json tags are honored and
// ,omitempty fields with zero values are dropped. Guards against an encoder
// that treats the raw tag ("name,omitempty") as the column name.
func TestRenderGenericTOONHonorsJSONTags(t *testing.T) {
	type item struct {
		Name  string `json:"name"`
		Size  int    `json:"size,omitempty"`
		Note  string `json:"note,omitempty"`
		Cache string `json:"-"`
	}
	result := struct {
		Items []item `json:"items"`
		Total int    `json:"total"`
	}{
		Items: []item{{Name: "a.go", Size: 10, Cache: "secret"}},
		Total: 1,
	}

	payload, err := toGeneric(result)
	if err != nil {
		t.Fatalf("toGeneric: %v", err)
	}
	out, err := renderGeneric(FormatTOON, false, payload)
	if err != nil {
		t.Fatalf("toon render error: %v", err)
	}
	if strings.Contains(out, "omitempty") {
		t.Errorf("TOON leaked the ,omitempty tag option into a column name: %q", out)
	}
	if strings.Contains(out, "note") {
		t.Errorf("TOON included an empty ,omitempty field: %q", out)
	}
	if strings.Contains(out, "secret") || strings.Contains(out, "Cache") {
		t.Errorf("TOON included a json:\"-\" field: %q", out)
	}
	if !strings.Contains(out, "name") {
		t.Errorf("TOON missing the name column: %q", out)
	}
}

func TestRenderErrorFormats(t *testing.T) {
	err := errors.New("boom")

	// Every case passes nil steps, which is what keeps these byte-for-byte
	// assertions valid: injectNextSteps no-ops on an empty slice, so guidance
	// costs an errorless caller nothing.

	// Text (non-compact) keeps the "Error: " prefix used pre-AXI.
	if got := renderError(FormatText, false, err, nil); got != "Error: boom" {
		t.Errorf("text error = %q, want %q", got, "Error: boom")
	}
	// Text compact is the bare message.
	if got := renderError(FormatText, true, err, nil); got != "boom" {
		t.Errorf("compact text error = %q, want %q", got, "boom")
	}
	// JSON error is byte-compatible with pre-AXI.
	wantJSON, _ := json.MarshalIndent(map[string]interface{}{"error": true, "message": "boom"}, "", "  ")
	if got := renderError(FormatJSON, false, err, nil); got != string(wantJSON) {
		t.Errorf("json error = %q, want %q", got, wantJSON)
	}
	// JSON compact uses abbreviated keys, as pre-AXI.
	wantMin, _ := json.Marshal(map[string]interface{}{"err": true, "msg": "boom"})
	if got := renderError(FormatJSON, true, err, nil); got != string(wantMin) {
		t.Errorf("compact json error = %q, want %q", got, wantMin)
	}
	// TOON error is structured (not the "Error: " text form, not raw JSON braces).
	toonErr := renderError(FormatTOON, false, err, nil)
	if strings.HasPrefix(toonErr, "Error:") || strings.HasPrefix(toonErr, "{") {
		t.Errorf("toon error should be structured TOON, got %q", toonErr)
	}
	if !strings.Contains(toonErr, "boom") {
		t.Errorf("toon error missing message: %q", toonErr)
	}
}

// AC7: guidance reaches JSON through the document itself, because appending a
// TOON block would stop it being one JSON value. TOON must NOT gain the field —
// it gets a trailing block instead, and carrying both would bill the agent for
// the same strings twice.
func TestRenderErrorPlacesGuidanceByFormat(t *testing.T) {
	err := errors.New("boom")
	steps := []string{"Valid flags and subcommands: llm-filesystem --help"}

	var got map[string]interface{}
	if uerr := json.Unmarshal([]byte(renderError(FormatJSON, false, err, steps)), &got); uerr != nil {
		t.Fatalf("a JSON error with guidance must parse as one document: %v", uerr)
	}
	if _, ok := got["next_steps"].([]interface{}); !ok {
		t.Errorf("next_steps missing from JSON error: %v", got)
	}

	if toonErr := renderError(FormatTOON, false, err, steps); strings.Contains(toonErr, "next_steps") {
		t.Errorf("TOON error gained a next_steps field: %q", toonErr)
	}

	// Text renders the same with or without steps; emitDiagnostic appends its
	// bullets after the body rather than folding them into the message.
	if got := renderError(FormatText, false, err, steps); got != "Error: boom" {
		t.Errorf("text error = %q, want the message unchanged by steps", got)
	}
}
