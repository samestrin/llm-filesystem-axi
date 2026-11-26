//go:build !windows

package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// A new file is created as the shell would create it, 0666 &^ umask, not with
// os.CreateTemp's 0600 that a rename would otherwise carry into place. The
// umask is set here and the mode asserted as a literal: comparing against
// processUmask() passed even when processUmask returned 0 and new files came
// out 0666, world-writable (review of PR #92).
func TestLargeWriteFileCreatesANewFileUnderTheUmask(t *testing.T) {
	old := syscall.Umask(0o027)
	t.Cleanup(func() { syscall.Umask(old) })

	p := filepath.Join(t.TempDir(), "new")
	if _, err := LargeWriteFile(LargeWriteFileOptions{Path: p, Content: "x"}); err != nil {
		t.Fatalf("LargeWriteFile() error = %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("new file mode under umask 027 = %v, want 0640", got)
	}
}

// Linux answers from /proc/self/status, so the set-and-restore fallback that
// macOS uses would otherwise run in no Linux test. Both must report the umask
// in force and leave it in force.
func TestProcessUmaskReadsTheUmaskInForce(t *testing.T) {
	old := syscall.Umask(0o027)
	t.Cleanup(func() { syscall.Umask(old) })

	for name, read := range map[string]func() fs.FileMode{"processUmask": processUmask, "umaskBySetting": umaskBySetting} {
		if got := read(); got != 0o027 {
			t.Errorf("%s() = %o, want 027", name, got)
		}
	}
	if now := syscall.Umask(0o027); now != 0o027 {
		t.Errorf("reading the umask changed it to %o", now)
	}
}
