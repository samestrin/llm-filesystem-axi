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
