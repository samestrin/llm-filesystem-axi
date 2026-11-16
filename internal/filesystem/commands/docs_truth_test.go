package commands

import (
	"os"
	"strings"
	"testing"
)

// Epic 10 T6 swept the docs to the truth after T1-T5 closed four gaps. Three
// claims had no guard and could rot back the same way the command count and
// the stderr sentence did:
//
//  1. The one remaining principle 7 gap (no session-end capture) must carry
//     its reason — this CLI keeps no session history — wherever it is stated.
//     A gap without a reason reads as an oversight, which is exactly what the
//     owner asked about on 2026-11-13.
//  2. The reference doc and the agent-facing docs must state the T5 gate:
//     move-file and copy-file need --confirm to replace an existing
//     destination. Saying only "deletes need --confirm" sends an agent into a
//     silent overwrite it was told was safe.
//  3. The docs that describe search must say the totals stay exact past
//     --max-results and a capped result carries truncated: true. The old
//     behaviour reported the cap as the total; a doc that stays silent about
//     the marker lets an agent read a capped list as complete.

// sessionEndReasonProblems returns one message per gap item that names the
// session-end gap without its reason, and a message if no item names it at
// all (the gap is still open; dropping it from the list is a deliberate edit
// that must update this test, not a silent pass).
func sessionEndReasonProblems(gaps []string) []string {
	var out []string
	found := false
	for _, g := range gaps {
		if !strings.Contains(g, "session-end capture") {
			continue
		}
		found = true
		if !strings.Contains(g, "keeps no session history") {
			out = append(out, "the session-end gap is stated without its reason (this CLI keeps no session history): "+g)
		}
	}
	if !found {
		out = append(out, "no gap item names the session-end capture gap; it is still open, so the list must state it with its reason")
	}
	return out
}

// Defends T6's "Done when": every known-gaps line maps to a test or a stated
// reason. The session-end gap is the one that stays, so its reason must be
// present in the README bullet, the catalog exception item and the audit.
func TestSessionEndGapStatesItsReason(t *testing.T) {
	readme, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}
	entry, err := os.ReadFile("../../../docs/axi-catalog-entry.yaml")
	if err != nil {
		t.Fatalf("reading docs/axi-catalog-entry.yaml: %v", err)
	}
	for _, msg := range sessionEndReasonProblems(readmeGapBullets(string(readme))) {
		t.Error("README.md: " + msg)
	}
	for _, msg := range sessionEndReasonProblems(catalogExceptionItems(string(entry))) {
		t.Error("docs/axi-catalog-entry.yaml: " + msg)
	}
	audit, err := os.ReadFile("../../../docs/axi-audit.md")
	if err != nil {
		t.Fatalf("reading docs/axi-audit.md: %v", err)
	}
	if !strings.Contains(string(audit), "session-end capture") || !strings.Contains(string(audit), "keeps no session history") {
		t.Error("docs/axi-audit.md must name the remaining session-end capture gap and its reason (this CLI keeps no session history)")
	}
}

// Proves the reason guard can fail: the gap without its reason, and a list
// that dropped the gap entirely, must both be flagged; the reasoned gap passes.
func TestSessionEndReasonGuardFlagsMissingReason(t *testing.T) {
	reasoned := "Principle 7: setup registers three agents; there is no session-end capture, because this CLI keeps no session history"
	if p := sessionEndReasonProblems([]string{reasoned}); len(p) != 0 {
		t.Errorf("the reasoned gap was flagged: %v", p)
	}
	bare := "Principle 7: setup registers three agents; there is no session-end capture"
	if p := sessionEndReasonProblems([]string{bare}); len(p) != 1 {
		t.Errorf("the unreasoned gap gave %d problems, want 1: %v", len(p), p)
	}
	if p := sessionEndReasonProblems([]string{"write-file replaces an existing destination without --confirm"}); len(p) != 1 {
		t.Errorf("a list that dropped the gap gave %d problems, want 1: %v", len(p), p)
	}
}

// overwriteGateProblems returns one message per missing part of the T5 gate
// statement: both command names, the existing-destination condition and
// --confirm. All three together are the claim; any one alone is not.
func overwriteGateProblems(body string) []string {
	var out []string
	for _, want := range []string{"`move-file` and `copy-file`", "when the destination already exists", "--confirm"} {
		if !strings.Contains(body, want) {
			out = append(out, "missing "+want+" from the move/copy overwrite gate statement")
		}
	}
	return out
}

// Defends T5's behaviour in the docs an agent actually reads: the reference
// doc, the routing guide and the on-demand skill all said only "deletes need
// --confirm" while the code already refused an unconfirmed overwrite.
func TestDocsStateTheMoveCopyOverwriteGate(t *testing.T) {
	for _, doc := range []string{
		"../../../docs/llm-filesystem-commands.md",
		"../../../integrations/cli/AGENTS.md",
		"../../../integrations/claude-code/skill/SKILL.md",
	} {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		for _, msg := range overwriteGateProblems(string(body)) {
			t.Errorf("%s: %s", doc, msg)
		}
	}
}

// exactTotalsProblems returns one message per missing part of the T4 claim:
// the cap flag, the truncation marker and the word that says the total is the
// real count, not the cap.
func exactTotalsProblems(body string) []string {
	var out []string
	for _, want := range []string{"`--max-results`", "`truncated: true`", "exact"} {
		if !strings.Contains(body, want) {
			out = append(out, "missing "+want+" from the exact-search-totals statement")
		}
	}
	return out
}

// Defends T4's behaviour in every doc that describes search totals. The
// README's Limits section already stated it; the reference doc and the
// routing guide did not, so an agent reading either could treat a capped
// list's total as the whole count.
func TestSearchDocsStateExactTotals(t *testing.T) {
	for _, doc := range []string{
		"../../../README.md",
		"../../../docs/llm-filesystem-commands.md",
		"../../../integrations/cli/AGENTS.md",
	} {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		for _, msg := range exactTotalsProblems(string(body)) {
			t.Errorf("%s: %s", doc, msg)
		}
	}
}

// Proves both doc guards can fail: the pre-T5 / pre-T4 sentences must be
// flagged, and the full statements must pass.
func TestDocTruthGuardsFlagStaleClaims(t *testing.T) {
	staleGate := "`delete-file` requires `--confirm`. Without it nothing is touched."
	if p := overwriteGateProblems(staleGate); len(p) == 0 {
		t.Error("the deletes-only sentence was not flagged by the overwrite gate guard")
	}
	fullGate := "`delete-file` requires `--confirm`, and so do `move-file` and `copy-file` when the destination already exists."
	if p := overwriteGateProblems(fullGate); len(p) != 0 {
		t.Errorf("the full gate statement was flagged: %v", p)
	}
	staleTotals := "A search lists up to `--max-results` matches and reports the count it collected."
	if p := exactTotalsProblems(staleTotals); len(p) == 0 {
		t.Error("the cap-as-total sentence was not flagged by the exact totals guard")
	}
	fullTotals := "Search totals are exact past `--max-results`; a capped result adds `truncated: true`."
	if p := exactTotalsProblems(fullTotals); len(p) != 0 {
		t.Errorf("the full totals statement was flagged: %v", p)
	}
}