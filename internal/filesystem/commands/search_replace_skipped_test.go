package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// search-and-replace left a matched file alone, hard-linked under
// --allowed-dirs or unwritable, and the result did not say so: a run that
// changed nothing at all printed "Modified 0 files" and exited 0 (epic 16 T1).

// linkedTree lays out allowed/{a,b}.txt both holding "foo\n", with b also
// linked from outside allowed. It returns allowed, a and b.
func linkedTree(t *testing.T) (allowed, a, b string) {
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

func replaceArgs(format, allowed string) []string {
	return []string{"--allowed-dirs", allowed, "--format", format,
		"search-and-replace", "--path", allowed, "--pattern", "foo", "--replacement", "bar"}
}

func TestSearchAndReplaceReportsSkippedInEveryFormat(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		allowed, a, b := linkedTree(t)
		stdout, _, code := runCLI(t, replaceArgs("json", allowed)...)

		if code != -1 {
			t.Errorf("exit = %d, want 0: a.txt was written", code)
		}
		assertFileContent(t, a, "bar\n")
		assertFileContent(t, b, "foo\n")
		var got struct {
			FilesModified int `json:"files_modified"`
			Skipped       []struct {
				Path   string `json:"path"`
				Reason string `json:"reason"`
			} `json:"skipped"`
			NextSteps []string `json:"next_steps"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("not one JSON document: %v: %q", err, stdout)
		}
		if len(got.Skipped) != 1 || got.Skipped[0].Path != b || !strings.Contains(got.Skipped[0].Reason, "hard links") {
			t.Errorf("skipped = %+v, want b.txt with the hard-link reason", got.Skipped)
		}
		if len(got.NextSteps) == 0 || !strings.HasPrefix(got.NextSteps[0], "Not written (1)") {
			t.Errorf("next_steps does not name the skipped count: %q", got.NextSteps)
		}
	})

	t.Run("toon", func(t *testing.T) {
		allowed, _, b := linkedTree(t)
		stdout, _, _ := runCLI(t, replaceArgs("toon", allowed)...)

		if !hasTOONField(stdout, "skipped") || !strings.Contains(stdout, b) {
			t.Errorf("TOON does not list b.txt under skipped: %q", stdout)
		}
		if !strings.Contains(stdout, "Not written (1)") {
			t.Errorf("help[] does not name the skipped count: %q", stdout)
		}
	})

	t.Run("text", func(t *testing.T) {
		allowed, _, b := linkedTree(t)
		stdout, _, _ := runCLI(t, replaceArgs("text", allowed)...)

		if !strings.Contains(stdout, "1 skipped") || !strings.Contains(stdout, b+": ") {
			t.Errorf("text does not report the skip: %q", stdout)
		}
	})
}

// A run that matched files and wrote none of them did not succeed, so it
// must not exit 0.
func TestSearchAndReplaceWithEveryMatchSkippedExitsNonZero(t *testing.T) {
	allowed, a, b := linkedTree(t)
	if err := os.WriteFile(a, []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCLI(t, replaceArgs("json", allowed)...)

	if code != 1 {
		t.Errorf("exit = %d, want 1: every matched file was skipped", code)
	}
	assertFileContent(t, a, "nothing here\n")
	assertFileContent(t, b, "foo\n")
	if entries, _ := os.ReadDir(allowed); len(entries) != 2 {
		t.Errorf("the tree changed: %v", entries)
	}
	if !strings.Contains(stdout, b) {
		t.Errorf("the result does not name the skipped file: %q", stdout)
	}
}

// Known-good: no match at all is not a failure; nothing was left undone.
func TestSearchAndReplaceWithNoMatchStillExitsZero(t *testing.T) {
	allowed, _, _ := linkedTree(t)
	_, _, code := runCLI(t, "--allowed-dirs", allowed,
		"search-and-replace", "--path", allowed, "--pattern", "absent", "--replacement", "x")
	if code != -1 {
		t.Errorf("exit = %d, want 0", code)
	}
}
