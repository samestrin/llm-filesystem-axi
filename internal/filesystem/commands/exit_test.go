package commands

import (
	"encoding/json"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// runCLI drives the root command the way main does, capturing what the user
// would see and the status the process would return.
//
// It restores the resolved output globals as well as the sinks. execute runs
// PersistentPreRunE, which assigns activeFmt/activeCompact/activeFull/
// activeFields, and those would otherwise survive into whatever test ran next —
// a cross-test leak that reads as a real failure in a file that never touched
// them. The renderer tests only escape it today because they all call
// withFormat first.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var outBuf, errBuf strings.Builder
	prevOut, prevErr, prevExit := outWriter, errWriter, exitFunc
	prevFmt, prevCompact, prevFull, prevFields := activeFmt, activeCompact, activeFull, activeFields
	prevListKey, prevCmdPath := activeListKey, activeCmdPath

	got := -1
	outWriter, errWriter = &outBuf, &errBuf
	exitFunc = func(c int) { got = c }
	t.Cleanup(func() {
		outWriter, errWriter, exitFunc = prevOut, prevErr, prevExit
		activeFmt, activeCompact, activeFull, activeFields = prevFmt, prevCompact, prevFull, prevFields
		activeListKey, activeCmdPath = prevListKey, prevCmdPath
	})

	execute(args)

	return outBuf.String(), errBuf.String(), got
}

// assertStructuredError checks that body is a parseable error document in f,
// carrying the error/message pair renderError produces. Text is exempt by
// design: it is the human format and keeps its pre-AXI "Error: <msg>" shape.
func assertStructuredError(t *testing.T, f Format, body, want string) {
	t.Helper()

	switch f {
	case FormatJSON:
		var doc struct {
			Error   bool   `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			t.Fatalf("body is not parseable JSON: %v\nbody = %q", err, body)
		}
		if !doc.Error {
			t.Error(`want "error": true`)
		}
		if !strings.Contains(strings.ToLower(doc.Message), want) {
			t.Errorf("message = %q, want it to mention %q", doc.Message, want)
		}
	case FormatTOON:
		if !strings.Contains(body, "error: true") {
			t.Errorf("body = %q, want a TOON error: true field", body)
		}
		if !strings.Contains(body, "message:") {
			t.Errorf("body = %q, want a TOON message field", body)
		}
	case FormatText:
		if !strings.HasPrefix(body, "Error: ") {
			t.Errorf("body = %q, want the pre-AXI %q prefix", body, "Error: ")
		}
	}
}

// AC7: a usage error used to be plain text on stderr regardless of --format, so
// an agent running --format json got nothing parseable back from a typo, and
// stdout — the stream it actually reads — stayed empty. AXI asks for structured
// errors on stdout and reserves stderr for logs.
//
// This replaces TestUsageErrorsFailLoudWithExitUsage (AC4). Every assertion that
// test made survives here: the exit code, the refusal to exit silently, and the
// cause substring. Each is retargeted to the stream its format actually uses,
// and joined by two the old test could not make — the body parses as a document,
// and the other stream stays empty so a diagnostic is never reported twice.
//
// They are all usage errors, not tool failures. Every subcommand uses cobra's
// Run rather than RunE and reports its own failures through OutputError, which
// exits before returning — so any error that reaches execute is necessarily a
// parse or resolution error, which is what makes ExitUsage the right code
// without inspecting the error's type.
func TestUsageErrorsAreStructuredWithExitUsage(t *testing.T) {
	causes := []struct {
		name string
		args []string
		want string
	}{
		{"unknown subcommand", []string{"bogus-command"}, "unknown command"},
		{"unknown flag", []string{"--nosuchflag"}, "unknown flag"},
		{"missing required flag", []string{"list-directory"}, "required flag"},
	}

	for _, c := range causes {
		for _, f := range []Format{FormatTOON, FormatJSON, FormatText} {
			t.Run(c.name+"/"+string(f), func(t *testing.T) {
				args := append([]string{"--format", string(f)}, c.args...)
				stdout, stderr, code := runCLI(t, args...)

				if code != int(goaxi.ExitUsage) {
					t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
				}

				// Text keeps its pre-AXI home on stderr; the structured
				// formats must reach stdout, where an agent reads.
				body, quiet := stdout, stderr
				if f == FormatText {
					body, quiet = stderr, stdout
				}

				if body == "" {
					t.Fatal("a usage error must explain itself; this exited silently")
				}
				if !strings.Contains(strings.ToLower(body), c.want) {
					t.Errorf("body = %q, want it to mention %q", body, c.want)
				}
				if quiet != "" {
					t.Errorf("other stream = %q, want empty; one diagnostic, one stream", quiet)
				}

				assertStructuredError(t, f, body, c.want)
			})
		}
	}
}

// End to end: a usage error must reach STDOUT even when an earlier flag's value
// happens to look like a format request. Getting this wrong is not cosmetic —
// text is the one format that also changes which stream the diagnostic lands
// on, so a misread silently empties stdout for the agent reading it.
func TestUsageErrorStaysOnStdoutDespiteAMisleadingValue(t *testing.T) {
	stdout, stderr, code := runCLI(t,
		"search-code", "--path", ".", "--pattern", "--min", "--bogusflag")

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if stdout == "" {
		t.Errorf("the diagnostic went to stderr because a flag VALUE was read as --min; stderr = %q", stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

// AC7: an invalid --format is the one usage error whose requested format cannot
// be honoured, because the value naming the format is itself what is being
// rejected. It must still produce a structured body rather than echoing the
// rejected name back as though it were valid, so it falls back to the TOON
// default. Deriving the format from argv rather than from activeFmt is what
// makes this deterministic: PersistentPreRunE returns before assigning, so
// activeFmt here still holds whatever the previous test left behind.
func TestInvalidFormatReportsItselfInTheDefaultFormat(t *testing.T) {
	stdout, stderr, code := runCLI(t, "--format", "yaml", "list-directory", "--path", ".")

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty; the body belongs on stdout", stderr)
	}
	if !strings.Contains(strings.ToLower(stdout), "invalid --format") {
		t.Errorf("stdout = %q, want it to mention %q", stdout, "invalid --format")
	}
	assertStructuredError(t, FormatTOON, stdout, "invalid --format")
}

// A usage error is not a tool failure, and an agent cannot decide whether a
// retry is worthwhile if the two share a code.
func TestUsageAndToolFailureUseDifferentCodes(t *testing.T) {
	if goaxi.ExitUsage == goaxi.ExitError {
		t.Fatal("ExitUsage and ExitError must differ")
	}

	_, _, usage := runCLI(t, "bogus-command")

	withFormat(t, FormatText, false, false)
	_, _, failure := captureOutput(t, func() { OutputError(errBoom{}) })

	if usage == failure {
		t.Errorf("usage and tool failure both exited %d", usage)
	}
	if failure != int(goaxi.ExitError) {
		t.Errorf("tool failure exit = %d, want ExitError (%d)", failure, goaxi.ExitError)
	}
}

// A successful run must report success explicitly, not by omission.
func TestSuccessExitsExitOK(t *testing.T) {
	_, _, code := runCLI(t, "list-allowed-directories")

	if code != int(goaxi.ExitOK) && code != -1 {
		t.Errorf("exit = %d, want ExitOK (%d) or no explicit exit", code, goaxi.ExitOK)
	}
}

// --help and --version are not errors.
func TestHelpAndVersionExitZero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, stderr, code := runCLI(t, args...)

			if code != int(goaxi.ExitOK) && code != -1 {
				t.Errorf("exit = %d, want ExitOK (%d) or no explicit exit", code, goaxi.ExitOK)
			}
			if strings.Contains(strings.ToLower(stderr), "error") {
				t.Errorf("stderr = %q, want no error for a help request", stderr)
			}
		})
	}
}

// The codes must come from the shared constants, so this tool and every other
// AXI agree. A local literal would drift.
func TestExitCodesComeFromGoAxi(t *testing.T) {
	if goaxi.ExitOK != 0 {
		t.Errorf("ExitOK = %d, want 0", goaxi.ExitOK)
	}
	if goaxi.ExitError != 1 {
		t.Errorf("ExitError = %d, want 1", goaxi.ExitError)
	}
	if goaxi.ExitUsage != 2 {
		t.Errorf("ExitUsage = %d, want 2", goaxi.ExitUsage)
	}
}
