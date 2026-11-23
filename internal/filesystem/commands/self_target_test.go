package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// countEntries counts everything under root, so a refusal that wrote first is
// caught even when it then exits with the right code.
func countEntries(t *testing.T, root string) int {
	t.Helper()
	n := 0
	filepath.Walk(root, func(string, os.FileInfo, error) error { n++; return nil })
	return n
}

// Copying a directory into itself recursed until the path was too long, left
// hundreds of junk files and exited 1; sync-directories with the destination
// inside the source ran too (EST-8264 item 5). It is a malformed request, so
// exit 2, refused before anything is written.
func TestCopyIntoItselfIsRefused(t *testing.T) {
	cases := []struct {
		name string
		args func(d string) []string
	}{
		{"copy-file into a subdirectory", func(d string) []string {
			return []string{"copy-file", "--source", d, "--dest", filepath.Join(d, "sub", "inner")}
		}},
		{"copy-file onto itself", func(d string) []string {
			return []string{"copy-file", "--source", d, "--dest", d, "--confirm"}
		}},
		{"move-file into a subdirectory", func(d string) []string {
			return []string{"move-file", "--source", d, "--dest", filepath.Join(d, "inner")}
		}},
		{"batch copy into a subdirectory", func(d string) []string {
			ops := `[{"operation":"copy","source":` + string(jsonString(filepath.Join(d, "f.txt"))) +
				`,"destination":` + string(jsonString(filepath.Join(d, "first.txt"))) + `},` +
				`{"operation":"copy","source":` + string(jsonString(d)) +
				`,"destination":` + string(jsonString(filepath.Join(d, "sub", "inner"))) + `}]`
			return []string{"batch-file-operations", "--operations", ops}
		}},
		{"sync-directories into a subdirectory", func(d string) []string {
			return []string{"sync-directories", "--source", d, "--dest", filepath.Join(d, "inner")}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := filepath.Join(t.TempDir(), "d")
			mustWrite(t, filepath.Join(d, "f.txt"), "x")
			before := countEntries(t, d)

			_, _, code := runCLI(t, tc.args(d)...)
			if code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			if after := countEntries(t, d); after != before {
				t.Errorf("%d entries under the source, was %d: the refusal wrote first", after, before)
			}
		})
	}

	// The check is on path components, not text: d-copy starts with "d" and
	// is a sibling, not a child.
	t.Run("a sibling sharing the prefix still copies", func(t *testing.T) {
		d := filepath.Join(t.TempDir(), "d")
		mustWrite(t, filepath.Join(d, "f.txt"), "x")
		if _, _, code := runCLI(t, "copy-file", "--source", d, "--dest", d+"-copy"); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
		if _, err := os.Stat(filepath.Join(d+"-copy", "f.txt")); err != nil {
			t.Error(err)
		}
	})
}

// delete-file --path ~ --recursive --confirm removed all of HOME, and --path .
// removed the working directory (EST-8264 item 2). The root, HOME, the cwd and
// any directory holding one of them are refused with exit 2, as setup refuses
// HOME. Every case runs against temporary directories, and the root case
// omits --recursive, so a regression cannot remove anything real.
func TestDeleteRefusesHomeCwdAndRoot(t *testing.T) {
	setup := func(t *testing.T) (base, home, cwd string) {
		base = t.TempDir()
		home = filepath.Join(base, "home")
		cwd = filepath.Join(base, "work", "cwd")
		mustWrite(t, filepath.Join(home, "keep.txt"), "x")
		mustWrite(t, filepath.Join(cwd, "keep.txt"), "x")
		t.Setenv("HOME", home)
		t.Chdir(cwd)
		return
	}

	for _, tc := range []struct {
		name string
		path func(base, home, cwd string) string
	}{
		{"tilde", func(_, _, _ string) string { return "~" }},
		{"HOME by path", func(_, home, _ string) string { return home }},
		{"dot", func(_, _, _ string) string { return "." }},
		{"cwd by path", func(_, _, cwd string) string { return cwd }},
		{"an ancestor of the cwd", func(base, _, _ string) string { return filepath.Join(base, "work") }},
		{"an ancestor of both", func(base, _, _ string) string { return base }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, home, cwd := setup(t)
			_, _, code := runCLI(t, "delete-file", "--path", tc.path(base, home, cwd), "--recursive", "--confirm")
			if code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
			for _, keep := range []string{filepath.Join(home, "keep.txt"), filepath.Join(cwd, "keep.txt")} {
				if _, err := os.Stat(keep); err != nil {
					t.Errorf("%s is gone: %v", keep, err)
				}
			}
		})
	}

	t.Run("the filesystem root", func(t *testing.T) {
		setup(t)
		if _, _, code := runCLI(t, "delete-file", "--path", "/", "--confirm"); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
	})

	t.Run("batch delete of HOME", func(t *testing.T) {
		_, home, _ := setup(t)
		ops := `[{"operation":"delete","source":` + string(jsonString(home)) + `,"recursive":true}]`
		if _, _, code := runCLI(t, "batch-file-operations", "--operations", ops, "--confirm"); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		if _, err := os.Stat(filepath.Join(home, "keep.txt")); err != nil {
			t.Errorf("HOME was emptied: %v", err)
		}
	})

	// Known-good: a directory inside HOME or the cwd is an ordinary delete.
	t.Run("a subdirectory of HOME still deletes", func(t *testing.T) {
		_, home, _ := setup(t)
		mustWrite(t, filepath.Join(home, "sub", "f.txt"), "x")
		if _, _, code := runCLI(t, "delete-file", "--path", filepath.Join(home, "sub"), "--recursive", "--confirm"); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
		mustNotExist(t, filepath.Join(home, "sub"))
	})
}
