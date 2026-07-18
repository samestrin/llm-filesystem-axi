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
