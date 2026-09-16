package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// hasNestedTOONField is hasTOONField for a field inside a list item, which TOON
// indents. It still anchors at the start of the trimmed line rather than
// searching anywhere in it, so the failure hasTOONField exists to prevent -- a
// t.TempDir() path containing the field name and matching as a substring --
// cannot happen here either. A path is printed as a value after "file: ", never
// at the start of its own line.
func hasNestedTOONField(out, field string) bool {
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimLeft(line, " \t-")
		if strings.HasPrefix(trimmed, field+":") || strings.HasPrefix(trimmed, field+"[") {
			return true
		}
	}
	return false
}

// searchFixture writes a file whose match sits in the middle, so context lines
// above and below it are distinguishable from the matched line itself.
func searchFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := "line above alpha\nthe NEEDLE is here\nline below beta\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// --context N was accepted, the context lines were read, and then the minimal
// projection dropped them on the floor and the command exited 0. Output was
// byte-identical to the same search with no --context at all, so an agent that
// asked for context got none and was told it had succeeded.
//
// CodeMatch carries File, Line, Content and Context; the projection kept the
// first three unconditionally. An explicitly requested field is more specific
// than a default minimal schema and has to win, the same way an explicit
// --max-size already beats --full.
func TestSearchCodeContextSurvivesTheMinimalProjection(t *testing.T) {
	dir := searchFixture(t)

	without, _, code := runCLI(t, "search-code", "--path", dir, "--pattern", "NEEDLE")
	if code != int(goaxi.ExitOK) && code != -1 {
		t.Fatalf("search without --context exited %d", code)
	}
	with, _, code := runCLI(t, "search-code", "--path", dir, "--pattern", "NEEDLE", "--context", "1")
	if code != int(goaxi.ExitOK) && code != -1 {
		t.Fatalf("search with --context exited %d", code)
	}

	if with == without {
		t.Errorf("--context 1 produced byte-identical output to no --context;\nthe flag was accepted and ignored:\n%s", with)
	}
	if !hasNestedTOONField(with, "context") {
		t.Errorf("--context 1 did not emit a context field:\n%s", with)
	}
	if !strings.Contains(with, "line above alpha") {
		t.Errorf("--context 1 did not return the surrounding line:\n%s", with)
	}
}

// The default must not change for callers who never ask for context. An empty
// context field appearing on every match would cost tokens for nothing, which
// is the opposite of what the minimal field set exists to do.
func TestSearchCodeWithoutContextIsUnchanged(t *testing.T) {
	dir := searchFixture(t)

	out, _, code := runCLI(t, "search-code", "--path", dir, "--pattern", "NEEDLE")
	if code != int(goaxi.ExitOK) && code != -1 {
		t.Fatalf("exit = %d", code)
	}
	if hasNestedTOONField(out, "context") {
		t.Errorf("a search with no --context emitted a context field:\n%s", out)
	}
	if strings.Contains(out, "line above alpha") {
		t.Errorf("a search with no --context returned surrounding lines:\n%s", out)
	}
}

// --full with --context already worked; this pins it so the fix cannot be
// satisfied by moving the breakage to the other mode.
func TestSearchCodeFullWithContextStillWorks(t *testing.T) {
	dir := searchFixture(t)

	out, _, code := runCLI(t, "search-code", "--path", dir, "--pattern", "NEEDLE", "--context", "1", "--full")
	if code != int(goaxi.ExitOK) && code != -1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "line above alpha") {
		t.Errorf("--full --context 1 lost the surrounding line:\n%s", out)
	}
}

// AC10: help never suggests the impossible. The line read "Add --full for
// surrounding context lines", but --full alone returns no context whatsoever -
// context exists only when --context N asked for it. An agent following that
// line runs a second search and gets the identical output again.
func TestSearchCodeHelpNamesTheFlagThatActuallyReturnsContext(t *testing.T) {
	dir := searchFixture(t)

	out, _, _ := runCLI(t, "search-code", "--path", dir, "--pattern", "NEEDLE")
	if strings.Contains(out, "Add --full for surrounding context") {
		t.Errorf("help still advises --full, which returns no context on its own:\n%s", out)
	}
	if !strings.Contains(out, "--context") {
		t.Errorf("help does not name --context, the flag that actually returns context:\n%s", out)
	}

	// Already asked for context: offering it again is noise. The assertion is
	// on "add --context", the string the hint emits today, so it goes red the
	// moment the contextLines gate that suppresses the hint stops firing.
	withCtx, _, _ := runCLI(t, "search-code", "--path", dir, "--pattern", "NEEDLE", "--context", "1")
	if strings.Contains(withCtx, "add --context") {
		t.Errorf("help offered context to a caller who already requested it:\n%s", withCtx)
	}
}

// Cobra accepts a negative --context and core treats it as no context (its
// gate is contextLines > 0), so the output must match a zero-context search:
// no context field, and the hint naming --context as the way to get it. The
// projection and hint gates used different predicates (contextLines > 0 vs
// contextLines == 0), so --context -1 fell between them and produced output
// with neither context lines nor the line saying how to get them.
func TestSearchCodeNegativeContextBehavesAsNoContext(t *testing.T) {
	dir := searchFixture(t)

	out, _, code := runCLI(t, "search-code", "--path", dir, "--pattern", "NEEDLE", "--context", "-1")
	if code != int(goaxi.ExitOK) && code != -1 {
		t.Fatalf("exit = %d", code)
	}
	if hasNestedTOONField(out, "context") {
		t.Errorf("--context -1 emitted a context field core never populated:\n%s", out)
	}
	if strings.Contains(out, "line above alpha") {
		t.Errorf("--context -1 returned surrounding lines:\n%s", out)
	}
	if !strings.Contains(out, "add --context") {
		t.Errorf("--context -1 suppresses the hint that names --context, leaving no way to learn how to get context:\n%s", out)
	}
}
