package commands

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// symlinkJail builds an --allowed-dirs root A and a directory outside it, with
// A/link pointing at the outside directory and A/filelink at a file there. The
// allowed-dirs check compared text prefixes, so every path through A/link
// passed it and the operation landed outside A with exit 0 (EST-8264 item 1).
func symlinkJail(t *testing.T) (allowed, outside string) {
	t.Helper()
	allowed, outside = t.TempDir(), t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.txt"), "outside data")
	mustWrite(t, filepath.Join(allowed, "src.txt"), "inside data")
	if err := os.Symlink(outside, filepath.Join(allowed, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(allowed, "filelink")); err != nil {
		t.Fatal(err)
	}
	return allowed, outside
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("%s was created", path)
	}
}

// Every write, copy, delete, archive and sync command must refuse a path that
// resolves outside --allowed-dirs, whether the link is in the destination, the
// source, or the final component. The assertion is on the outside directory,
// not the exit code (runCLI reports -1 when nothing called exit, i.e. 0): the bug exited 0, and a refusal that ran first would
// still pass an exit-code-only check.
func TestAllowedDirsResolvesSymlinks(t *testing.T) {
	zipWith := func(t *testing.T, path string) {
		t.Helper()
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		w, _ := zw.Create("entry.txt")
		w.Write([]byte("x"))
		zw.Close()
		f.Close()
	}

	t.Run("write-file through a linked directory", func(t *testing.T) {
		a, out := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "write-file", "--path", filepath.Join(a, "link", "new.txt"), "--content", "x")
		mustNotExist(t, filepath.Join(out, "new.txt"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("write-file through a dangling link", func(t *testing.T) {
		a, out := symlinkJail(t)
		if err := os.Symlink(filepath.Join(out, "made.txt"), filepath.Join(a, "dangle")); err != nil {
			t.Fatal(err)
		}
		_, _, code := runCLI(t, "--allowed-dirs", a, "write-file", "--path", filepath.Join(a, "dangle"), "--content", "x")
		mustNotExist(t, filepath.Join(out, "made.txt"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("write-file over a link to an outside file", func(t *testing.T) {
		a, out := symlinkJail(t)
		runCLI(t, "--allowed-dirs", a, "write-file", "--path", filepath.Join(a, "filelink"), "--content", "clobbered")
		if b, _ := os.ReadFile(filepath.Join(out, "secret.txt")); string(b) != "outside data" {
			t.Errorf("outside file now holds %q", b)
		}
	})

	t.Run("large-write-file", func(t *testing.T) {
		a, out := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "large-write-file", "--path", filepath.Join(a, "link", "big.txt"), "--content", "x")
		mustNotExist(t, filepath.Join(out, "big.txt"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("write-multiple-files", func(t *testing.T) {
		a, out := symlinkJail(t)
		files := `[{"path":` + string(jsonString(filepath.Join(a, "link", "m.txt"))) + `,"content":"x"}]`
		_, _, code := runCLI(t, "--allowed-dirs", a, "write-multiple-files", "--files", files)
		mustNotExist(t, filepath.Join(out, "m.txt"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("copy-file destination", func(t *testing.T) {
		a, out := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "copy-file", "--source", filepath.Join(a, "src.txt"), "--dest", filepath.Join(a, "link", "c.txt"))
		mustNotExist(t, filepath.Join(out, "c.txt"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("copy-file linked source", func(t *testing.T) {
		a, _ := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "copy-file", "--source", filepath.Join(a, "filelink"), "--dest", filepath.Join(a, "in.txt"))
		mustNotExist(t, filepath.Join(a, "in.txt"))
		if code == -1 {
			t.Error("exit 0 for a source outside --allowed-dirs")
		}
	})

	// A link nested inside a copied tree reads outside data in just the same.
	t.Run("copy-file tree holding a link", func(t *testing.T) {
		a, out := symlinkJail(t)
		mustWrite(t, filepath.Join(a, "tree", "ok.txt"), "ok")
		if err := os.Symlink(filepath.Join(out, "secret.txt"), filepath.Join(a, "tree", "leak")); err != nil {
			t.Fatal(err)
		}
		_, _, code := runCLI(t, "--allowed-dirs", a, "copy-file", "--source", filepath.Join(a, "tree"), "--dest", filepath.Join(a, "tree2"))
		mustNotExist(t, filepath.Join(a, "tree2", "leak"))
		if code == -1 {
			t.Error("exit 0 copying a tree that links outside --allowed-dirs")
		}
	})

	t.Run("move-file destination", func(t *testing.T) {
		a, out := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "move-file", "--source", filepath.Join(a, "src.txt"), "--dest", filepath.Join(a, "link", "mv.txt"))
		mustNotExist(t, filepath.Join(out, "mv.txt"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("create-directory", func(t *testing.T) {
		a, out := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "create-directory", "--path", filepath.Join(a, "link", "newdir"))
		mustNotExist(t, filepath.Join(out, "newdir"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("delete-file through a linked directory", func(t *testing.T) {
		a, out := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "delete-file", "--path", filepath.Join(a, "link", "secret.txt"), "--confirm")
		if _, err := os.Stat(filepath.Join(out, "secret.txt")); err != nil {
			t.Errorf("outside file was deleted: %v", err)
		}
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("batch-file-operations delete", func(t *testing.T) {
		a, out := symlinkJail(t)
		ops := `[{"operation":"delete","source":` + string(jsonString(filepath.Join(a, "link", "secret.txt"))) + `}]`
		runCLI(t, "--allowed-dirs", a, "batch-file-operations", "--operations", ops, "--confirm")
		if _, err := os.Stat(filepath.Join(out, "secret.txt")); err != nil {
			t.Errorf("outside file was deleted: %v", err)
		}
	})

	t.Run("compress-files output", func(t *testing.T) {
		a, out := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "compress-files", "--paths", filepath.Join(a, "src.txt"), "--output", filepath.Join(a, "link", "o.zip"))
		mustNotExist(t, filepath.Join(out, "o.zip"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("compress-files linked input", func(t *testing.T) {
		a, _ := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "compress-files", "--paths", filepath.Join(a, "filelink"), "--output", filepath.Join(a, "o.zip"))
		mustNotExist(t, filepath.Join(a, "o.zip"))
		if code == -1 {
			t.Error("exit 0 for an input outside --allowed-dirs")
		}
	})

	t.Run("extract-archive destination", func(t *testing.T) {
		a, out := symlinkJail(t)
		zipWith(t, filepath.Join(a, "a.zip"))
		_, _, code := runCLI(t, "--allowed-dirs", a, "extract-archive", "--archive", filepath.Join(a, "a.zip"), "--dest", filepath.Join(a, "link", "x"))
		mustNotExist(t, filepath.Join(out, "x"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("sync-directories destination", func(t *testing.T) {
		a, out := symlinkJail(t)
		mustWrite(t, filepath.Join(a, "sd", "f.txt"), "x")
		_, _, code := runCLI(t, "--allowed-dirs", a, "sync-directories", "--source", filepath.Join(a, "sd"), "--dest", filepath.Join(a, "link", "s"))
		mustNotExist(t, filepath.Join(out, "s"))
		if code == -1 {
			t.Error("exit 0 for a path outside --allowed-dirs")
		}
	})

	t.Run("sync-directories linked source", func(t *testing.T) {
		a, _ := symlinkJail(t)
		_, _, code := runCLI(t, "--allowed-dirs", a, "sync-directories", "--source", filepath.Join(a, "link"), "--dest", filepath.Join(a, "s2"))
		mustNotExist(t, filepath.Join(a, "s2"))
		if code == -1 {
			t.Error("exit 0 for a source outside --allowed-dirs")
		}
	})
}

// The known-good side: resolving links must not refuse what is genuinely
// inside. An allowed dir named through a link, and a link that stays inside
// the jail, both still work, and removing a link that points outside removes
// only the link, so it is allowed.
func TestAllowedDirsSymlinksInsideStillWork(t *testing.T) {
	a, out := symlinkJail(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(a, alias); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runCLI(t, "--allowed-dirs", alias, "write-file", "--path", filepath.Join(a, "via-alias.txt"), "--content", "x"); code != -1 {
		t.Errorf("write inside an allowed dir named through a link: exit %d", code)
	}
	if err := os.Symlink(filepath.Join(a, "src.txt"), filepath.Join(a, "inner")); err != nil {
		t.Fatal(err)
	}
	if _, _, code := runCLI(t, "--allowed-dirs", a, "copy-file", "--source", filepath.Join(a, "inner"), "--dest", filepath.Join(a, "copied.txt")); code != -1 {
		t.Errorf("copy of a link that stays inside: exit %d", code)
	}
	if _, _, code := runCLI(t, "--allowed-dirs", a, "write-file", "--path", filepath.Join(a, "new", "deep.txt"), "--content", "x", "--create-dirs"); code != -1 {
		t.Errorf("write to a path whose parents do not exist yet: exit %d", code)
	}
	if _, _, code := runCLI(t, "--allowed-dirs", a, "delete-file", "--path", filepath.Join(a, "filelink"), "--confirm"); code != -1 {
		t.Errorf("deleting the link itself: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(out, "secret.txt")); err != nil {
		t.Errorf("deleting the link removed its target: %v", err)
	}
}
