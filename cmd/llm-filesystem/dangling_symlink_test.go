package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// Defends debt row acf05a43daf6d146 (fixed in b2ecd11): destinationExists used
// os.Stat, which follows symlinks, so a dangling symlink at the destination
// read as "nothing there" and copy-file replaced the link with a regular file,
// exit 0, no --confirm. The in-process regression test lives in
// internal/filesystem/commands (TestDanglingSymlinkDestinationNeedsConfirm);
// this one runs the row's exact reproduction against the real binary, because
// that is how the row was reported and how a caller meets it.
func TestCopyFileDanglingSymlinkDestinationNeedsConfirm(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "f")
	if err := os.WriteFile(src, []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "nonexistent")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(missing, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	out, code := runBinary(t, "copy-file", "--source", src, "--dest", link)

	// A refusal is a usage error (exit 2), not a tool failure (exit 1) and
	// never success.
	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)\nOutput: %s", code, goaxi.ExitUsage, out)
	}
	if !strings.Contains(out, "would overwrite an existing destination") || !strings.Contains(out, "--confirm") {
		t.Errorf("output missing the --confirm refusal: %q", out)
	}

	// Side effects, not just the exit code: the link must still be a symlink
	// to its missing target, nothing may have been written through it, and
	// the source must be untouched.
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("destination is no longer a symlink: %v", err)
	}
	if target != missing {
		t.Errorf("symlink target = %q, want %q", target, missing)
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Errorf("copy wrote through the dangling link (err %v)", err)
	}
	b, err := os.ReadFile(src)
	if err != nil || string(b) != "payload\n" {
		t.Errorf("source changed: %q, %v", b, err)
	}
}
