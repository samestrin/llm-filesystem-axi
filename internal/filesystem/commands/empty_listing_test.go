package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

// Fresh-context review finding 3 (EST-6608): an empty directory listed as
// items: null. AXI principle 5 asks for a definitive empty state; null reads as
// "no answer" rather than "nothing here". The list is an empty array in every
// format, beside total: 0, and with a filter that matches nothing too.
func TestListDirectoryEmptyIsAnEmptyListNotNull(t *testing.T) {
	for _, args := range [][]string{
		{"list-directory", "--path", t.TempDir()},
		{"list-directory", "--path", t.TempDir(), "--pattern", "*.nothing"},
	} {
		t.Run(strings.Join(args[len(args)-1:], ""), func(t *testing.T) {
			stdout, _, code := runCLI(t, append(args, "--format", "toon")...)
			if code != -1 {
				t.Fatalf("toon exit %d", code)
			}
			if !hasTOONField(stdout, "items[0]") || strings.Contains(stdout, "items: null") {
				t.Errorf("toon: want items[0]:, not null\n%s", stdout)
			}
			if !strings.Contains(stdout, "\ntotal: 0\n") {
				t.Errorf("toon: want total: 0\n%s", stdout)
			}

			stdout, _, code = runCLI(t, append(args, "--format", "json")...)
			if code != -1 {
				t.Fatalf("json exit %d", code)
			}
			var doc map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("json: %v\n%s", err, stdout)
			}
			if string(doc["items"]) != "[]" || string(doc["total"]) != "0" {
				t.Errorf("json: items = %s, total = %s; want [], 0", doc["items"], doc["total"])
			}

			stdout, _, code = runCLI(t, append(args, "--format", "text")...)
			if code != -1 || !strings.Contains(stdout, "(0 entries)") || strings.Contains(stdout, "null") {
				t.Errorf("text: exit %d\n%s", code, stdout)
			}
		})
	}
}
