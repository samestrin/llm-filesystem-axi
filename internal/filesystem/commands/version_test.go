package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// withVersion stamps Version for one test, the way the release ldflags do.
func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := Version
	Version = v
	t.Cleanup(func() { Version = prev })
}

// Defends the version contract: -v, -V and --version all print the bare
// version — the release tag without its "v" — as one line on stdout and exit 0,
// whatever output format is also asked for. Cobra's default printed
// "llm-filesystem version v1.0.1", knew nothing of -V (exit 2), and failed
// `--version --format json` as an unknown command "json", because its version
// flag is registered after the command is resolved.
func TestVersionFlagsPrintTheBareVersion(t *testing.T) {
	withVersion(t, "v1.0.2")

	modes := [][]string{
		nil,
		{"--format", "json"},
		{"--format", "text"},
		{"--format", "toon"},
		{"--json"},
		{"--min"},
		{"--axi"},
	}
	for _, flag := range []string{"-v", "-V", "--version"} {
		for _, mode := range modes {
			for _, args := range [][]string{
				append([]string{flag}, mode...),
				append(append([]string{}, mode...), flag),
			} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					stdout, stderr, code := runCLI(t, args...)

					if code != int(goaxi.ExitOK) && code != -1 {
						t.Errorf("exit = %d, want ExitOK", code)
					}
					if stdout != "1.0.2\n" {
						t.Errorf("stdout = %q, want %q", stdout, "1.0.2\n")
					}
					if stderr != "" {
						t.Errorf("stderr = %q, want nothing", stderr)
					}
				})
			}
		}
	}
}

// A build stamped without a leading "v" must print unchanged, so the strip
// removes a tag prefix and nothing else.
func TestVersionWithoutPrefixPrintsUnchanged(t *testing.T) {
	withVersion(t, "1.0.2")

	stdout, _, _ := runCLI(t, "-V")
	if stdout != "1.0.2\n" {
		t.Errorf("stdout = %q, want %q", stdout, "1.0.2\n")
	}
}

// -V belongs to the root only. A subcommand given it keeps failing loud as an
// unknown flag, rather than silently printing a version in place of the work
// it was asked to do. The read is otherwise valid, so a -V that leaked to the
// subcommand shows up as a successful read rather than hiding behind a
// missing --path.
func TestSubcommandVersionShorthandStaysAUsageError(t *testing.T) {
	withVersion(t, "v1.0.2")
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCLI(t, "read-file", "--path", path, "-V")
	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if !strings.Contains(stdout, "unknown shorthand flag: 'V'") {
		t.Errorf("want an unknown-flag diagnostic for -V: %q", stdout)
	}
	if strings.Contains(stdout, "1.0.2") {
		t.Errorf("a subcommand must not print the version: %q", stdout)
	}
}
