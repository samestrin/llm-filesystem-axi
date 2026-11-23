package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// hardLinkJail builds an --allowed-dirs root A holding A/hl, a hard link to a
// file outside A. A hard link has no target to resolve, so the allowed-dirs
// check passed it, and write-file opened it O_TRUNC (or O_APPEND) in place,
// changing the outside file with exit 0 (EST-8338).
func hardLinkJail(t *testing.T) (allowed, secret, link string) {
	t.Helper()
	allowed, outside := t.TempDir(), t.TempDir()
	secret = filepath.Join(outside, "secret.txt")
	mustWrite(t, secret, "outside data")
	link = filepath.Join(allowed, "hl")
	if err := os.Link(secret, link); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	return allowed, secret, link
}

// The assertion is on the outside file's bytes: the bug exited 0, and only
// the outside file shows the write went through.
func TestAllowedDirsRefusesWriteThroughHardLink(t *testing.T) {
	cases := []struct {
		name string
		args func(a, link string) []string
	}{
		{"write-file", func(a, link string) []string {
			return []string{"--allowed-dirs", a, "write-file", "--path", link, "--content", "clobbered"}
		}},
		{"write-file --append", func(a, link string) []string {
			return []string{"--allowed-dirs", a, "write-file", "--path", link, "--content", "appended", "--append"}
		}},
		{"write-multiple-files", func(a, link string) []string {
			files := `[{"path":` + string(jsonString(link)) + `,"content":"clobbered"}]`
			return []string{"--allowed-dirs", a, "write-multiple-files", "--files", files}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, secret, link := hardLinkJail(t)
			_, _, code := runCLI(t, c.args(a, link)...)
			if got := readString(t, secret); got != "outside data" {
				t.Errorf("outside file now holds %q", got)
			}
			if code == -1 {
				t.Error("exit 0 for a write through a hard link under --allowed-dirs")
			}
		})
	}
}

// The refusal is a refusal of the request: nothing was written, so exit 2.
func TestAllowedDirsHardLinkRefusalExitsTwo(t *testing.T) {
	a, _, link := hardLinkJail(t)
	if _, _, code := runCLI(t, "--allowed-dirs", a, "write-file", "--path", link, "--content", "x"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

// Known-good: an ordinary file under --allowed-dirs still writes and appends,
// and without --allowed-dirs a hard link is written through as Unix does.
func TestHardLinkGateLeavesOrdinaryWritesAlone(t *testing.T) {
	a := t.TempDir()
	p := filepath.Join(a, "f.txt")
	mustWrite(t, p, "one")
	if _, _, code := runCLI(t, "--allowed-dirs", a, "write-file", "--path", p, "--content", "+two", "--append"); code != -1 {
		t.Errorf("append to a single-link file: exit %d", code)
	}
	if got := readString(t, p); got != "one+two" {
		t.Errorf("file is %q", got)
	}

	_, secret, link := hardLinkJail(t)
	if _, _, code := runCLI(t, "write-file", "--path", link, "--content", "shared"); code != -1 {
		t.Errorf("write through a hard link with no --allowed-dirs: exit %d", code)
	}
	if got := readString(t, secret); got != "shared" {
		t.Errorf("linked file is %q", got)
	}
}
