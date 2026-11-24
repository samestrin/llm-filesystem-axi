package core

import (
	"os"
	"path/filepath"
	"testing"
)

// linkCount is what the hard-link gate in WriteFile reads. On Windows it
// returned 1 unconditionally, so the gate never fired there and a write
// through a hard link changed the outside file with exit 0 (review of PR
// #88). This runs on every platform that runs the suite.
func TestLinkCountSeesASecondName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := linkCount(p, info); n != 1 {
		t.Errorf("single-link file: linkCount = %d, want 1", n)
	}
	if err := os.Link(p, filepath.Join(dir, "second")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	info, err = os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := linkCount(p, info); n != 2 {
		t.Errorf("hard-linked file: linkCount = %d, want 2", n)
	}
}
