package commands

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
	"github.com/spf13/cobra"
)

// helpFlagLine matches one row of cobra's "Flags:" block: an optional "-s, "
// short form, then the long name.
var helpFlagLine = regexp.MustCompile(`^\s+(?:-(\w), )?--([\w-]+)`)

// flagsFromHelp runs `<cmd> --help` and returns the command's own flags in the
// form the unknown-flag guidance uses, read from the "Flags:" block only. Global
// Flags are not the command's own and are left out.
func flagsFromHelp(t *testing.T, cmd ...string) []string {
	t.Helper()

	stdout, _, _ := runCLI(t, append(cmd, "--help")...)
	var flags []string
	inFlags := false
	for _, line := range strings.Split(stdout, "\n") {
		switch {
		case line == "Flags:":
			inFlags = true
			continue
		case inFlags && strings.TrimSpace(line) == "":
			return flags
		}
		if !inFlags {
			continue
		}
		m := helpFlagLine.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("unparseable flag row in %s --help: %q", cmd, line)
		}
		f := "--" + m[2]
		if m[1] != "" {
			f += " (-" + m[1] + ")"
		}
		flags = append(flags, f)
	}
	if len(flags) == 0 {
		t.Fatalf("%s --help has no Flags: block:\n%s", cmd, stdout)
	}
	return flags
}

// unknownFlagSteps runs `<cmd> --bogus` under --format json, asserts it is a
// usage error, and returns the guidance the document carries.
func unknownFlagSteps(t *testing.T, cmd ...string) []string {
	t.Helper()

	args := append(append([]string{"--format", "json"}, cmd...), "--bogus")
	stdout, stderr, code := runCLI(t, args...)
	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	var doc struct {
		NextSteps []string `json:"next_steps"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, stdout)
	}
	return doc.NextSteps
}

// listedFlags pulls the flag list out of the guidance entry for path.
func listedFlags(t *testing.T, steps []string, path string) []string {
	t.Helper()

	prefix := "Flags of " + path + ": "
	for _, s := range steps {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			list, _, found := strings.Cut(rest, "; ")
			if !found {
				t.Fatalf("flag entry has no --help pointer after the list: %q", s)
			}
			return strings.Split(list, ", ")
		}
	}
	t.Fatalf("no entry starting %q in %q", prefix, steps)
	return nil
}

// An unknown flag used to answer with only "see --help", so an agent that had
// guessed a flag name needed a second call to learn the real ones. The error
// now names them. Comparing against the command's own --help, rather than a
// hard-coded list, is what keeps the two from drifting when a flag is added.
func TestUnknownFlagListsTheCommandsOwnFlags(t *testing.T) {
	steps := unknownFlagSteps(t, "read-file")

	got := listedFlags(t, steps, "llm-filesystem read-file")
	want := flagsFromHelp(t, "read-file")
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("listed flags = %q, want read-file --help's %q", got, want)
	}

	hasPath := false
	for _, f := range got {
		if f == "--path" {
			hasPath = true
		}
	}
	if !hasPath {
		t.Errorf("listed flags = %q, want --path among them", got)
	}

	// The list is a summary; --help still carries types and meanings, so the
	// pointer must survive alongside it.
	if !strings.HasSuffix(steps[0], "llm-filesystem read-file --help") {
		t.Errorf("entry = %q, want it to end with the --help pointer", steps[0])
	}
}

// A command whose only own flag is cobra's --help must still get a list that
// matches its --help, not an empty "Flags of x: ;" fragment.
func TestUnknownFlagOnACommandWithOnlyHelp(t *testing.T) {
	steps := unknownFlagSteps(t, "list-allowed-directories")

	got := listedFlags(t, steps, "llm-filesystem list-allowed-directories")
	want := flagsFromHelp(t, "list-allowed-directories")
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("listed flags = %q, want %q", got, want)
	}
}

// A command with no flags at all must fall back to the bare pointer rather than
// a list with nothing in it. No real command qualifies — cobra gives each one
// --help — so a bare command stands in for the one that someday might.
func TestUsageStepsForACommandWithNoFlags(t *testing.T) {
	steps := usageSteps(&cobra.Command{Use: "bare"}, flagError{errors.New("unknown flag: --bogus")})
	if len(steps) != 1 || steps[0] != "Valid flags and subcommands: bare --help" {
		t.Errorf("steps = %q, want the bare --help pointer", steps)
	}
}

// Hidden flags stay hidden: the root accepts -V and --axi but --help does not
// advertise them, so the list must not either. Root is also where the flags are
// persistent ones, which LocalFlags reports and a naive Flags() walk would not.
func TestUnknownFlagAtRootMatchesRootHelp(t *testing.T) {
	steps := unknownFlagSteps(t)

	got := listedFlags(t, steps, "llm-filesystem")
	want := flagsFromHelp(t)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("listed flags = %q, want root --help's %q", got, want)
	}
	for _, f := range got {
		if strings.Contains(f, "axi") || strings.Contains(f, "version-alias") {
			t.Errorf("listed a hidden flag: %q", f)
		}
	}
}

// Only a flag error gets the list. An unknown subcommand already has its own
// remedy in the pointer, and appending the root's flags to it would answer a
// question the caller did not ask.
func TestUnknownSubcommandKeepsTheBarePointer(t *testing.T) {
	stdout, _, code := runCLI(t, "--format", "json", "bogus-command")
	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	var doc struct {
		NextSteps []string `json:"next_steps"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, stdout)
	}
	if len(doc.NextSteps) != 1 || doc.NextSteps[0] != "Valid flags and subcommands: llm-filesystem --help" {
		t.Errorf("next_steps = %q, want only the bare pointer", doc.NextSteps)
	}
}

// The list must reach stdout in TOON under the default and under --axi, where
// an agent reads. Text sends diagnostics to stderr by design (emitDiagnostic)
// and is covered by TestUsageErrorsAreStructuredWithExitUsage.
func TestUnknownFlagListReachesStdout(t *testing.T) {
	for _, args := range [][]string{
		{"read-file", "--bogus"},
		{"--axi", "read-file", "--bogus"},
		{"read-file", "--axi", "--bogus"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, code := runCLI(t, args...)
			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if !hasTOONField(stdout, "error") || !hasTOONField(stdout, "help") {
				t.Fatalf("stdout = %q, want a TOON error with a help block", stdout)
			}
			if !strings.Contains(stdout, "Flags of llm-filesystem read-file: ") ||
				!strings.Contains(stdout, "--path") {
				t.Errorf("stdout = %q, want the read-file flag list", stdout)
			}
		})
	}
}
