package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alpkeskin/gotoon"
)

// Format is an output rendering mode.
type Format string

const (
	// FormatTOON is the token-efficient default (AXI principle #1).
	FormatTOON Format = "toon"
	// FormatJSON is machine-parseable JSON, byte-compatible with the pre-AXI --json flag.
	FormatJSON Format = "json"
	// FormatText is human-readable plain text.
	FormatText Format = "text"
)

// parseFormat validates and normalizes a --format value. Unknown values fail
// loud (AXI principle #6: reject unknown input rather than silently defaulting).
func parseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(s)) {
	case FormatTOON:
		return FormatTOON, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatText:
		return FormatText, nil
	default:
		return FormatTOON, fmt.Errorf("invalid --format %q (want toon, json, or text)", s)
	}
}

// resolveFormat selects the effective output format and compact flag from the
// new --format flag and the legacy --json/--min flags.
//
// Precedence: an explicit --format wins; otherwise legacy --json maps to JSON;
// otherwise a bare --min keeps its pre-AXI meaning (terse text); otherwise the
// TOON default applies. compact is derived from --min and only affects JSON.
func resolveFormat(formatFlag string, formatSet, jsonFlag, minFlag bool) (Format, bool, error) {
	compact := minFlag
	switch {
	case formatSet:
		f, err := parseFormat(formatFlag)
		if err != nil {
			return FormatTOON, compact, err
		}
		return f, compact, nil
	case jsonFlag:
		return FormatJSON, compact, nil
	case minFlag:
		return FormatText, compact, nil
	default:
		return FormatTOON, compact, nil
	}
}

// renderResult renders a result value in the given format. JSON output is kept
// byte-identical to the pre-AXI behavior. A TOON encode failure is surfaced as
// an error so the caller can fall back defensively.
func renderResult(f Format, compact bool, result interface{}, textFn func() string) (string, error) {
	switch f {
	case FormatText:
		return textFn(), nil
	case FormatJSON:
		if compact {
			b, err := json.Marshal(result)
			return string(b), err
		}
		b, err := json.MarshalIndent(result, "", "  ")
		return string(b), err
	case FormatTOON:
		// Round-trip through encoding/json so struct json tags — including
		// ,omitempty and json:"-" — are honored exactly as in --format json,
		// then re-encode the resulting generic value as TOON. Encoding the
		// struct directly would leak Go field names and omitempty options.
		generic, err := toGeneric(result)
		if err != nil {
			return "", err
		}
		return gotoon.Encode(generic)
	default:
		b, err := json.MarshalIndent(result, "", "  ")
		return string(b), err
	}
}

// renderGeneric renders an already-generic value (map/slice/scalar) as JSON or
// TOON. Used after minimal projection / next-step injection, which operate on
// the generic representation.
func renderGeneric(f Format, compact bool, v interface{}) (string, error) {
	if f == FormatJSON {
		if compact {
			b, err := json.Marshal(v)
			return string(b), err
		}
		b, err := json.MarshalIndent(v, "", "  ")
		return string(b), err
	}
	return gotoon.Encode(v)
}

// toGeneric marshals v to JSON and back into a tag-free generic value
// (map/slice/scalar) so downstream encoders see exactly the JSON projection.
func toGeneric(v interface{}) (interface{}, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic interface{}
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil, err
	}
	return generic, nil
}

// renderError renders an error in the given format. Text keeps the pre-AXI
// "Error: " prefix (or the bare message under compact); JSON keeps the pre-AXI
// key shapes; TOON emits the same structured error as a TOON document.
func renderError(f Format, compact bool, err error) string {
	switch f {
	case FormatText:
		if compact {
			return err.Error()
		}
		return "Error: " + err.Error()
	case FormatJSON:
		if compact {
			b, _ := json.Marshal(map[string]interface{}{"err": true, "msg": err.Error()})
			return string(b)
		}
		b, _ := json.MarshalIndent(map[string]interface{}{"error": true, "message": err.Error()}, "", "  ")
		return string(b)
	case FormatTOON:
		s, encErr := gotoon.Encode(map[string]interface{}{"error": true, "message": err.Error()})
		if encErr != nil {
			b, _ := json.Marshal(map[string]interface{}{"error": true, "message": err.Error()})
			return string(b)
		}
		return s
	default:
		return "Error: " + err.Error()
	}
}
