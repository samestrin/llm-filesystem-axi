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

// renderResult JSON output must remain byte-identical to the pre-AXI behavior
// so existing --json consumers do not break.
func TestRenderResultJSONBackCompat(t *testing.T) {
	result := map[string]interface{}{"path": "/x", "total": 2}

	pretty, err := renderResult(FormatJSON, false, result, func() string { return "TEXT" })
	if err != nil {
		t.Fatal(err)
	}
	wantPretty, _ := json.MarshalIndent(result, "", "  ")
	if pretty != string(wantPretty) {
		t.Errorf("pretty json = %q, want %q", pretty, wantPretty)
	}

	compact, err := renderResult(FormatJSON, true, result, func() string { return "TEXT" })
	if err != nil {
		t.Fatal(err)
	}
	wantCompact, _ := json.Marshal(result)
	if compact != string(wantCompact) {
		t.Errorf("compact json = %q, want %q", compact, wantCompact)
	}
}

func TestRenderResultText(t *testing.T) {
	out, err := renderResult(FormatText, false, map[string]interface{}{"a": 1}, func() string { return "hello text" })
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello text" {
		t.Errorf("text render = %q, want %q", out, "hello text")
	}
}

func TestRenderResultTOON(t *testing.T) {
	result := map[string]interface{}{
		"items": []map[string]interface{}{
			{"name": "a.go", "type": "file"},
			{"name": "b.go", "type": "file"},
		},
		"total": 2,
	}
	out, err := renderResult(FormatTOON, false, result, func() string { return "TEXT" })
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
func TestRenderResultTOONHonorsJSONTags(t *testing.T) {
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

	out, err := renderResult(FormatTOON, false, result, func() string { return "TEXT" })
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

	// Text (non-compact) keeps the "Error: " prefix used pre-AXI.
	if got := renderError(FormatText, false, err); got != "Error: boom" {
		t.Errorf("text error = %q, want %q", got, "Error: boom")
	}
	// Text compact is the bare message.
	if got := renderError(FormatText, true, err); got != "boom" {
		t.Errorf("compact text error = %q, want %q", got, "boom")
	}
	// JSON error is byte-compatible with pre-AXI.
	wantJSON, _ := json.MarshalIndent(map[string]interface{}{"error": true, "message": "boom"}, "", "  ")
	if got := renderError(FormatJSON, false, err); got != string(wantJSON) {
		t.Errorf("json error = %q, want %q", got, wantJSON)
	}
	// JSON compact uses abbreviated keys, as pre-AXI.
	wantMin, _ := json.Marshal(map[string]interface{}{"err": true, "msg": "boom"})
	if got := renderError(FormatJSON, true, err); got != string(wantMin) {
		t.Errorf("compact json error = %q, want %q", got, wantMin)
	}
	// TOON error is structured (not the "Error: " text form, not raw JSON braces).
	toonErr := renderError(FormatTOON, false, err)
	if strings.HasPrefix(toonErr, "Error:") || strings.HasPrefix(toonErr, "{") {
		t.Errorf("toon error should be structured TOON, got %q", toonErr)
	}
	if !strings.Contains(toonErr, "boom") {
		t.Errorf("toon error missing message: %q", toonErr)
	}
}
