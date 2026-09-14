package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditBlock(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "code.go")
	if err := os.WriteFile(f, []byte("hello world\ngoodbye world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := EditBlock(EditBlockOptions{Path: f, OldString: "hello world", NewString: "hi there"})
	if err != nil {
		t.Fatalf("EditBlock: %v", err)
	}
	if !res.Success {
		t.Errorf("EditBlock not successful: %+v", res)
	}
	got, _ := os.ReadFile(f)
	if !strings.Contains(string(got), "hi there") || strings.Contains(string(got), "hello world") {
		t.Errorf("file after edit = %q", got)
	}
}

func TestEditBlockMissingTargetErrors(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "code.go")
	if err := os.WriteFile(f, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EditBlock(EditBlockOptions{Path: f, OldString: "not present", NewString: "x"}); err == nil {
		t.Error("editing a non-existent block should error")
	}
}

// EditBlock, the singular form, already refuses when its target is not in the
// file — TestEditBlockMissingTargetErrors pins that. EditBlocks did not. It
// reported:
//
//	changes: 0
//	message: Applied 0 edits successfully
//	success: true
//
// at exit 0, having changed nothing. Two sibling functions disagreeing about
// whether a no-op is a success is not a design choice, it is a bug.
//
// It matters most through JSON. The --edits payload decodes into
// EditPair{old_string,new_string}, and Go ignores unknown fields, so a payload
// keyed {old,new} — which this project's own documentation shipped for months —
// decodes to entirely empty edits, skips every one, and reports success.
func TestEditBlocksFailsWhenNothingMatched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := EditBlocks(EditBlocksOptions{
		Path:        path,
		Edits:       []EditPair{{OldString: "absent", NewString: "x"}},
		AllowedDirs: []string{dir},
	})

	if err == nil {
		t.Errorf("an edit that matched nothing reported success: %+v", res)
	}
	if got, _ := os.ReadFile(path); string(got) != "hello world" {
		t.Errorf("file was rewritten despite nothing matching: %q", got)
	}
}

// The wrong-JSON-key symptom, at the level where it actually bites. An edit with
// no old_string cannot match anything, and skipping it silently is what turned a
// mistyped payload into a reported success.
func TestEditBlocksFailsOnAnEmptyOldString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := EditBlocks(EditBlocksOptions{
		Path:        path,
		Edits:       []EditPair{{OldString: "", NewString: "x"}},
		AllowedDirs: []string{dir},
	})

	if err == nil {
		t.Errorf("an empty old_string was accepted and reported as success: %+v", res)
	}
}

// Partial application is a real success — some work happened — but the caller
// must be told what did NOT apply, or it assumes all of it did.
func TestEditBlocksReportsPartialApplication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("keep alpha here"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := EditBlocks(EditBlocksOptions{
		Path: path,
		Edits: []EditPair{
			{OldString: "alpha", NewString: "beta"},
			{OldString: "nowhere", NewString: "x"},
		},
		AllowedDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("a partially applied edit is still a success: %v", err)
	}

	if res.Changes != 1 {
		t.Errorf("changes = %d, want 1", res.Changes)
	}
	if !strings.Contains(res.Message, "1") || !strings.Contains(res.Message, "2") {
		t.Errorf("message = %q, want it to say how many of how many applied", res.Message)
	}
	// Structured, so a caller detects the partial result without reading prose.
	if res.Unmatched != 1 {
		t.Errorf("unmatched = %d, want 1", res.Unmatched)
	}
	if got, _ := os.ReadFile(path); string(got) != "keep beta here" {
		t.Errorf("content = %q, want the matching edit applied", got)
	}
}

// The happy path must keep working, or the fix above is just breakage.
func TestEditBlocksStillAppliesEveryMatchingEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("one two"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := EditBlocks(EditBlocksOptions{
		Path: path,
		Edits: []EditPair{
			{OldString: "one", NewString: "1"},
			{OldString: "two", NewString: "2"},
		},
		AllowedDirs: []string{dir},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Changes != 2 {
		t.Errorf("changes = %d, want 2", res.Changes)
	}
	if got, _ := os.ReadFile(path); string(got) != "1 2" {
		t.Errorf("content = %q, want %q", got, "1 2")
	}
}

func TestSearchAndReplace(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("foo foo foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := SearchAndReplace(SearchReplaceOptions{Path: f, Pattern: "foo", Replacement: "bar"})
	if err != nil {
		t.Fatalf("SearchAndReplace: %v", err)
	}
	if res.TotalChanges == 0 {
		t.Errorf("expected changes, got %+v", res)
	}
	got, _ := os.ReadFile(f)
	if strings.Contains(string(got), "foo") {
		t.Errorf("replacement incomplete: %q", got)
	}
}

func TestSearchAndReplaceDryRunLeavesFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	original := "keep me foo"
	if err := os.WriteFile(f, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SearchAndReplace(SearchReplaceOptions{Path: f, Pattern: "foo", Replacement: "bar", DryRun: true}); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	got, _ := os.ReadFile(f)
	if string(got) != original {
		t.Errorf("dry run modified file: %q, want %q", got, original)
	}
}
