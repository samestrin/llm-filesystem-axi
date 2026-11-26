package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every edit used to write with os.WriteFile(path, ..., 0644). On an existing
// file WriteFile keeps the mode it has, but it truncates first and writes
// second, so a crash or a full disk mid-write left a truncated file, and the
// backup was always created 0644 — a 0600 secret's backup was world-readable.
// Edits now go through one helper that writes a temp file beside the target,
// fsyncs it, sets the original mode and renames it into place.

type editRun struct {
	name string
	run  func(path, dir string) error
}

// allowedDirs is the --allowed-dirs list for d, and none when d is empty.
func allowedDirs(d string) []string {
	if d == "" {
		return nil
	}
	return []string{d}
}

// editRuns makes one real change to a file holding "alpha\nbeta\n" through
// every edit command that rewrites a file.
func editRuns() []editRun {
	return []editRun{
		{"edit-block", func(p, d string) error {
			_, err := EditBlock(EditBlockOptions{Path: p, OldString: "alpha", NewString: "ALPHA", AllowedDirs: allowedDirs(d)})
			return err
		}},
		{"edit-blocks", func(p, d string) error {
			_, err := EditBlocks(EditBlocksOptions{Path: p, Edits: []EditPair{{OldString: "alpha", NewString: "ALPHA"}}, AllowedDirs: allowedDirs(d)})
			return err
		}},
		{"safe-edit", func(p, d string) error {
			_, err := SafeEdit(SafeEditOptions{Path: p, OldString: "alpha", NewString: "ALPHA", Backup: true, AllowedDirs: allowedDirs(d)})
			return err
		}},
		{"edit-file", func(p, d string) error {
			_, err := EditFile(EditFileOptions{Path: p, Operation: "replace", Line: 1, Content: "ALPHA", AllowedDirs: allowedDirs(d)})
			return err
		}},
		{"edit-multiple-blocks", func(p, d string) error {
			_, err := EditMultipleBlocks(EditMultipleBlocksOptions{Path: p, Edits: []MultiEditOperation{{OldText: "alpha", NewText: "ALPHA"}}, Backup: true, AllowedDirs: allowedDirs(d)})
			return err
		}},
		{"search-and-replace", func(p, d string) error {
			_, err := SearchAndReplace(SearchReplaceOptions{Path: p, Pattern: "alpha", Replacement: "ALPHA", AllowedDirs: allowedDirs(d)})
			return err
		}},
	}
}

func TestEditsKeepTheFileMode(t *testing.T) {
	for _, mode := range []fs.FileMode{0o755, 0o600} {
		for _, e := range editRuns() {
			t.Run(e.name+"/"+mode.String(), func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "f")
				if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				// Chmod rather than WriteFile's perm, which umask can narrow.
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}

				if err := e.run(path, dir); err != nil {
					t.Fatalf("edit: %v", err)
				}

				got, _ := os.ReadFile(path)
				if !strings.HasPrefix(string(got), "ALPHA") {
					t.Fatalf("the edit did not apply: %q", got)
				}
				entries, _ := os.ReadDir(dir)
				for _, entry := range entries {
					info, err := entry.Info()
					if err != nil {
						t.Fatal(err)
					}
					// The edited file and any backup beside it both keep the
					// original mode; a leftover temp file fails the name check.
					if !strings.HasPrefix(entry.Name(), "f") {
						t.Errorf("left %s behind", entry.Name())
					}
					if info.Mode().Perm() != mode {
						t.Errorf("%s mode = %v, want %v", entry.Name(), info.Mode().Perm(), mode)
					}
				}
			})
		}
	}
}

// The helper renames into place, so a write that fails part-way must leave the
// original bytes exactly as they were and no temp file behind.
func TestEditWriteFailureLeavesTheOriginalIntact(t *testing.T) {
	for _, e := range editRuns() {
		t.Run(e.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f")
			if err := os.WriteFile(path, []byte("alpha\nbeta\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			prev := syncTempFile
			syncTempFile = func(f *os.File) error { return errors.New("injected: disk full") }
			t.Cleanup(func() { syncTempFile = prev })

			err := e.run(path, dir)
			// search-and-replace reports per file rather than failing the run;
			// what matters for every command is that the file is intact.
			if err == nil && e.name != "search-and-replace" {
				t.Error("a failed write reported success")
			}
			if got, _ := os.ReadFile(path); string(got) != "alpha\nbeta\n" {
				t.Errorf("a failed write changed the original: %q", got)
			}
			entries, _ := os.ReadDir(dir)
			for _, entry := range entries {
				if entry.Name() != "f" && !strings.HasPrefix(entry.Name(), "f.bak.") {
					t.Errorf("a failed write left %s behind", entry.Name())
				}
			}
		})
	}
}

// Renaming over a symlink would replace the link with a regular file and leave
// the file it points to unedited. The write lands on the link's target.
func TestEditThroughASymlinkEditsTheTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := EditBlock(EditBlockOptions{Path: link, OldString: "alpha", NewString: "ALPHA", AllowedDirs: []string{dir}}); err != nil {
		t.Fatalf("EditBlock: %v", err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced by a regular file")
	}
	if got, _ := os.ReadFile(target); string(got) != "ALPHA\n" {
		t.Errorf("target = %q, want the edit", got)
	}
}

// filepath.Walk hands search-and-replace the symlink's own Lstat info, whose
// mode is 0777. Passing that to the atomic write chmodded a 0600 target behind
// the link to rwxrwxrwx — the mode-keeping fix itself leaking a secret. The
// mode kept must be the target's.
func TestSearchAndReplaceThroughASymlinkKeepsTheTargetMode(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret")
	if err := os.WriteFile(target, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}

	if _, err := SearchAndReplace(SearchReplaceOptions{Path: dir, Pattern: "alpha", Replacement: "ALPHA"}); err != nil {
		t.Fatalf("SearchAndReplace: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("target mode = %v, want -rw-------", info.Mode().Perm())
	}
	if got, _ := os.ReadFile(target); string(got) != "ALPHA\n" {
		t.Errorf("target = %q, want the edit", got)
	}
}

// Every step of the atomic write can fail, and each failure must leave the
// original bytes in place and no temp file behind. The sync step is covered
// per command above; these cover the rest of the helper's branches.
func TestWriteFileAtomicFailuresLeaveTheOriginal(t *testing.T) {
	injected := errors.New("injected")
	steps := []struct {
		name   string
		inject func() func()
	}{
		{"write", func() func() {
			prev := writeTempFile
			writeTempFile = func(*os.File, []byte) (int, error) { return 0, injected }
			return func() { writeTempFile = prev }
		}},
		{"chmod", func() func() {
			prev := chmodTempFile
			chmodTempFile = func(*os.File, fs.FileMode) error { return injected }
			return func() { chmodTempFile = prev }
		}},
		{"close", func() func() {
			prev := closeTempFile
			closeTempFile = func(*os.File) error { return injected }
			return func() { closeTempFile = prev }
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f")
			if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.inject())

			if err := replaceFile(path, []byte("new")); !errors.Is(err, injected) {
				t.Errorf("err = %v, want the injected %s failure", err, s.name)
			}
			if got, _ := os.ReadFile(path); string(got) != "original" {
				t.Errorf("a failed %s changed the original: %q", s.name, got)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("a failed %s left a temp file: %v", s.name, entries)
			}
		})
	}
}

// The temp file cannot be created when the directory does not exist; the
// error is returned rather than swallowed.
func TestWriteFileAtomicReportsAnUncreatableTempFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "f")
	if err := writeFileAtomic(path, []byte("x"), 0o644); err == nil {
		t.Error("a write into a missing directory reported success")
	}
}

// replaceFile keeps an existing file's mode, so a path that is not there is an
// error, not a new 0000 file.
func TestReplaceFileRefusesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	if err := replaceFile(path, []byte("x")); err == nil {
		t.Error("replaceFile created a file that did not exist")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("replaceFile left %s behind: %v", path, err)
	}
}

// A backup that cannot be taken must return its error and no path to a file
// that does not exist: a missing source fails the stat, a directory the read.
// Returning "" without the error let both edit callers apply the edit anyway.
func TestCreateEditBackupReportsAnErrorWhenItCannotRead(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{filepath.Join(dir, "absent"), dir} {
		backupPath, err := createEditBackup(p)
		if err == nil {
			t.Errorf("createEditBackup(%s) reported success", p)
		}
		if backupPath != "" {
			t.Errorf("createEditBackup(%s) = %q, want no backup path", p, backupPath)
		}
	}
}
