package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

func TestBinaryBuilds(t *testing.T) {
	// This test ensures the binary compiles successfully
	cmd := exec.Command("go", "build", "-o", "/dev/null", ".")
	cmd.Dir = "."
	if err := cmd.Run(); err != nil {
		t.Fatalf("Binary failed to build: %v", err)
	}
}

// requireBinary returns the path to the built CLI. Locally an absent binary
// skips; when CI is set it fails, because a skipped end-to-end test in CI
// reports a green run that exercised nothing (TD #24).
func requireBinary(t *testing.T) string {
	t.Helper()

	const path = "../../build/llm-filesystem"
	switch missingBinary(path) {
	case binaryFatal:
		t.Fatal("binary not built; CI must run make build before go test")
	case binarySkip:
		t.Skip("binary not built; run make build")
	}
	return path
}

type binaryState int

const (
	binaryPresent binaryState = iota
	binarySkip
	binaryFatal
)

// missingBinary decides what an absent binary means: fatal under CI, a skip
// elsewhere. Split out so a test can prove the CI branch without needing the
// real ./build/ to be absent.
func missingBinary(path string) binaryState {
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return binaryPresent
	}
	if os.Getenv("CI") != "" {
		return binaryFatal
	}
	return binarySkip
}

// Defends TD #24: if the CI branch is deleted, end-to-end tests silently go
// back to skipping under CI and a green run proves nothing.
func TestMissingBinaryIsFatalUnderCI(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "no-such-binary")

	t.Setenv("CI", "1")
	if got := missingBinary(absent); got != binaryFatal {
		t.Errorf("CI set, binary absent: got %v, want binaryFatal", got)
	}
	t.Setenv("CI", "")
	if got := missingBinary(absent); got != binarySkip {
		t.Errorf("CI unset, binary absent: got %v, want binarySkip", got)
	}
	t.Setenv("CI", "1")
	if got := missingBinary(os.Args[0]); got != binaryPresent {
		t.Errorf("binary present: got %v, want binaryPresent", got)
	}
}

// runBinary returns the combined output and the real process exit status.
func runBinary(t *testing.T, args ...string) (string, int) {
	t.Helper()

	out, err := exec.Command(requireBinary(t), args...).CombinedOutput()
	if err == nil {
		return string(out), 0
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("running %v: %v", args, err)
	}
	return string(out), exitErr.ExitCode()
}

// This used to assert nothing. The exit-code check sat inside "if err != nil",
// so a correct exit 0 left "err" nil and skipped the whole block — the status
// was never positively asserted in either direction.
func TestHelpFlag(t *testing.T) {
	output, code := runBinary(t, "--help")

	if code != 0 {
		t.Errorf("--help exit = %d, want 0\nOutput: %s", code, output)
	}
	if len(output) == 0 {
		t.Error("--help produced no output")
	}
}

// The version flags on the real binary, stamped by make build's ldflags. All
// three must print the same single line and exit 0, with no "llm-filesystem
// version" prefix and no "v" from the tag. Before this, -V exited 2 and the
// other two printed "llm-filesystem version v1.0.1".
func TestVersionFlagsPrintOneBareLine(t *testing.T) {
	want, code := runBinary(t, "--version")
	if code != 0 {
		t.Fatalf("--version exit = %d, want 0\nOutput: %s", code, want)
	}
	line := strings.TrimSuffix(want, "\n")
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("--version must print exactly one line: %q", want)
	}
	if strings.Contains(line, "llm-filesystem") || strings.HasPrefix(line, "v") {
		t.Errorf("--version must print the bare version: %q", line)
	}

	for _, args := range [][]string{
		{"-v"}, {"-V"},
		{"-V", "--format", "json"}, {"--format", "text", "-v"}, {"--axi", "--version"},
	} {
		got, code := runBinary(t, args...)
		if code != 0 {
			t.Errorf("%v exit = %d, want 0\nOutput: %s", args, code, got)
		}
		if got != want {
			t.Errorf("%v printed %q, want the same line as --version: %q", args, got, want)
		}
	}
}

// End-to-end proof of the fail-loud contract, against the real process rather
// than the injected exit hook. Each of these used to exit 1 printing nothing.
func TestUsageErrorsExitTwoWithADiagnostic(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"unknown subcommand", []string{"bogus-command"}},
		{"unknown flag", []string{"--nosuchflag"}},
		{"missing required flag", []string{"list-directory"}},
		{"invalid format", []string{"--format", "yaml", "list-directory", "--path", "."}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			output, code := runBinary(t, c.args...)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
			}
			if strings.TrimSpace(output) == "" {
				t.Error("exited silently; a usage error must explain itself")
			}
		})
	}
}

// A real tool failure is ExitError, not ExitUsage, so the two stay tellable
// apart from outside the process.
func TestToolFailureExitsOne(t *testing.T) {
	output, code := runBinary(t, "read-file", "--path", "/definitely/not/here.txt")

	if code != int(goaxi.ExitError) {
		t.Errorf("exit = %d, want ExitError (%d)\nOutput: %s", code, goaxi.ExitError, output)
	}
	if strings.TrimSpace(output) == "" {
		t.Error("a tool failure must emit a structured body")
	}
}

// The README's Limits section and the catalog entry's exception text say
// that with --format text, errors go to stderr — with a qualifier this test
// pins. A flag parse failure is rendered through the format parsed SO FAR:
// "--format text" placed AFTER the offending flag is never parsed, so the
// diagnostic falls back to the default toon on stdout. Without the qualifier
// the docs claim stdout is empty in a case where it carries 200+ bytes.
func TestFormatTextStderrOnlyOnceFormatIsParsed(t *testing.T) {
	bin := requireBinary(t)

	run := func(args ...string) (stdout, stderr string, code int) {
		t.Helper()
		var outBuf, errBuf strings.Builder
		cmd := exec.Command(bin, args...)
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		if err == nil {
			return outBuf.String(), errBuf.String(), 0
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running %v: %v", args, err)
		}
		return outBuf.String(), errBuf.String(), exitErr.ExitCode()
	}

	stdout, stderr, code := run("read-file", "--format", "text", "--bogus")
	if code != int(goaxi.ExitUsage) {
		t.Errorf("--format text --bogus: exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if stdout != "" {
		t.Errorf("--format text --bogus: stdout = %q, want empty once --format text is parsed", stdout)
	}
	if stderr == "" {
		t.Error("--format text --bogus: stderr is empty; the usage error must go there")
	}

	stdout, stderr, code = run("read-file", "--bogus", "--format", "text")
	if code != int(goaxi.ExitUsage) {
		t.Errorf("--bogus --format text: exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if stdout == "" {
		t.Error("--bogus --format text: stdout is empty; the unparsed --format must leave the toon default writing to stdout")
	}
	if stderr != "" {
		t.Errorf("--bogus --format text: stderr = %q, want empty (toon diagnostics go to stdout)", stderr)
	}
}

// Runs the skill's archive example (SKILL.md: compress a directory to
// release.tar.gz, extract it to ./out) against the real binary, in a working
// directory, with the same relative paths. The archive commands once reported
// success while writing a type other than the one the extension named, so the
// archive could not be read back. Assert the extracted files, not the exit
// code: only the bytes on disk show the round trip worked.
func TestSkillArchiveExampleRoundTrips(t *testing.T) {
	bin, err := filepath.Abs(requireBinary(t))
	if err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	files := map[string]string{
		"dist/app.txt":         "top-level file\n",
		"dist/nested/deep.txt": "nested file\n",
	}
	for rel, body := range files {
		p := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("compress-files", "--paths", "dist", "--output", "release.tar.gz")
	run("extract-archive", "--archive", "release.tar.gz", "--dest", "./out")

	// The archive may keep or drop the leading "dist/"; accept either layout.
	for rel, want := range files {
		found := false
		for _, cand := range []string{rel, strings.TrimPrefix(rel, "dist/")} {
			got, err := os.ReadFile(filepath.Join(work, "out", cand))
			if err != nil {
				continue
			}
			found = true
			if string(got) != want {
				t.Errorf("%s extracted as %q, want %q", cand, got, want)
			}
		}
		if !found {
			t.Errorf("%s not found under out/ after the round trip", rel)
		}
	}
}
