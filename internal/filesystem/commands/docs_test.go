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
// It drifted because nothing could check it. CI's path filter is
// ['**.go','go.mod','go.sum'], so a docs-only change runs no CI at all. This
// test lives in a .go file specifically so the one claim that keeps rotting is
// checked on every run that matters.
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
