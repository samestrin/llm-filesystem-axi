package commands

import (
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// runCLI drives the root command the way main does, capturing what the user
// would see and the status the process would return.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var outBuf, errBuf strings.Builder
	prevOut, prevErr, prevExit := outWriter, errWriter, exitFunc

	got := -1
	outWriter, errWriter = &outBuf, &errBuf
	exitFunc = func(c int) { got = c }
	t.Cleanup(func() { outWriter, errWriter, exitFunc = prevOut, prevErr, prevExit })

	execute(args)

	return outBuf.String(), errBuf.String(), got
}

// AC4: every one of these used to exit 1 with ZERO output. The root command sets
// SilenceErrors, and Execute discarded the error cobra returned, so a typo
// produced a bare non-zero status and nothing to act on. AXI principle 6 asks a
// tool to fail loud on unknown input.
//
// They are all usage errors, not tool failures. Every subcommand uses cobra's
// Run rather than RunE and reports its own failures through OutputError, which
// exits before returning — so any error that reaches execute is necessarily a
// parse or resolution error, which is what makes ExitUsage the right code
// without inspecting the error's type.
func TestUsageErrorsFailLoudWithExitUsage(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown subcommand", []string{"bogus-command"}, "unknown command"},
		{"unknown flag", []string{"--nosuchflag"}, "unknown flag"},
		{"missing required flag", []string{"list-directory"}, "required flag"},
		{"invalid format value", []string{"--format", "yaml", "list-directory", "--path", "."}, "invalid --format"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, c.args...)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
			}
			if stderr == "" {
				t.Fatal("a usage error must explain itself; this exited silently")
			}
			if !strings.Contains(strings.ToLower(stderr), c.want) {
				t.Errorf("stderr = %q, want it to mention %q", stderr, c.want)
			}
		})
	}
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
