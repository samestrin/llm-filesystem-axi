package commands

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The documented command count drifted three ways at once: the CLI's own
// description said 27, README said 27, and docs/llm-filesystem-commands.md said
// 26 — against a real 28.
//
// It drifted because nothing could check it: CI's path filter used to match
// only ['**.go','go.mod','go.sum'], so a docs-only change ran no CI at all.
// The filter now covers markdown too, but this test keeps the one claim that
// keeps rotting checked on every local and CI run, independently of filters.
func TestDocumentedCommandCountMatchesReality(t *testing.T) {
	const doc = "../../../docs/llm-filesystem-commands.md"

	body, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("reading %s: %v", doc, err)
	}

	m := regexp.MustCompile(`(\d+) commands`).FindSubmatch(body)
	if m == nil {
		t.Fatalf("%s no longer states a command count; either restore it or delete this test", doc)
	}
	documented, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("parsing the documented count: %v", err)
	}

	if actual := realCommandCount(); documented != actual {
		t.Errorf("%s says %d commands, the binary has %d", doc, documented, actual)
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
