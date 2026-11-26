package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An edit renamed a temp file over the target. For a file with a second name
// that split the link: the edited name got a new inode and the other name kept
// the old bytes, with exit 0. A file with more than one link is now written in
// place after the temp copy is fsynced, and the result says so (EST-8805).

// linkedPair writes "alpha\nbeta\n" to dir/a and links dir/b to it.
func linkedPair(t *testing.T) (dir, a, b string) {
	t.Helper()
	dir = t.TempDir()
	a, b = filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	return dir, a, b
}

// bothNamesHold fails unless a and b are still one file holding want.
func bothNamesHold(t *testing.T, a, b, want string) {
	t.Helper()
	for _, p := range []string{a, b} {
		if got, _ := os.ReadFile(p); string(got) != want {
			t.Errorf("%s = %q, want %q", filepath.Base(p), got, want)
		}
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA != nil || errB != nil || !os.SameFile(ia, ib) {
		t.Errorf("a and b are no longer the same file")
	}
}

func TestSafeEditThroughAHardLinkWritesInPlaceAndSaysSo(t *testing.T) {
	_, a, b := linkedPair(t)
	res, err := SafeEdit(SafeEditOptions{Path: a, OldString: "alpha", NewString: "ALPHA"})
	if err != nil {
		t.Fatalf("SafeEdit: %v", err)
	}
	bothNamesHold(t, a, b, "ALPHA\nbeta\n")
	if !res.InPlace {
		t.Error("result does not say the write was in place")
	}
	if !strings.Contains(res.Message, "in place") {
		t.Errorf("message %q does not say the write was in place", res.Message)
	}
}

func TestEditMultipleBlocksThroughAHardLinkWritesInPlaceAndSaysSo(t *testing.T) {
	_, a, b := linkedPair(t)
	res, err := EditMultipleBlocks(EditMultipleBlocksOptions{Path: b, Edits: []MultiEditOperation{{OldText: "beta", NewText: "BETA"}}})
	if err != nil {
		t.Fatalf("EditMultipleBlocks: %v", err)
	}
	bothNamesHold(t, a, b, "alpha\nBETA\n")
	if !res.InPlace {
		t.Error("result does not say the write was in place")
	}
	if !strings.Contains(res.Message, "in place") {
		t.Errorf("message %q does not say the write was in place", res.Message)
	}
}

// Every other edit command shares the helper, so none of them splits a link.
func TestEveryEditKeepsAHardLink(t *testing.T) {
	for _, e := range editRuns() {
		t.Run(e.name, func(t *testing.T) {
			_, a, b := linkedPair(t)
			// An empty dir means no --allowed-dirs, which would refuse a
			// linked file; this case is the one without it.
			if err := e.run(a, ""); err != nil {
				t.Fatalf("edit: %v", err)
			}
			got, _ := os.ReadFile(b)
			if !strings.HasPrefix(string(got), "ALPHA") {
				t.Errorf("the other name holds %q, want the edit", got)
			}
			bothNamesHold(t, a, b, string(got))
		})
	}
}

// Known-good: an ordinary single-link edit still renames into place and says
// nothing about an in-place write.
func TestAnOrdinaryEditIsNotReportedInPlace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	res, err := SafeEdit(SafeEditOptions{Path: p, OldString: "alpha", NewString: "ALPHA"})
	if err != nil {
		t.Fatalf("SafeEdit: %v", err)
	}
	if res.InPlace || strings.Contains(res.Message, "in place") {
		t.Errorf("a renamed write reported in place: %+v", res)
	}
	after, _ := os.Stat(p)
	if os.SameFile(before, after) {
		t.Error("the write did not go through the temp-and-rename path")
	}
}

// With --allowed-dirs the other names may be outside the allowed directories,
// and an in-place write would change them there. The edit is refused with the
// same usage error as write-file, and neither name changes.
func TestEditThroughAHardLinkUnderAllowedDirsIsRefused(t *testing.T) {
	for _, e := range editRuns() {
		t.Run(e.name, func(t *testing.T) {
			dir, a, b := linkedPair(t)
			err := e.run(a, dir)
			// search-and-replace skips a file it cannot write rather than
			// failing the run; the bytes are what matter for every command.
			if e.name != "search-and-replace" {
				var ue *UsageError
				if !errors.As(err, &ue) || !strings.Contains(err.Error(), "hard links") {
					t.Errorf("err = %v, want the hard-link usage error", err)
				}
			}
			bothNamesHold(t, a, b, "alpha\nbeta\n")
			entries, _ := os.ReadDir(dir)
			if len(entries) != 2 {
				t.Errorf("a refused edit left files behind: %v", entries)
			}
		})
	}
}

// The refusal is the one write-file gives, word for word apart from the path.
func TestEditHardLinkRefusalMatchesWriteFile(t *testing.T) {
	dir, a, _ := linkedPair(t)
	_, editErr := SafeEdit(SafeEditOptions{Path: a, OldString: "alpha", NewString: "ALPHA", AllowedDirs: []string{dir}})
	_, writeErr := WriteFile(WriteFileOptions{Path: a, Content: "x", AllowedDirs: []string{dir}})
	if editErr == nil || writeErr == nil || editErr.Error() != writeErr.Error() {
		t.Errorf("safe-edit: %v\nwrite-file: %v", editErr, writeErr)
	}
}

// A preview of an edit the tool will refuse is refused too.
func TestSafeEditDryRunThroughAHardLinkUnderAllowedDirsIsRefused(t *testing.T) {
	dir, a, _ := linkedPair(t)
	if _, err := SafeEdit(SafeEditOptions{Path: a, OldString: "alpha", NewString: "ALPHA", DryRun: true, AllowedDirs: []string{dir}}); err == nil {
		t.Error("a dry run previewed an edit the real run refuses")
	}
}

// A writable file in a directory the caller cannot write: CreateTemp beside it
// failed, so the edit failed although the file itself was writable. It now
// writes in place and says so.
func TestEditInAnUnwritableDirectoryWritesInPlaceAndSaysSo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes any directory")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	res, err := SafeEdit(SafeEditOptions{Path: p, OldString: "alpha", NewString: "ALPHA"})
	if err != nil {
		t.Fatalf("SafeEdit: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "ALPHA\n" {
		t.Errorf("file = %q, want the edit", got)
	}
	if !res.InPlace || !strings.Contains(res.Message, "not writable") {
		t.Errorf("result does not say the write was in place and why: %+v", res)
	}
}

// If the in-place write fails after truncating, the file may be partial. The
// error must say so and name the fsynced temp copy holding the new content,
// which is kept rather than deleted.
func TestAFailedInPlaceWriteKeepsTheTempCopyAndNamesIt(t *testing.T) {
	_, a, _ := linkedPair(t)
	prev := writeInPlace
	writeInPlace = func(string, []byte) (bool, error) { return true, errors.New("injected: disk full") }
	t.Cleanup(func() { writeInPlace = prev })

	_, err := SafeEdit(SafeEditOptions{Path: a, OldString: "alpha", NewString: "ALPHA"})
	if err == nil {
		t.Fatal("a failed in-place write reported success")
	}
	i := strings.LastIndex(err.Error(), " is in ")
	if i < 0 {
		t.Fatalf("error does not name the temp copy: %v", err)
	}
	kept := strings.TrimSpace(err.Error()[i+len(" is in "):])
	t.Cleanup(func() { _ = os.Remove(kept) })
	if got, rerr := os.ReadFile(kept); rerr != nil || string(got) != "ALPHA\nbeta\n" {
		t.Errorf("temp copy %s = %q (%v), want the new content", kept, got, rerr)
	}
}

// If the in-place write cannot even open the file, nothing was truncated: the
// error must not claim the file may be partial, and no temp copy is left.
func TestAnInPlaceWriteThatCannotOpenLeavesNoTempCopy(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a read-only file for writing")
	}
	dir, a, b := linkedPair(t)
	if err := os.Chmod(a, 0o444); err != nil {
		t.Fatal(err)
	}
	_, err := SafeEdit(SafeEditOptions{Path: a, OldString: "alpha", NewString: "ALPHA"})
	if err == nil {
		t.Fatal("an edit of a read-only file reported success")
	}
	if strings.Contains(err.Error(), "partial") {
		t.Errorf("error claims a partial file when nothing was written: %v", err)
	}
	bothNamesHold(t, a, b, "alpha\nbeta\n")
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("a refused write left files behind: %v", entries)
	}
}

// The owner half of the fix cannot be proven unprivileged: only root may chown
// to another uid. What can be proven is that the temp file is chowned to the
// original's uid and gid before the rename, and that a refused chown, which is
// the normal case for an unprivileged edit of someone else's file, does not
// fail the edit.
func TestTheTempFileIsChownedToTheOriginalOwnerBestEffort(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	wantUID, wantGID, ok := fileOwner(info)
	if !ok {
		t.Skip("the platform reports no owner")
	}
	called := false
	prev := chownTempFile
	chownTempFile = func(_ *os.File, uid, gid int) error {
		called = true
		if uid != wantUID || gid != wantGID {
			t.Errorf("chown(%d, %d), want the original's (%d, %d)", uid, gid, wantUID, wantGID)
		}
		return errors.New("injected: operation not permitted")
	}
	t.Cleanup(func() { chownTempFile = prev })

	if _, err := SafeEdit(SafeEditOptions{Path: p, OldString: "alpha", NewString: "ALPHA"}); err != nil {
		t.Fatalf("a refused chown failed the edit: %v", err)
	}
	if !called {
		t.Error("the temp file was never chowned")
	}
	if got, _ := os.ReadFile(p); string(got) != "ALPHA\n" {
		t.Errorf("file = %q, want the edit", got)
	}
}

// The real writeInPlace, not a stub: a write that fails after the O_TRUNC
// open must report truncated, or writeFileAtomic deletes the only fsynced copy
// of the new content and never says the file may be partial (review of PR
// #91). /dev/full opens for writing and fails every write with ENOSPC.
func TestWriteInPlaceReportsTruncatedWhenTheWriteFails(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full unavailable")
	}
	truncated, err := writeInPlace("/dev/full", []byte("new"))
	if err == nil {
		t.Fatal("a write to /dev/full reported success")
	}
	if !truncated {
		t.Error("a write that failed after the truncating open reported the file untouched")
	}
}

// The same for a failed fsync, end to end: the edit errors, says the file may
// be partial, and keeps the temp copy holding the new content.
func TestAFailedInPlaceSyncReportsAPartialFileAndKeepsTheCopy(t *testing.T) {
	_, a, _ := linkedPair(t)
	prev := syncInPlaceFile
	syncInPlaceFile = func(*os.File) error { return errors.New("injected: I/O error") }
	t.Cleanup(func() { syncInPlaceFile = prev })

	_, err := SafeEdit(SafeEditOptions{Path: a, OldString: "alpha", NewString: "ALPHA"})
	if err == nil {
		t.Fatal("a failed in-place sync reported success")
	}
	if !strings.Contains(err.Error(), "may be partial") {
		t.Errorf("error does not say the file may be partial: %v", err)
	}
	i := strings.LastIndex(err.Error(), " is in ")
	if i < 0 {
		t.Fatalf("error does not name the temp copy: %v", err)
	}
	kept := strings.TrimSpace(err.Error()[i+len(" is in "):])
	t.Cleanup(func() { _ = os.Remove(kept) })
	if got, rerr := os.ReadFile(kept); rerr != nil || string(got) != "ALPHA\nbeta\n" {
		t.Errorf("temp copy %s = %q (%v), want the new content", kept, got, rerr)
	}
}
