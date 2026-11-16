package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extract-archive caps total bytes and entry count against decompression
// bombs. A run stopped at a cap prints the result with truncated, exits 1, and
// its help names the flag that raises the cap that stopped it.
func TestExtractArchiveCapFlags(t *testing.T) {
	archive := writeArchive(t, "zip", [][2]string{{"a.txt", strings.Repeat("a", 64)}, {"b.txt", "b"}})

	cases := []struct {
		flag, value string
	}{
		{"--max-bytes", "10"},
		{"--max-entries", "1"},
	}
	for _, c := range cases {
		t.Run(c.flag, func(t *testing.T) {
			dest := t.TempDir()
			out, _, code := runCLI(t, "extract-archive", "--archive", archive, "--dest", dest, c.flag, c.value)
			if code != 1 {
				t.Errorf("a run stopped at a cap exited %d, want 1:\n%s", code, out)
			}
			if !hasTOONLine(out, "truncated: true") {
				t.Errorf("no truncated: true:\n%s", out)
			}
			i := strings.Index(out, "\nhelp[")
			if i < 0 || !strings.Contains(out[i:], c.flag) {
				t.Errorf("help does not name %s:\n%s", c.flag, out)
			}
			if _, err := os.Stat(filepath.Join(dest, "b.txt")); !os.IsNotExist(err) {
				t.Errorf("an entry past the cap was extracted: %v", err)
			}
		})
	}

	// 0 is neither a cap nor "no limit"; it is refused before anything is
	// written, as a usage error.
	for _, flag := range []string{"--max-bytes", "--max-entries"} {
		dest := t.TempDir()
		_, _, code := runCLI(t, "extract-archive", "--archive", archive, "--dest", dest, flag, "0")
		if code != 2 {
			t.Errorf("%s 0 exited %d, want 2", flag, code)
		}
		if entries, _ := os.ReadDir(dest); len(entries) != 0 {
			t.Errorf("%s 0 still extracted %d entries", flag, len(entries))
		}
	}
}
