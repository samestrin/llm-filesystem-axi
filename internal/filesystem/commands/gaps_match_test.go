package commands

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// itoa keeps the failure messages free of fmt for consistency with docs_test.go.
func itoa(n int) string { return strconv.Itoa(n) }

// The README's Limits section and the `exception` text in
// docs/axi-catalog-entry.yaml both state the known AXI gaps, and a reader of
// either must see the identical list — a gap named in one place only would
// mislead whoever reads the other. Nothing enforced that, so the two could
// drift the same way the documented command count did. This test splits each
// side into items and compares them item for item, in order.
func TestReadmeLimitsGapsMatchCatalogException(t *testing.T) {
	readme, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	entry, err := os.ReadFile("../../../docs/axi-catalog-entry.yaml")
	if err != nil {
		t.Fatalf("reading docs/axi-catalog-entry.yaml: %v", err)
	}

	bullets := readmeGapBullets(string(readme))
	if len(bullets) == 0 {
		t.Fatal("README.md Limits section names no known-gap bullets; either restore them or delete this test")
	}
	items := catalogExceptionItems(string(entry))
	if len(items) == 0 {
		t.Fatal("docs/axi-catalog-entry.yaml has no exception text; either restore it or delete this test")
	}

	for _, msg := range gapDrift(string(readme), string(entry)) {
		t.Error(msg)
	}
}

// The two known-gap statements must not just agree with each other — they
// must be TRUE. Two claims once shipped that the source contradicts:
//
//  1. "only delete-file and batch-file-operations deletions require
//     [--confirm]" — false: fileops.go:151-172 also refuses a batch move or
//     copy onto an EXISTING destination without --confirm
//     (TestBatchOverwriteRequiresConfirm).
//  2. "with --format text, usage and tool errors go to stderr and stdout is
//     empty" — false for `read-file --bogus --format text`: the unknown flag
//     is hit before --format is parsed, so the diagnostic renders as toon on
//     stdout (TestFormatTextStderrOnlyOnceFormatIsParsed).
//
// This test pins the qualifiers, so the drift guard above cannot be satisfied
// by reintroducing either falsehood in both files at once.
func TestKnownGapStatementsAreTrue(t *testing.T) {
	readme, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	entry, err := os.ReadFile("../../../docs/axi-catalog-entry.yaml")
	if err != nil {
		t.Fatalf("reading docs/axi-catalog-entry.yaml: %v", err)
	}
	for _, text := range []string{string(readme), string(entry)} {
		if !strings.Contains(text, "move or copy onto a path that already exists") &&
			!strings.Contains(text, "move or copy onto an existing") {
			t.Error("the confirm rule omits the batch move/copy overwrite gate (fileops.go:151-172)")
		}
		if !strings.Contains(text, "once --format text is parsed") {
			t.Error("the stderr rule claims errors go to stderr with --format text without the parsed-first qualifier")
		}
	}
	// Epic 10 T1: setup registers Codex too
	// (TestSetupWritesCodexHooksOnceRepairsAfterAMoveAndLeavesThemAlone), so a
	// principle 7 gap saying "Claude Code only" is now false. Only the gap items
	// are checked: the catalog's source observations describe a pinned older
	// revision and stay true of it.
	for _, gaps := range [][]string{readmeGapBullets(string(readme)), catalogExceptionItems(string(entry))} {
		joined := strings.Join(gaps, "\n")
		if strings.Contains(joined, "Claude Code only") || !strings.Contains(joined, ".codex/hooks.json") {
			t.Error("the principle 7 gap must say setup also registers Codex in .codex/hooks.json")
		}
		// Epic 10 T2: and OpenCode
		// (TestSetupWritesTheOpenCodePluginOnceRepairsAfterAMoveAndLeavesItAlone).
		if strings.Contains(joined, "no OpenCode plugin") || !strings.Contains(joined, ".opencode/plugins/") {
			t.Error("the principle 7 gap must say setup also registers OpenCode with a plugin in .opencode/plugins/")
		}
	}
}

// readmeGapBullets returns the bullet lines following the "Known gaps" lead-in
// in the README's Limits section, with the "- " marker stripped.
func readmeGapBullets(readme string) []string {
	var out []string
	inList := false
	for _, line := range strings.Split(readme, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			inList = false
		case strings.HasPrefix(line, "Known gaps against"):
			inList = true
		case inList && strings.HasPrefix(line, "- "):
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(line, "- "), "."))
		case inList && strings.TrimSpace(line) != "":
			inList = false
		}
	}
	return out
}

// catalogExceptionItems extracts the double-quoted exception scalar from the
// catalog entry and splits it into sentences. Each sentence is one gap item;
// the items contain no ". " themselves, so splitting on that boundary is exact.
func catalogExceptionItems(entry string) []string {
	const marker = `exception: "`
	i := strings.Index(entry, marker)
	if i < 0 {
		return nil
	}
	rest := entry[i+len(marker):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return nil
	}
	var out []string
	for _, s := range strings.Split(rest[:j], ". ") {
		out = append(out, strings.TrimSuffix(s, "."))
	}
	return out
}

// Proves the guard can fail: a README-only gap, and a catalog-only rewording,
// must both be caught.
func TestGapsMatchGuardFlagsDrift(t *testing.T) {
	readme := "## Limits\n\nKnown gaps against the principles:\n\n- gap one.\n- gap two.\n"
	entry := `  exception: "gap one. gap two."`
	if msgs := gapDrift(readme, entry); len(msgs) != 0 {
		t.Errorf("matching lists reported drift: %v", msgs)
	}
	if msgs := gapDrift(readme+"- gap three.\n", entry); len(msgs) == 0 {
		t.Error("a README-only gap was not flagged")
	}
	if msgs := gapDrift(readme, `  exception: "gap one. gap two reworded."`); len(msgs) == 0 {
		t.Error("a reworded catalog item was not flagged")
	}
}

// gapDrift is TestReadmeLimitsGapsMatchCatalogException's comparison over
// strings, so the sabotage test above can feed it synthetic input.
func gapDrift(readme, entry string) []string {
	bullets := readmeGapBullets(readme)
	items := catalogExceptionItems(entry)
	var msgs []string
	if len(bullets) != len(items) {
		msgs = append(msgs, "README.md names "+itoa(len(bullets))+" gaps, the catalog exception names "+itoa(len(items))+"; they must match item for item")
	}
	for i, b := range bullets {
		if i >= len(items) {
			break
		}
		if items[i] != b {
			msgs = append(msgs, "gap "+itoa(i+1)+" differs:\n  README:  "+b+"\n  catalog: "+items[i])
		}
	}
	return msgs
}
