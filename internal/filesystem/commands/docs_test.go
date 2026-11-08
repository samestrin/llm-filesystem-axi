package commands

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The documented command count drifted three ways at once: the CLI's own
// description said 27, README said 27, and docs/llm-filesystem-commands.md said
// 26 — against a real 28. It drifted again when T4 had to correct five files.
//
// It drifted because nothing could check it: CI's path filter used to match
// only ['**.go','go.mod','go.sum'], so a docs-only change ran no CI at all.
// The filter now covers markdown too, but this test keeps the one claim that
// keeps rotting checked on every local and CI run, independently of filters.
//
// Every file that states a count is checked, not only the commands doc.
// CHANGELOG.md is deliberately absent: its 1.0.0 entry says 28 commands and
// that is history, not a claim about the current binary.
var countStatingFiles = []string{
	"../../../README.md",
	"../../../cmd/llm-filesystem/README.md",
	"../../../integrations/claude-code/README.md",
	"../../../docs/llm-filesystem-commands.md",
	"../../../release/axi-catalog-entry.md",
}

var commandCountRe = regexp.MustCompile(`(\d+) commands`)

// countMismatches returns one message per "N commands" claim in body that
// differs from actual, or a message if body states no count at all.
func countMismatches(name string, body []byte, actual int) []string {
	ms := commandCountRe.FindAllSubmatch(body, -1)
	if ms == nil {
		return []string{name + " no longer states a command count; either restore it or remove it from countStatingFiles"}
	}
	var out []string
	for _, m := range ms {
		documented, err := strconv.Atoi(string(m[1]))
		if err != nil {
			out = append(out, name+": parsing the documented count: "+err.Error())
			continue
		}
		if documented != actual {
			out = append(out, name+" says "+strconv.Itoa(documented)+" commands, the binary has "+strconv.Itoa(actual))
		}
	}
	return out
}

func TestDocumentedCommandCountMatchesReality(t *testing.T) {
	actual := realCommandCount()
	for _, doc := range countStatingFiles {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		for _, msg := range countMismatches(doc, body, actual) {
			t.Error(msg)
		}
	}
}

// Proves the guard can fail: a file stating the old count (28) must be
// flagged, wherever it appears, and a file with no count must be flagged too.
//
// This loop alone does not prove every file is checked — it iterates the same
// list TestCountStatingFilesCoversEveryKnownFile pins, so an entry deleted from
// countStatingFiles would simply stop being examined here, silently. The pair
// is what makes the coverage real.
func TestCountGuardFlagsStaleOrMissingCount(t *testing.T) {
	actual := realCommandCount()
	stale := strconv.Itoa(actual - 1)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"current count", "a CLI with " + strconv.Itoa(actual) + " commands", 0},
		{"stale count", "a CLI with " + stale + " commands", 1},
		{"stale second mention", strconv.Itoa(actual) + " commands, later " + stale + " commands", 1},
		{"no count", "a CLI", 1},
	}
	for _, c := range cases {
		if got := countMismatches("f.md", []byte(c.body), actual); len(got) != c.want {
			t.Errorf("%s: got %d problems %v, want %d", c.name, len(got), got, c.want)
		}
	}
	// Each real file, with its count rewritten to the stale number, must fail.
	for _, doc := range countStatingFiles {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		mutated := commandCountRe.ReplaceAll(body, []byte(stale+" commands"))
		if len(countMismatches(doc, mutated, actual)) == 0 {
			t.Errorf("%s: stating %s commands was not flagged", doc, stale)
		}
	}
}

// Every assertion above is driven by countStatingFiles, so the list is the only
// thing that decides which files are checked: delete an entry and both tests go
// on passing, having quietly stopped looking at that file. This pins the list
// itself, to the five files that state a count.
//
// CHANGELOG.md and docs/axi-audit.md both mention counts as history ("28
// commands" in the 1.0.0 entry, the audit's note that --help listed 28 before
// setup existed) and are deliberately not listed.
func TestCountStatingFilesCoversEveryKnownFile(t *testing.T) {
	want := []string{
		"../../../README.md",
		"../../../cmd/llm-filesystem/README.md",
		"../../../integrations/claude-code/README.md",
		"../../../docs/llm-filesystem-commands.md",
		"../../../release/axi-catalog-entry.md",
	}

	if len(countStatingFiles) != len(want) {
		t.Fatalf("countStatingFiles has %d entries, want %d: %v",
			len(countStatingFiles), len(want), countStatingFiles)
	}
	for i := range want {
		if countStatingFiles[i] != want[i] {
			t.Errorf("countStatingFiles[%d] = %q, want %q — a file is checked by name or not at all",
				i, countStatingFiles[i], want[i])
		}
	}
}

// The agent-facing routing table must match the binary EXACTLY, in both
// directions.
//
// A table that omits a command is worse than no table: the agent concludes the
// capability does not exist and works around it. A table naming a command that
// was removed sends the agent into a guaranteed failure.
//
// TestDocumentedCommandCountMatchesReality above compares a number, which
// catches neither of those — a rename keeps the count identical. This compares
// the sets.
func TestEveryCommandIsDocumentedForAgents(t *testing.T) {
	const doc = "../../../integrations/cli/AGENTS.md"

	body, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("reading %s: %v", doc, err)
	}

	// Commands are table rows shaped: | `name` | description |
	documented := map[string]bool{}
	rows := regexp.MustCompile("(?m)^\\|\\s*`([a-z][a-z-]+)`\\s*\\|").FindAllSubmatch(body, -1)
	for _, m := range rows {
		documented[string(m[1])] = true
	}
	if len(documented) == 0 {
		t.Fatalf("%s lists no commands; rows must be shaped | `name` | description |", doc)
	}

	actual := map[string]bool{}
	for _, c := range RootCmd().Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		actual[c.Name()] = true
	}

	for name := range actual {
		if !documented[name] {
			t.Errorf("%s does not list %q — an agent reading it concludes the capability is absent", doc, name)
		}
	}
	for name := range documented {
		if !actual[name] {
			t.Errorf("%s lists %q, which the binary does not have — an agent following it will fail", doc, name)
		}
	}
}

// realCommandCount counts what a user would see as a command: cobra's built-in
// help and completion are excluded by name rather than by arithmetic, because
// cobra adds them lazily and they may not be present at construction time.
func realCommandCount() int {
	n := 0
	for _, c := range RootCmd().Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		n++
	}
	return n
}
