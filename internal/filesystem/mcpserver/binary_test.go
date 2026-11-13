package mcpserver

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeLookup struct {
	env     map[string]string
	pathHit string
	exe     string
}

func (f fakeLookup) resolve() (string, error) {
	return resolveBinary(
		func(k string) string { return f.env[k] },
		func(string) (string, error) {
			if f.pathHit != "" {
				return f.pathHit, nil
			}
			return "", errors.New("not found")
		},
		func() (string, error) { return f.exe, nil },
	)
}

func executable(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("exec bit checks are POSIX")
	}
	p := filepath.Join(dir, "llm-filesystem")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// The order is the plan's: LLM_FILESYSTEM_BINARY, then PATH, then beside the
// running server. The variable wins over a PATH hit, which is what lets CI
// and an operator point at a specific build.
func TestResolveBinaryOrder(t *testing.T) {
	env := executable(t, t.TempDir())
	onPath := executable(t, t.TempDir())
	serverDir := t.TempDir()
	sibling := executable(t, serverDir)
	server := filepath.Join(serverDir, "llm-filesystem-mcp")

	cases := []struct {
		name string
		f    fakeLookup
		want string
	}{
		{"variable", fakeLookup{env: map[string]string{BinaryEnvVar: env}, pathHit: onPath, exe: server}, env},
		{"PATH", fakeLookup{pathHit: onPath, exe: server}, onPath},
		{"sibling", fakeLookup{exe: server}, sibling},
	}
	for _, c := range cases {
		got, err := c.f.resolve()
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}

// A variable that points nowhere is an error, not a cue to quietly run a
// different llm-filesystem from PATH than the one the operator named.
func TestResolveBinaryBadVariableIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	_, err := fakeLookup{env: map[string]string{BinaryEnvVar: missing}, pathHit: "/usr/bin/llm-filesystem"}.resolve()
	if err == nil {
		t.Fatal("a missing LLM_FILESYSTEM_BINARY fell through to PATH")
	}
	for _, want := range []string{BinaryEnvVar, missing} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// Defends the plan's not-found rule: the error names the variable and every
// path tried, so a broken install is fixable from the error alone.
func TestResolveBinaryNotFoundNamesEverything(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	t.Setenv("PATH", dirA+string(os.PathListSeparator)+dirB)
	serverDir := t.TempDir()
	// Present but not executable: must not count as found.
	if err := os.WriteFile(filepath.Join(serverDir, "llm-filesystem"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := fakeLookup{exe: filepath.Join(serverDir, "llm-filesystem-mcp")}.resolve()
	if err == nil {
		t.Fatal("found a binary that is not there")
	}
	for _, want := range []string{BinaryEnvVar, filepath.Join(dirA, "llm-filesystem"), filepath.Join(dirB, "llm-filesystem"), filepath.Join(serverDir, "llm-filesystem")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q:\n%s", want, err)
		}
	}
}

// The production entry point resolves with the real environment.
func TestResolveBinaryUsesTheEnvironment(t *testing.T) {
	bin := executable(t, t.TempDir())
	t.Setenv(BinaryEnvVar, bin)
	got, err := ResolveBinary()
	if err != nil || got != bin {
		t.Errorf("ResolveBinary = %q, %v; want %q", got, err, bin)
	}
}
