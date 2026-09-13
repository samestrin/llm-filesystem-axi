package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	goaxi "github.com/samestrin/go-axi"
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
	return encodeTOON(v)
}

// encodeTOON is the single TOON encoder for the whole CLI.
//
// go-axi rather than a bare codec, for two things it adds.
//
// It sanitizes. File names and file contents are text this tool did not author,
// and toon-go passes U+2028, U+2029, lone C1 bytes and invalid UTF-8 straight
// through, so without this a payload carrying a raw escape sequence reaches
// whatever terminal renders the output. Stripping joins the surrounding visible
// text rather than dropping the field.
//
// And it guards against silent loss. toon-go supports neither defined string
// types nor encoding.TextMarshaler, and a violating type does not error — it
// emits EMPTY output, so the command prints nothing and exits zero.
//
// EncodeChecked rather than Check followed by Encode. That pair sanitizes and
// marshals the same value twice to serve one guard; EncodeChecked derives its
// verdict from the bytes it writes. Medians of six runs on a 2000-row payload,
// go-axi v0.2.1 on an M5, with a bare Encode as the floor:
//
//	Encode    EncodeChecked    Check+Encode
//	1.02ms    1.17ms (+15%)    2.78ms (+173%)
//
// Rerun them rather than trust them, from a go-axi checkout:
//
//	go test -run '^$' -bench Output -benchmem
//
// The guard costs about 15% here. An earlier version of this function dropped it
// to avoid the 2.7x that Check+Encode cost, which was the wrong trade: the cost
// was duplicated work, not safety, and it was fixable in the library.
//
// Returning "" alongside the error matters: no caller may write a partial body.
func encodeTOON(v interface{}) (string, error) {
	var buf bytes.Buffer
	if _, err := goaxi.EncodeChecked(&buf, v); err != nil {
		return "", err
	}

	// EncodeChecked terminates with exactly one newline; the caller assembles the
	// document and writes it once. Trimming keeps the string-return contract
	// these renderers have always had.
	return strings.TrimSuffix(buf.String(), "\n"), nil
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
		s, encErr := encodeTOON(map[string]interface{}{"error": true, "message": err.Error()})
		if encErr != nil {
			b, _ := json.Marshal(map[string]interface{}{"error": true, "message": err.Error()})
			return string(b)
		}
		return s
	default:
		return "Error: " + err.Error()
	}
}
