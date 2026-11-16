package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The four string-matching edit commands each ran strings.Replace(..., 1) after
// strings.Contains, so an old_string that occurred twice silently changed the
// FIRST occurrence and reported success. The caller cannot know which one it
// meant, and neither can the tool: the edit has to be refused, naming the
// count, and --replace-all is the explicit way to mean "every one".

const twoMatches = "x := 1\nfoo()\ny := 2\nfoo()\n"

func writeTwoMatches(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte(twoMatches), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// assertAmbiguous checks the refusal names the match count, and that the file
// was not touched — the side effect, not just the error.
func assertAmbiguous(t *testing.T, path string, err error, wantEdit, wantMatches int) {
	t.Helper()
	var amb *AmbiguousMatchError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want an *AmbiguousMatchError", err)
	}
	if amb.Matches != wantMatches || amb.Edit != wantEdit {
		t.Errorf("AmbiguousMatchError = %+v, want edit %d with %d matches", amb, wantEdit, wantMatches)
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("message %q does not name the match count", err.Error())
	}
	if got, _ := os.ReadFile(path); string(got) != twoMatches {
		t.Errorf("an ambiguous edit changed the file:\n%s", got)
	}
}

func TestEditBlockRefusesAnAmbiguousMatch(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := EditBlock(EditBlockOptions{Path: path, OldString: "foo()", NewString: "bar()", AllowedDirs: []string{dir}})
	if err == nil {
		t.Fatalf("an old_string matching twice reported success: %+v", res)
	}
	assertAmbiguous(t, path, err, 0, 2)
}

func TestEditBlockReplaceAllReplacesEveryMatch(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := EditBlock(EditBlockOptions{Path: path, OldString: "foo()", NewString: "bar()", ReplaceAll: true, AllowedDirs: []string{dir}})
	if err != nil {
		t.Fatalf("EditBlock --replace-all: %v", err)
	}
	if res.Replacements != 2 {
		t.Errorf("replacements = %d, want 2", res.Replacements)
	}
	if got, _ := os.ReadFile(path); strings.Count(string(got), "bar()") != 2 || strings.Contains(string(got), "foo()") {
		t.Errorf("file after --replace-all:\n%s", got)
	}
}

// A unique match still applies, and reports the one replacement it made.
func TestEditBlockUniqueMatchReportsOneReplacement(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := EditBlock(EditBlockOptions{Path: path, OldString: "y := 2", NewString: "y := 3", AllowedDirs: []string{dir}})
	if err != nil {
		t.Fatalf("EditBlock: %v", err)
	}
	if res.Replacements != 1 {
		t.Errorf("replacements = %d, want 1", res.Replacements)
	}
}

func TestSafeEditRefusesAnAmbiguousMatch(t *testing.T) {
	dir, path := writeTwoMatches(t)
	// Dry run too: a preview of the first-match edit would be a preview of an
	// edit the tool must not make.
	for _, dry := range []bool{false, true} {
		res, err := SafeEdit(SafeEditOptions{Path: path, OldString: "foo()", NewString: "bar()", DryRun: dry, AllowedDirs: []string{dir}})
		if err == nil {
			t.Fatalf("dry_run=%v: an old_string matching twice reported success: %+v", dry, res)
		}
		assertAmbiguous(t, path, err, 0, 2)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a refused safe-edit left a backup behind: %v", entries)
	}
}

func TestSafeEditReplaceAllReplacesEveryMatch(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := SafeEdit(SafeEditOptions{Path: path, OldString: "foo()", NewString: "bar()", ReplaceAll: true, AllowedDirs: []string{dir}})
	if err != nil {
		t.Fatalf("SafeEdit --replace-all: %v", err)
	}
	if res.Replacements != 2 {
		t.Errorf("replacements = %d, want 2", res.Replacements)
	}
	if got, _ := os.ReadFile(path); strings.Contains(string(got), "foo()") {
		t.Errorf("file after --replace-all:\n%s", got)
	}
}

// Uniqueness is checked per edit against the content at that point, and one
// ambiguous edit refuses the whole batch: applying the others would leave the
// file half-edited with no way to tell which half.
func TestEditBlocksRefusesAnAmbiguousEdit(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := EditBlocks(EditBlocksOptions{
		Path: path,
		Edits: []EditPair{
			{OldString: "x := 1", NewString: "x := 9"},
			{OldString: "foo()", NewString: "bar()"},
		},
		AllowedDirs: []string{dir},
	})
	if err == nil {
		t.Fatalf("an ambiguous edit reported success: %+v", res)
	}
	assertAmbiguous(t, path, err, 2, 2)
}

// "At that point" matters in both directions: an earlier edit can make a later
// old_string unique (here it renames the first foo()), and that later edit must
// then apply.
func TestEditBlocksChecksUniquenessAgainstTheEditedContent(t *testing.T) {
	dir, path := writeTwoMatches(t)
	_, err := EditBlocks(EditBlocksOptions{
		Path: path,
		Edits: []EditPair{
			{OldString: "x := 1\nfoo()", NewString: "x := 1\nbaz()"},
			{OldString: "foo()", NewString: "bar()"},
		},
		AllowedDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("the second edit was unique once the first applied: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "x := 1\nbaz()\ny := 2\nbar()\n" {
		t.Errorf("file = %q", got)
	}
}

func TestEditBlocksReplaceAllReplacesEveryMatch(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := EditBlocks(EditBlocksOptions{
		Path:        path,
		Edits:       []EditPair{{OldString: "foo()", NewString: "bar()"}},
		ReplaceAll:  true,
		AllowedDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("EditBlocks --replace-all: %v", err)
	}
	if res.Replacements != 2 {
		t.Errorf("replacements = %d, want 2", res.Replacements)
	}
	if got, _ := os.ReadFile(path); strings.Contains(string(got), "foo()") {
		t.Errorf("file after --replace-all:\n%s", got)
	}
}

func TestEditMultipleBlocksRefusesAnAmbiguousEdit(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := EditMultipleBlocks(EditMultipleBlocksOptions{
		Path: path,
		Edits: []MultiEditOperation{
			{OldText: "x := 1", NewText: "x := 9"},
			{OldText: "foo()", NewText: "bar()"},
		},
		Backup:      true,
		AllowedDirs: []string{dir},
	})
	if err == nil {
		t.Fatalf("an ambiguous edit reported success: %+v", res)
	}
	assertAmbiguous(t, path, err, 2, 2)
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a refused edit left a backup behind: %v", entries)
	}
}

func TestEditMultipleBlocksReplaceAllReplacesEveryMatch(t *testing.T) {
	dir, path := writeTwoMatches(t)
	res, err := EditMultipleBlocks(EditMultipleBlocksOptions{
		Path:        path,
		Edits:       []MultiEditOperation{{OldText: "foo()", NewText: "bar()"}},
		ReplaceAll:  true,
		AllowedDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("EditMultipleBlocks --replace-all: %v", err)
	}
	if res.Replacements != 2 {
		t.Errorf("replacements = %d, want 2", res.Replacements)
	}
	if got, _ := os.ReadFile(path); strings.Contains(string(got), "foo()") {
		t.Errorf("file after --replace-all:\n%s", got)
	}
}
