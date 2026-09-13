package core

import (
	"os"
	"path/filepath"
	"testing"
)

// Both searches normalize the path and check it against the sandbox, but neither
// ever stats it. NormalizePath only expands ~, cleans, and makes the path
// absolute; ValidatePath answers "is this inside the allowed directories", not
// "does this exist". So a path that is not there walks nothing and returns a
// clean zero — no error, and the CLI exits 0.
//
// "I searched and found nothing" and "that directory is not there" are different
// answers, and an agent that cannot tell them apart will conclude the code it
// was looking for does not exist when it simply mistyped the path.
func TestSearchRefusesAMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "definitely-not-here")

	t.Run("search-code", func(t *testing.T) {
		res, err := SearchCode(SearchCodeOptions{
			Path:    missing,
			Pattern: "anything",
		})

		if err == nil {
			t.Errorf("a missing path returned a clean result instead of an error: %+v", res)
		}
	})

	t.Run("search-files", func(t *testing.T) {
		res, err := SearchFiles(SearchFilesOptions{
			Path:    missing,
			Pattern: "*.go",
		})

		if err == nil {
			t.Errorf("a missing path returned a clean result instead of an error: %+v", res)
		}
	})
}

// A path that exists but is a FILE is equally not searchable as a directory.
func TestSearchRefusesAFilePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	if res, err := SearchCode(SearchCodeOptions{Path: file, Pattern: "hello"}); err == nil {
		t.Errorf("a file path was accepted as a search root: %+v", res)
	}
}

// The happy path must keep working: a real directory with a real match still
// returns that match and no error.
func TestSearchStillFindsMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle here\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle"})
	if err != nil {
		t.Fatalf("searching a real directory failed: %v", err)
	}
	if res.TotalMatches != 1 {
		t.Errorf("total_matches = %d, want 1", res.TotalMatches)
	}

	files, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*.txt"})
	if err != nil {
		t.Fatalf("searching a real directory failed: %v", err)
	}
	if files.Total != 1 {
		t.Errorf("total = %d, want 1", files.Total)
	}
}
