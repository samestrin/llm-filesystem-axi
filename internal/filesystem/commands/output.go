package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

// formatFromArgs recovers the requested output format from raw argv.
//
// The usage-error path cannot consult activeFmt. PersistentPreRunE is what
// assigns it, and for an unknown flag or an unknown subcommand cobra fails while
// parsing — before that hook ever runs — so activeFmt still holds the package
// default, or whatever the previous invocation in this process left there.
// Re-scanning argv is the one source that is correct for every usage-error
// class, including the ones where the hook did run.
//
// An invalid --format falls back to TOON, because that value is itself the error
// being reported. resolveFormat already returns TOON alongside its error, so the
// error is discardable here, and only here.
//
// The scan walks root's command tree as it goes, so it knows which flags the
// resolved command declares. A token is skipped as a value only after a flag
// declared to take one: that is what keeps `--pattern --json` from being read as
// a format request. An unknown flag takes no value. Cobra stops at it, so the
// --format after it is never parsed, and this scan is the only thing that still
// sees it — `read-file --bogus --format text` answers in text.
//
// compress-files accepts --format zip|tar.gz|tgz as a deprecated alias for
// --archive-format; that value is not an output format, the same exception
// PersistentPreRunE makes.
func formatFromArgs(root *cobra.Command, args []string) (Format, bool) {
	var (
		formatVal string
		formatSet bool
		jsonFlag  bool
		minFlag   bool
	)

	cmd := root
	descending := true
	// setFormat records one --format, last one winning. On compress-files an
	// archive type unsets it, as PersistentPreRunE does with cobra's last value.
	setFormat := func(v string) {
		formatVal, formatSet = v, !(cmd.Name() == "compress-files" && isArchiveType(v))
	}

scan:
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			break scan
		case strings.HasPrefix(a, "--"):
			name, val, joined := strings.Cut(a[2:], "=")
			fl := lookupFlag(cmd, name, "")
			switch {
			case name == "format" && fl != nil:
				if joined {
					setFormat(val)
				} else if i+1 < len(args) {
					i++
					setFormat(args[i])
				}
			case name == "json" && fl != nil:
				jsonFlag = true
			case name == "min" && fl != nil:
				minFlag = true
			case fl != nil && !joined && takesValue(fl):
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// Shorthands, possibly combined (-ab). The first one that takes a
			// value swallows the rest of the token, or the next token if there
			// is no rest; an unknown one ends the token, as it ends pflag's parse.
			for j := 1; j < len(a); j++ {
				fl := lookupFlag(cmd, "", a[j:j+1])
				if fl == nil {
					break
				}
				if takesValue(fl) {
					if j == len(a)-1 {
						i++
					}
					break
				}
			}
		default:
			// A positional names a subcommand only until the first one that
			// does not, as in cobra's own resolution.
			if !descending {
				continue
			}
			if sub := findSubcommand(cmd, a); sub != nil {
				cmd = sub
			} else {
				descending = false
			}
		}
	}

	f, compact, _ := resolveFormat(formatVal, formatSet, jsonFlag, minFlag)
	return f, compact
}

// lookupFlag finds a flag by long name or by shorthand on cmd or on any
// ancestor's persistent flags. It reads the declarations directly because
// cobra merges inherited flags into cmd.Flags() only while parsing, and this
// runs after a parse that may have failed before getting there.
func lookupFlag(cmd *cobra.Command, name, shorthand string) *pflag.Flag {
	find := func(fs *pflag.FlagSet) *pflag.Flag {
		if name != "" {
			return fs.Lookup(name)
		}
		return fs.ShorthandLookup(shorthand)
	}
	if fl := find(cmd.Flags()); fl != nil {
		return fl
	}
	for c := cmd; c != nil; c = c.Parent() {
		if fl := find(c.PersistentFlags()); fl != nil {
			return fl
		}
	}
	return nil
}

// takesValue reports whether a bare --flag consumes the next token, the rule
// pflag applies: a flag with a NoOptDefVal (every bool) does not.
func takesValue(fl *pflag.Flag) bool {
	return fl.NoOptDefVal == ""
}

// findSubcommand returns cmd's subcommand named or aliased name, or nil.
func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name || sub.HasAlias(name) {
			return sub
		}
	}
	return nil
}

// textSteps renders recovery guidance for the human format, to be appended to a
// body that already ends in a newline. It returns "" for no steps, so a stepless
// command leaves no trailing blank line.
//
// Shared by the success path and the error path. They had separate copies that
// produced identical bytes, which is one copy too many for a format whose exact
// shape two tests pin.
func textSteps(steps []string) string {
	if len(steps) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\nNext steps:\n")
	for _, s := range steps {
		sb.WriteString("  - " + s + "\n")
	}
	return sb.String()
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
//
// steps is recovery guidance, and only the JSON branch consumes it. JSON has to
// stay one parseable document, so its guidance goes inside as next_steps — the
// same field and the same injector the success path uses. TOON and text carry
// theirs after the body, which emitDiagnostic appends. Passing nil steps leaves
// every byte of the pre-AXI output unchanged, because injectNextSteps no-ops on
// an empty slice.
func renderError(f Format, compact bool, err error, steps []string) string {
	switch f {
	case FormatText:
		if compact {
			return err.Error()
		}
		return "Error: " + err.Error()
	case FormatJSON:
		if compact {
			b, _ := json.Marshal(injectNextSteps(map[string]interface{}{"err": true, "msg": err.Error()}, steps))
			return string(b)
		}
		b, _ := json.MarshalIndent(injectNextSteps(map[string]interface{}{"error": true, "message": err.Error()}, steps), "", "  ")
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
