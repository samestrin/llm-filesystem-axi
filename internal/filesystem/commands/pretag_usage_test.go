package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// edit-block --old x without --new deleted the match and exited 0; safe-edit
// did the same; edit-file replace or insert without --content blanked a line or
// inserted an empty one (EST-8264 item 3). The JSON payloads already require
// the replacement; the flags now do too. An explicit "" still works.
func TestEditsRequireTheReplacement(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"edit-block without --new", []string{"edit-block", "--old", "two"}},
		{"safe-edit without --new", []string{"safe-edit", "--old", "two"}},
		{"edit-file replace without --content", []string{"edit-file", "--operation", "replace", "--line", "2"}},
		{"edit-file insert without --content", []string{"edit-file", "--operation", "insert", "--line", "2"}},
		{"edit-file unknown operation", []string{"edit-file", "--operation", "frob", "--line", "2", "--content", "x"}},
		{"edit-block with an empty --old", []string{"edit-block", "--old", "", "--new", "x"}},
		{"safe-edit with an empty --old", []string{"safe-edit", "--old", "", "--new", "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "f.txt")
			mustWrite(t, f, "one\ntwo\nthree\n")
			_, _, code := runCLI(t, append(tc.args, "--path", f)...)
			if code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			if got := readString(t, f); got != "one\ntwo\nthree\n" {
				t.Errorf("file changed to %q", got)
			}
			mustNotExist(t, f+".bak")
		})
	}

	// Known-good: an explicit empty replacement is a deliberate deletion.
	t.Run("an explicit empty --new deletes", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "f.txt")
		mustWrite(t, f, "one\ntwo\n")
		if _, _, code := runCLI(t, "edit-block", "--path", f, "--old", "two\n", "--new", ""); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
		if got := readString(t, f); got != "one\n" {
			t.Errorf("file is %q", got)
		}
	})
	t.Run("an explicit empty --content replaces", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "f.txt")
		mustWrite(t, f, "one\ntwo\n")
		if _, _, code := runCLI(t, "edit-file", "--path", f, "--operation", "replace", "--line", "2", "--content", ""); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
	})
	t.Run("delete needs no --content", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "f.txt")
		mustWrite(t, f, "one\ntwo\n")
		if _, _, code := runCLI(t, "edit-file", "--path", f, "--operation", "delete", "--line", "2"); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
		if got := readString(t, f); got != "one\n" {
			t.Errorf("file is %q", got)
		}
	})
}

// Malformed invocations that exited 1, the code for "the tool tried and
// failed", which invites a retry of the same command (EST-8264 item 7).
func TestMalformedInvocationsExit2(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	mustWrite(t, src, "x")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"write-multiple-files with no entries", []string{"write-multiple-files", "--files", "[]"}},
		{"batch-file-operations with no entries", []string{"batch-file-operations", "--operations", "[]"}},
		{"copy-file with an empty source", []string{"copy-file", "--source", "", "--dest", filepath.Join(dir, "d.txt")}},
		{"delete-file with an empty path", []string{"delete-file", "--path", "", "--confirm"}},
		{"write-file with an empty path", []string{"write-file", "--path", "", "--content", "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, code := runCLI(t, tc.args...); code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
		})
	}

	// An empty --dest normalized to the working directory, which exists, so
	// the refusal said the copy "would overwrite an existing destination".
	t.Run("copy-file with an empty dest", func(t *testing.T) {
		stdout, _, code := runCLI(t, "copy-file", "--source", src, "--dest", "")
		if code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		if strings.Contains(stdout, "overwrite") {
			t.Errorf("wrong refusal: %s", stdout)
		}
	})
}

// A trailing separator names a directory; the write made a file of that name
// instead. Duplicate paths in one write-multiple-files kept the last silently.
func TestWritePathShapes(t *testing.T) {
	t.Run("write-file to a trailing-slash path", func(t *testing.T) {
		d := t.TempDir()
		if _, _, code := runCLI(t, "write-file", "--path", filepath.Join(d, "newd")+"/", "--content", "x"); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		mustNotExist(t, filepath.Join(d, "newd"))
	})
	t.Run("large-write-file to a trailing-slash path", func(t *testing.T) {
		d := t.TempDir()
		if _, _, code := runCLI(t, "large-write-file", "--path", filepath.Join(d, "newd")+"/", "--content", "x"); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		mustNotExist(t, filepath.Join(d, "newd"))
	})
	t.Run("write-multiple-files with a trailing-slash path", func(t *testing.T) {
		d := t.TempDir()
		files := `[{"path":` + string(jsonString(filepath.Join(d, "newd")+"/")) + `,"content":"x"}]`
		if _, _, code := runCLI(t, "write-multiple-files", "--files", files); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		mustNotExist(t, filepath.Join(d, "newd"))
	})
	t.Run("write-multiple-files with a duplicate path", func(t *testing.T) {
		d := t.TempDir()
		p := filepath.Join(d, "a.txt")
		files := `[{"path":` + string(jsonString(p)) + `,"content":"first"},{"path":` +
			string(jsonString(filepath.Join(d, ".", "a.txt"))) + `,"content":"second"}]`
		if _, _, code := runCLI(t, "write-multiple-files", "--files", files); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		mustNotExist(t, p)
	})
}

// search-and-replace rewrote .git/config along with the working tree.
func TestSearchAndReplaceSkipsGitDir(t *testing.T) {
	d := t.TempDir()
	mustWrite(t, filepath.Join(d, ".git", "config"), "url = old\n")
	mustWrite(t, filepath.Join(d, "src.txt"), "old\n")
	if _, _, code := runCLI(t, "search-and-replace", "--path", d, "--pattern", "old", "--replacement", "new"); code != -1 {
		t.Fatalf("exit %d, want success", code)
	}
	if got := readString(t, filepath.Join(d, ".git", "config")); got != "url = old\n" {
		t.Errorf(".git/config rewritten to %q", got)
	}
	if got := readString(t, filepath.Join(d, "src.txt")); got != "new\n" {
		t.Errorf("working file is %q, want it replaced", got)
	}
}

// extract-archive created --dest before finding the archive was missing.
func TestExtractMissingArchiveCreatesNothing(t *testing.T) {
	d := t.TempDir()
	_, _, code := runCLI(t, "extract-archive", "--archive", filepath.Join(d, "missing.zip"), "--dest", filepath.Join(d, "out"))
	if code == -1 {
		t.Error("exit 0 for a missing archive")
	}
	mustNotExist(t, filepath.Join(d, "out"))
}
