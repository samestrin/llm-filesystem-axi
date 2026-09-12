package main

import (
	"errors"
	"os"
	"os/exec"
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

// requireBinary returns the path to the built CLI, skipping when it is absent.
func requireBinary(t *testing.T) string {
	t.Helper()

	const path = "../../build/llm-filesystem"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("binary not built; run make build")
	}
	return path
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
