package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// search-and-replace skipped a matched file it would not or could not write
// and left no trace of it: the result counted only the files it changed, so
// "1 file modified" read as "every match was replaced" (epic 16 T1). Every
// such file is now listed under skipped with the reason.

// skipFixture lays out allowed/{a,b}.txt both holding "foo\n", with b also
// linked from outside allowed. It returns allowed, a and b.
func skipFixture(t *testing.T) (allowed, a, b string) {
	t.Helper()
	root := t.TempDir()
	allowed = filepath.Join(root, "allowed")
	if err := os.Mkdir(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	a, b = filepath.Join(allowed, "a.txt"), filepath.Join(allowed, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("foo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(b, filepath.Join(root, "outside.txt")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	return allowed, a, b
}

func TestSearchAndReplaceListsAHardLinkedFileUnderSkipped(t *testing.T) {
	allowed, a, b := skipFixture(t)

	res, err := SearchAndReplace(SearchReplaceOptions{
		Path: allowed, Pattern: "foo", Replacement: "bar", AllowedDirs: []string{allowed},
	})
	if err != nil {
		t.Fatalf("SearchAndReplace: %v", err)
	}

	if got, _ := os.ReadFile(a); string(got) != "bar\n" {
		t.Errorf("a.txt = %q, want the replacement", got)
	}
	if got, _ := os.ReadFile(b); string(got) != "foo\n" {
		t.Errorf("the linked b.txt = %q, want it byte-identical", got)
	}
	if res.FilesModified != 1 || res.TotalChanges != 1 {
		t.Errorf("files_modified=%d total_changes=%d, want 1 and 1", res.FilesModified, res.TotalChanges)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != b || !strings.Contains(res.Skipped[0].Reason, "hard links") {
		t.Errorf("skipped = %+v, want b.txt with the hard-link reason", res.Skipped)
	}
}

// A dry run previews the same skip, or it promises a write the real run
// will not make.
func TestSearchAndReplaceDryRunListsTheSameSkip(t *testing.T) {
	allowed, _, b := skipFixture(t)

	res, err := SearchAndReplace(SearchReplaceOptions{
		Path: allowed, Pattern: "foo", Replacement: "bar", DryRun: true, AllowedDirs: []string{allowed},
	})
	if err != nil {
		t.Fatalf("SearchAndReplace: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != b {
		t.Errorf("skipped = %+v, want b.txt", res.Skipped)
	}
	if res.FilesModified != 1 {
		t.Errorf("files_modified = %d, want 1", res.FilesModified)
	}
}

// Known-good: a hard-linked file the pattern does not match was never going
// to be written, so it is not skipped. The link check used to run before the
// file was read.
func TestSearchAndReplaceDoesNotListAnUnmatchedLinkedFile(t *testing.T) {
	allowed, a, b := skipFixture(t)
	if err := os.WriteFile(b, []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := SearchAndReplace(SearchReplaceOptions{
		Path: allowed, Pattern: "foo", Replacement: "bar", AllowedDirs: []string{allowed},
	})
	if err != nil {
		t.Fatalf("SearchAndReplace: %v", err)
	}
	if res.Skipped != nil {
		t.Errorf("skipped = %+v, want none: b.txt does not match", res.Skipped)
	}
	if got, _ := os.ReadFile(a); string(got) != "bar\n" {
		t.Errorf("a.txt = %q, want the replacement", got)
	}
}

// A file the write fails on is skipped with the error, and its matches are
// not counted in total_changes: they were never made.
func TestSearchAndReplaceListsAnUnwritableFileUnderSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes any file")
	}
	root := t.TempDir()
	ro := filepath.Join(root, "ro")
	if err := os.Mkdir(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	a, b := filepath.Join(root, "a.txt"), filepath.Join(ro, "b.txt")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("foo foo\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Neither a temp file beside it nor the in-place fallback can write b.
	if err := os.Chmod(b, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })

	res, err := SearchAndReplace(SearchReplaceOptions{Path: root, Pattern: "foo", Replacement: "bar"})
	if err != nil {
		t.Fatalf("SearchAndReplace: %v", err)
	}
	if got, _ := os.ReadFile(b); string(got) != "foo foo\n" {
		t.Errorf("b.txt = %q, want it unchanged", got)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != b || !strings.Contains(res.Skipped[0].Reason, "permission denied") {
		t.Errorf("skipped = %+v, want b.txt with permission denied", res.Skipped)
	}
	if res.FilesModified != 1 || res.TotalChanges != 2 {
		t.Errorf("files_modified=%d total_changes=%d, want 1 and 2 (a.txt only)", res.FilesModified, res.TotalChanges)
	}
}
