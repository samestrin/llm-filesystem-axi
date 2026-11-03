package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAxiFlagIsANoOpAlias defends the owner decision that --axi, a habit from
// sibling AXI tools, must not exit 2 here. It is an alias for the default, so
// it must change nothing: same bytes, and it must not override an explicit
// JSON request.
func TestAxiFlagIsANoOpAlias(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := [][]string{
		{"list-directory", "--path", dir},
		{"list-directory", "--path", dir, "--format", "json"},
		{"list-directory", "--path", dir, "--json"},
	}
	for _, base := range cases {
		want, _, wantCode := runCLI(t, base...)
		got, errOut, code := runCLI(t, append([]string{"--axi"}, base...)...)
		if code != wantCode || code == 2 {
			t.Fatalf("%v: --axi exit = %d (want %d); stderr = %q", base, code, wantCode, errOut)
		}
		if got != want {
			t.Errorf("%v: --axi changed output\nwith:    %q\nwithout: %q", base, got, want)
		}
	}

	jsonOut, _, _ := runCLI(t, "--axi", "--format", "json", "list-directory", "--path", dir)
	if !strings.HasPrefix(strings.TrimSpace(jsonOut), "{") && !strings.HasPrefix(strings.TrimSpace(jsonOut), "[") {
		t.Errorf("--axi --format json should still print JSON, got %q", jsonOut)
	}
}

// TestAxiFlagIsHidden keeps the alias out of --help so it is not advertised.
func TestAxiFlagIsHidden(t *testing.T) {
	out, _, _ := runCLI(t, "--help")
	if strings.Contains(out, "--axi") {
		t.Errorf("--axi must be hidden from --help, got:\n%s", out)
	}
}
