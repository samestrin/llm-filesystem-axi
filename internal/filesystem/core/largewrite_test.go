package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// LargeWriteFile wrote through os.Create(path + ".tmp") and renamed it over the
// target, so every overwrite reset the file to 0644 &^ umask: a 0600 secret
// became world-readable and a 0755 script lost its execute bits (EST-8806).
func TestLargeWriteFileKeepsTheTargetsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not keep Unix permission bits")
	}
	for _, mode := range []fs.FileMode{0o600, 0o755} {
		t.Run(mode.String(), func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "f")
			if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, mode); err != nil {
				t.Fatal(err)
			}
			if _, err := LargeWriteFile(LargeWriteFileOptions{Path: p, Content: "new", VerifyWrite: true}); err != nil {
				t.Fatalf("LargeWriteFile() error = %v", err)
			}
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != mode {
				t.Errorf("mode after overwrite = %v, want %v", got, mode)
			}
			assertContent(t, p, "new")
		})
	}
}

// --append replaced the file with the new content and reported success: the
// temp file held only opts.Content, and the rename discarded the old bytes.
func TestLargeWriteFileAppendKeepsTheExistingBytes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(p, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := LargeWriteFile(LargeWriteFileOptions{Path: p, Content: "second\n", Append: true, VerifyWrite: true})
	if err != nil {
		t.Fatalf("LargeWriteFile() error = %v", err)
	}
	assertContent(t, p, "first\nsecond\n")
	if result.Created {
		t.Error("an append to an existing file reported created")
	}
}

// Every call used the one temp name path+".tmp", so two writers to one path
// truncated and renamed each other's temp file, and one failed or left a mix.
func TestLargeWriteFileConcurrentWritesDoNotShareATempName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	contents := []string{strings.Repeat("a", 1<<20), strings.Repeat("b", 1<<20)}
	for round := 0; round < 20; round++ {
		var wg sync.WaitGroup
		errs := make([]error, len(contents))
		for i, c := range contents {
			wg.Add(1)
			go func(i int, c string) {
				defer wg.Done()
				_, errs[i] = LargeWriteFile(LargeWriteFileOptions{Path: p, Content: c})
			}(i, c)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d: writer %d failed: %v", round, i, err)
			}
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != contents[0] && string(got) != contents[1] {
			t.Fatalf("round %d: file holds neither write whole (%d bytes)", round, len(got))
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("temp files left behind: %v", names)
	}
}

// With A allowed and A/hl a hard link to a file outside A, the allowed-dirs
// check passes A/hl, and writing it changes the outside file. write-file has
// refused this since EST-8338; large-write-file did not.
func TestLargeWriteFileRefusesAHardLinkUnderAllowedDirs(t *testing.T) {
	allowed := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	hl := filepath.Join(allowed, "hl")
	if err := os.Link(outside, hl); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	for _, appendMode := range []bool{false, true} {
		_, err := LargeWriteFile(LargeWriteFileOptions{Path: hl, Content: "changed", Append: appendMode, Backup: true, AllowedDirs: []string{allowed}})
		var ue *UsageError
		if !errors.As(err, &ue) {
			t.Errorf("append=%v: error = %v, want a usage error", appendMode, err)
		}
		assertContent(t, outside, "outside")
		assertContent(t, hl, "outside")
	}
	if entries, _ := os.ReadDir(allowed); len(entries) != 1 {
		t.Errorf("a refused write left files behind: %d entries in the allowed dir", len(entries))
	}
}

// Without --allowed-dirs a hard-linked file is written in place, so every name
// sees the new bytes, and the result says the write was not atomic.
func TestLargeWriteFileWritesAHardLinkInPlaceAndSaysSo(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	result, err := LargeWriteFile(LargeWriteFileOptions{Path: a, Content: "new", VerifyWrite: true})
	if err != nil {
		t.Fatalf("LargeWriteFile() error = %v", err)
	}
	assertContent(t, a, "new")
	assertContent(t, b, "new")
	if !result.InPlace || !strings.Contains(result.Message, "not atomically") {
		t.Errorf("in-place write not reported: in_place=%v message=%q", result.InPlace, result.Message)
	}
}

// createBackupFile returned "" on failure and LargeWriteFile wrote anyway, so a
// caller who asked for a backup got the write and no safety copy. It also made
// every backup 0644, so a 0600 file's backup was world-readable.
func TestLargeWriteFileBackup(t *testing.T) {
	t.Run("keeps the original's mode", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not keep Unix permission bits")
		}
		p := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := LargeWriteFile(LargeWriteFileOptions{Path: p, Content: "new", Backup: true})
		if err != nil {
			t.Fatalf("LargeWriteFile() error = %v", err)
		}
		if result.Backup == "" {
			t.Fatal("no backup reported")
		}
		info, err := os.Stat(result.Backup)
		if err != nil {
			t.Fatalf("reported backup does not exist: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("backup mode = %v, want 0600", info.Mode().Perm())
		}
		assertContent(t, result.Backup, "old")
		assertContent(t, p, "new")
	})

	t.Run("a failed backup refuses the write", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("directory permissions do not make a backup unwritable here")
		}
		dir := t.TempDir()
		p := filepath.Join(dir, "f")
		if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		result, err := LargeWriteFile(LargeWriteFileOptions{Path: p, Content: "new", Backup: true})
		if err == nil {
			t.Fatalf("LargeWriteFile() succeeded without its backup: %+v", result)
		}
		if !strings.Contains(err.Error(), "failed to create backup") {
			t.Errorf("error = %v, want it to name the backup", err)
		}
		assertContent(t, p, "old")
	})
}

// A path that names a directory exists, so it took the existing-file branch:
// --append's read and the rename over it must fail, and nothing be written.
func TestLargeWriteFileReportsFailuresOnADirectory(t *testing.T) {
	for _, appendMode := range []bool{false, true} {
		dir := t.TempDir()
		target := filepath.Join(dir, "d")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := LargeWriteFile(LargeWriteFileOptions{Path: target, Content: "x", Append: appendMode})
		want := "failed to write file"
		if appendMode {
			want = "failed to read file to append to"
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("append=%v: error = %v, want %q", appendMode, err, want)
		}
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			t.Errorf("append=%v: the directory was replaced", appendMode)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 1 {
			t.Errorf("append=%v: a failed write left %d entries behind", appendMode, len(entries))
		}
	}
}

// --verify used to read the temp file before the rename. It now reads the
// target after it, so a mismatch cannot be undone: it must fail, not report
// success, and name the backup that still holds the old bytes.
func TestLargeWriteFileVerifyMismatchFailsAndNamesTheBackup(t *testing.T) {
	saved := readBackWritten
	t.Cleanup(func() { readBackWritten = saved })
	readBackWritten = func(string) ([]byte, error) { return []byte("corrupt"), nil }

	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := LargeWriteFile(LargeWriteFileOptions{Path: p, Content: "new", Backup: true, VerifyWrite: true})
	if err == nil {
		t.Fatalf("a failed verification reported success: %+v", result)
	}
	if !strings.Contains(err.Error(), "write verification failed") || !strings.Contains(err.Error(), ".bak.") {
		t.Errorf("error = %v, want the verification failure and the backup path", err)
	}
	// The rename already happened, so the file holds the new bytes.
	assertContent(t, p, "new")
}

func assertContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}
