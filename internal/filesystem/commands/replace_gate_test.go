package commands

import (
	"path/filepath"
	"testing"
)

// compress-files truncated --output before checking its inputs: an existing
// keep.zip plus one missing input left a partial archive in keep.zip's place,
// and an existing --output was replaced with no --confirm (EST-8264 item 4).
func TestCompressChecksInputsAndGatesAnExistingOutput(t *testing.T) {
	setup := func(t *testing.T) (ok, out string) {
		d := t.TempDir()
		ok = filepath.Join(d, "ok.txt")
		mustWrite(t, ok, "ok")
		out = filepath.Join(d, "keep.zip")
		mustWrite(t, out, "precious")
		return
	}

	t.Run("a missing input leaves an existing output alone", func(t *testing.T) {
		ok, out := setup(t)
		missing := filepath.Join(filepath.Dir(ok), "missing.txt")
		if _, _, code := runCLI(t, "compress-files", "--paths", ok+","+missing, "--output", out, "--confirm"); code != 1 {
			t.Errorf("exit %d, want 1", code)
		}
		if got := readString(t, out); got != "precious" {
			t.Errorf("keep.zip was rewritten (%d bytes)", len(got))
		}
	})

	t.Run("a missing input writes no new output", func(t *testing.T) {
		ok, _ := setup(t)
		fresh := filepath.Join(filepath.Dir(ok), "fresh.zip")
		runCLI(t, "compress-files", "--paths", ok+","+filepath.Join(filepath.Dir(ok), "missing.txt"), "--output", fresh)
		mustNotExist(t, fresh)
	})

	t.Run("an existing output needs --confirm", func(t *testing.T) {
		ok, out := setup(t)
		if _, _, code := runCLI(t, "compress-files", "--paths", ok, "--output", out); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		if got := readString(t, out); got != "precious" {
			t.Errorf("keep.zip was replaced without --confirm")
		}
	})

	t.Run("--confirm replaces it", func(t *testing.T) {
		ok, out := setup(t)
		stdout, _, code := runCLI(t, "compress-files", "--paths", ok, "--output", out, "--confirm")
		if code != -1 {
			t.Errorf("exit %d, want success\n%s", code, stdout)
		}
		if !hasTOONLine(stdout, "overwritten: true") {
			t.Errorf("overwritten not reported:\n%s", stdout)
		}
	})

	t.Run("a fresh output needs no --confirm", func(t *testing.T) {
		ok, _ := setup(t)
		if _, _, code := runCLI(t, "compress-files", "--paths", ok, "--output", filepath.Join(filepath.Dir(ok), "new.zip")); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
	})
}

// sync-directories replaced existing destination files, a newer one with
// older content included, without --confirm, and was not listed among the
// principle 6 exceptions (EST-8264 item 6).
func TestSyncReplacingFilesNeedsConfirm(t *testing.T) {
	setup := func(t *testing.T) (src, dst string) {
		d := t.TempDir()
		src, dst = filepath.Join(d, "src"), filepath.Join(d, "dst")
		mustWrite(t, filepath.Join(src, "f.txt"), "old")
		mustWrite(t, filepath.Join(dst, "f.txt"), "newer")
		return
	}

	t.Run("refused without --confirm", func(t *testing.T) {
		src, dst := setup(t)
		if _, _, code := runCLI(t, "sync-directories", "--source", src, "--dest", dst); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
		if got := readString(t, filepath.Join(dst, "f.txt")); got != "newer" {
			t.Errorf("destination file is %q", got)
		}
	})

	t.Run("--confirm replaces", func(t *testing.T) {
		src, dst := setup(t)
		if _, _, code := runCLI(t, "sync-directories", "--source", src, "--dest", dst, "--confirm"); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
		if got := readString(t, filepath.Join(dst, "f.txt")); got != "old" {
			t.Errorf("destination file is %q", got)
		}
	})

	t.Run("a dry run needs no --confirm", func(t *testing.T) {
		src, dst := setup(t)
		if _, _, code := runCLI(t, "sync-directories", "--source", src, "--dest", dst, "--dry-run"); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
		if got := readString(t, filepath.Join(dst, "f.txt")); got != "newer" {
			t.Errorf("a dry run wrote: %q", got)
		}
	})

	t.Run("a sync that replaces nothing needs no --confirm", func(t *testing.T) {
		src, _ := setup(t)
		fresh := filepath.Join(filepath.Dir(src), "fresh")
		if _, _, code := runCLI(t, "sync-directories", "--source", src, "--dest", fresh); code != -1 {
			t.Errorf("exit %d, want success", code)
		}
		if got := readString(t, filepath.Join(fresh, "f.txt")); got != "old" {
			t.Errorf("fresh copy is %q", got)
		}
	})
}
