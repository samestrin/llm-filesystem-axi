package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AC6 review finding 1 (EST-6846): EST-6608 fixed items: null for
// list-directory, but search-code and search-files with no match still printed
// matches: null, and find-large-files on a directory with nothing large printed
// files: null with no help[] at all. AXI principle 5 asks for a definitive
// empty state; null reads as "no answer" rather than "nothing here". Each list
// is an empty array in TOON and JSON, the total stays beside it at 0, and
// help[] says what to try next.
func TestEmptySearchResultsAreEmptyListsNotNull(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()

	for _, tc := range []struct {
		name, list, total string
		args              []string
	}{
		{"search-code", "matches", "total_matches", []string{"search-code", "--path", dir, "--pattern", "zzz"}},
		{"search-files", "matches", "total", []string{"search-files", "--path", dir, "--pattern", "*.zzz"}},
		{"find-large-files", "files", "total", []string{"find-large-files", "--path", empty}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, code := runCLI(t, append(tc.args, "--format", "toon")...)
			if code != -1 {
				t.Fatalf("toon exit %d\n%s", code, stdout)
			}
			if !hasTOONField(stdout, tc.list+"[0]") || strings.Contains(stdout, tc.list+": null") {
				t.Errorf("toon: want %s[0]:, not null\n%s", tc.list, stdout)
			}
			if !hasTOONField(stdout, tc.total) || !strings.Contains(stdout, "\n"+tc.total+": 0\n") {
				t.Errorf("toon: want %s: 0\n%s", tc.total, stdout)
			}
			if !hasTOONField(stdout, "help") {
				t.Errorf("toon: want an empty-state help[] line\n%s", stdout)
			}

			stdout, _, code = runCLI(t, append(tc.args, "--format", "json")...)
			if code != -1 {
				t.Fatalf("json exit %d", code)
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("json: %v\n%s", err, stdout)
			}
			if string(doc[tc.list]) != "[]" || string(doc[tc.total]) != "0" {
				t.Errorf("json: %s = %s, %s = %s; want [], 0", tc.list, doc[tc.list], tc.total, doc[tc.total])
			}
		})
	}
}

// Review of EST-6846: the find-large-files empty state has two help shapes,
// and the test above only reached the --min-size 0 one. Above 0, files may
// exist below the threshold, so lowering it is a real next step; at 0 there
// are no files at all, and suggesting it would only repeat the empty answer.
// Both shapes are pinned here, in TOON and JSON.
func TestFindLargeFilesEmptyStateHelpNamesTheThreshold(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const lower = "Lower the threshold: --min-size is 1000000 bytes"
	const tree = "See what is there: llm-filesystem get-directory-tree --path "

	for _, tc := range []struct {
		name      string
		args      []string
		threshold bool
	}{
		{"above-threshold", []string{"find-large-files", "--path", dir, "--min-size", "1000000"}, true},
		{"no-files", []string{"find-large-files", "--path", t.TempDir()}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, code := runCLI(t, append(tc.args, "--format", "toon")...)
			if code != -1 {
				t.Fatalf("toon exit %d\n%s", code, stdout)
			}
			if !hasTOONField(stdout, "files[0]") || !strings.Contains(stdout, "\ntotal: 0\n") {
				t.Errorf("toon: want files[0]: and total: 0\n%s", stdout)
			}
			if got := strings.Contains(stdout, lower); got != tc.threshold {
				t.Errorf("toon: threshold line present = %v, want %v\n%s", got, tc.threshold, stdout)
			}
			if !strings.Contains(stdout, tree) {
				t.Errorf("toon: want the get-directory-tree step\n%s", stdout)
			}

			stdout, _, code = runCLI(t, append(tc.args, "--format", "json")...)
			if code != -1 {
				t.Fatalf("json exit %d", code)
			}
			var doc struct {
				Files json.RawMessage `json:"files"`
				Total int             `json:"total"`
				Help  []string        `json:"next_steps"`
			}
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("json: %v\n%s", err, stdout)
			}
			if string(doc.Files) != "[]" || doc.Total != 0 {
				t.Errorf("json: files = %s, total = %d; want [], 0", doc.Files, doc.Total)
			}
			want := 1
			if tc.threshold {
				want = 2
			}
			if len(doc.Help) != want || (tc.threshold && doc.Help[0] != lower) ||
				!strings.HasPrefix(doc.Help[len(doc.Help)-1], tree) {
				t.Errorf("json: help = %q; want %d steps, threshold first when present, tree last", doc.Help, want)
			}
		})
	}
}
