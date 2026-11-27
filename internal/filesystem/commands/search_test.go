package commands

import (
	"encoding/json"
	"fmt"
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

// capFixture writes n lines containing "needle" across files of 500 lines, and
// n empty files named *.hit, so search-code and search-files both have exactly
// n matches.
func capFixture(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	for f := 0; f*500 < n; f++ {
		lines := min(500, n-f*500)
		body := strings.Repeat("a needle on this line\n", lines)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", f)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("m%d.hit", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Epic 10 T4, the task's "done when". Both searches stopped at --max-results
// (default 1000) and printed the cap as the total, with no marker and no hint,
// so 1,500 matches read as "total_matches: 1000" — a wrong number an agent had
// no way to doubt. A capped search must give the exact total, the shown count,
// truncated: true, and a help line naming --max-results.
func TestSearchCappedShowsExactTotalMarkerAndHint(t *testing.T) {
	dir := capFixture(t, 1500)

	for _, c := range []struct {
		name, totalField string
		args             []string
	}{
		{"search-code", "total_matches", []string{"search-code", "--path", dir, "--pattern", "needle"}},
		{"search-files", "total", []string{"search-files", "--path", dir, "--pattern", "*.hit"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, _, code := runCLI(t, c.args...)
			if code != int(goaxi.ExitOK) && code != -1 {
				t.Fatalf("exit = %d\n%s", code, out)
			}
			for _, want := range []string{c.totalField + ": 1500", "shown: 1000", "truncated: true"} {
				if !hasTOONLine(out, want) {
					t.Errorf("missing %q:\n%s", want, firstLines(out, 8))
				}
			}
			if c.name == "search-code" && !hasTOONLine(out, "total_files: 3") {
				t.Errorf("total_files is not the exact 3:\n%s", firstLines(out, 8))
			}
			if !helpNames(out, "--max-results") {
				t.Errorf("no help line names --max-results:\n%s", tail(out, 6))
			}

			js, _, _ := runCLI(t, append(c.args, "--format", "json")...)
			var payload map[string]interface{}
			if err := json.Unmarshal([]byte(js), &payload); err != nil {
				t.Fatalf("json: %v\n%s", err, js)
			}
			if payload[c.totalField] != float64(1500) || payload["shown"] != float64(1000) || payload["truncated"] != true {
				t.Errorf("json %s=%v shown=%v truncated=%v; want 1500, 1000, true",
					c.totalField, payload[c.totalField], payload["shown"], payload["truncated"])
			}
			if n := len(payload["matches"].([]interface{})); n != 1000 {
				t.Errorf("json listed %d matches, want 1000", n)
			}
			if !strings.Contains(fmt.Sprint(payload["next_steps"]), "--max-results") {
				t.Errorf("json next_steps do not name --max-results: %v", payload["next_steps"])
			}

			text, _, _ := runCLI(t, append(c.args, "--format", "text")...)
			if !strings.Contains(firstLines(text, 1), "1500") || !strings.Contains(firstLines(text, 1), "showing 1000") {
				t.Errorf("text header does not give 1000 of 1500: %q", firstLines(text, 1))
			}
			if !strings.Contains(text, "--max-results") {
				t.Errorf("text output does not name --max-results:\n%s", tail(text, 6))
			}
		})
	}
}

// The other half of "done when": ten matches under the cap carry no marker and
// no hint. A truncated: false or a --max-results line on every ordinary search
// would be noise, and would train an agent to ignore the real one.
func TestSearchUnderTheCapHasNoMarkerOrHint(t *testing.T) {
	dir := capFixture(t, 10)

	for _, args := range [][]string{
		{"search-code", "--path", dir, "--pattern", "needle"},
		{"search-files", "--path", dir, "--pattern", "*.hit"},
		{"search-code", "--path", dir, "--pattern", "needle", "--max-results", "10"},
	} {
		for _, format := range []string{"toon", "json", "text"} {
			out, _, _ := runCLI(t, append(args, "--format", format)...)
			// "showing" is the text format's spelling of the marker.
			for _, bad := range []string{"truncated", "shown", "showing", "--max-results"} {
				if strings.Contains(out, bad) {
					t.Errorf("%v --format %s mentions %q with nothing cut:\n%s", args, format, bad, out)
				}
			}
			if !strings.Contains(out, "10") {
				t.Errorf("%v --format %s does not report 10 matches:\n%s", args, format, out)
			}
		}
	}
}

// Epic 14 T1 at the CLI, the task's "done when": a tree with more than 1000
// matches searched with --max-results -1 must list every match, with no
// truncation marker and no cap hint, in every format. Before the fix the flag
// was accepted and printed matches[0] with truncated: true — exit 0 having
// listed nothing.
func TestSearchMaxResultsMinusOneListsEveryMatch(t *testing.T) {
	dir := capFixture(t, 1500)

	for _, c := range []struct {
		name, totalField string
		args             []string
	}{
		{"search-code", "total_matches", []string{"search-code", "--path", dir, "--pattern", "needle", "--max-results", "-1"}},
		{"search-files", "total", []string{"search-files", "--path", dir, "--pattern", "*.hit", "--max-results", "-1"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, _, code := runCLI(t, c.args...)
			if code != int(goaxi.ExitOK) && code != -1 {
				t.Fatalf("exit = %d\n%s", code, out)
			}
			if !hasTOONLine(out, c.totalField+": 1500") {
				t.Errorf("missing exact total of 1500:\n%s", firstLines(out, 8))
			}
			for _, bad := range []string{"truncated", "shown"} {
				if hasTOONField(out, bad) {
					t.Errorf("--max-results -1 carried a %s marker with nothing cut:\n%s", bad, firstLines(out, 8))
				}
			}
			if helpNames(out, "--max-results") {
				t.Errorf("--max-results -1 emitted a cap hint although nothing was cut:\n%s", tail(out, 6))
			}

			js, _, _ := runCLI(t, append(c.args, "--format", "json")...)
			var payload map[string]interface{}
			if err := json.Unmarshal([]byte(js), &payload); err != nil {
				t.Fatalf("json: %v\n%s", err, js)
			}
			if payload[c.totalField] != float64(1500) {
				t.Errorf("json %s = %v, want 1500", c.totalField, payload[c.totalField])
			}
			if n := len(payload["matches"].([]interface{})); n != 1500 {
				t.Errorf("json listed %d matches, want all 1500", n)
			}
			if _, ok := payload["truncated"]; ok {
				t.Errorf("json carries truncated with nothing cut: %v", payload["truncated"])
			}

			text, _, _ := runCLI(t, append(c.args, "--format", "text")...)
			if !strings.Contains(firstLines(text, 1), "1500") || strings.Contains(firstLines(text, 1), "showing") {
				t.Errorf("text header does not report all 1500 without a showing marker: %q", firstLines(text, 1))
			}
		})
	}
}

// T1 also asks for the flag help to say what -1 does. An undiscoverable
// sentinel is the trap --max-size's help already avoids ("0 = default 70000,
// -1 = no limit"); --max-results must name its own.
func TestSearchMaxResultsHelpStatesNoLimit(t *testing.T) {
	for _, cmd := range []string{"search-files", "search-code"} {
		out, _, _ := runCLI(t, cmd, "--help")
		if !strings.Contains(out, "-1 = no limit") {
			t.Errorf("%s --help does not say -1 means no limit:\n%s", cmd, out)
		}
	}
}

// helpNames reports whether a line of the trailing help block mentions s.
func helpNames(out, s string) bool {
	i := strings.LastIndex(out, "\nhelp[")
	return i >= 0 && strings.Contains(out[i:], s)
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	return strings.Join(lines[:min(n, len(lines))], "\n")
}

func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// Epic 14 T2 at the CLI. Both searches dropped an unreadable file or directory
// with no trace and exited 0, so "1 match" read as the whole answer for a tree
// holding three. What was skipped must reach the caller in every format: the
// skipped field in TOON and JSON, a help line naming each path, and the text
// header. search-files never opens a file, so only the directory is skipped.
func TestSearchReportsWhatItSkipped(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission bits would not be enforced")
	}
	dir := t.TempDir()
	lockedFile := filepath.Join(dir, "locked.txt")
	lockedDir := filepath.Join(dir, "lockeddir")
	for _, p := range []string{filepath.Join(dir, "ok.txt"), lockedFile, filepath.Join(lockedDir, "inner.txt")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("needle\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{lockedFile, lockedDir} {
		if err := os.Chmod(p, 0o000); err != nil {
			t.Skip("cannot remove permissions")
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
	}

	for _, c := range []struct {
		name string
		args []string
		want []string
	}{
		{"search-code", []string{"search-code", "--path", dir, "--pattern", "needle"}, []string{lockedFile, lockedDir}},
		{"search-code no match", []string{"search-code", "--path", dir, "--pattern", "absent"}, []string{lockedFile, lockedDir}},
		{"search-files", []string{"search-files", "--path", dir, "--pattern", "*.txt"}, []string{lockedDir}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, _, code := runCLI(t, c.args...)
			if code != int(goaxi.ExitOK) && code != -1 {
				t.Fatalf("exit = %d\n%s", code, out)
			}
			if !hasTOONField(out, "skipped") || !hasTOONLine(out, "  count: "+fmt.Sprint(len(c.want))) {
				t.Errorf("TOON has no skipped field with count %d:\n%s", len(c.want), out)
			}
			for _, p := range c.want {
				if !helpNames(out, p+": ") {
					t.Errorf("no help line names %s:\n%s", p, tail(out, 3))
				}
			}

			js, _, _ := runCLI(t, append(c.args, "--format", "json")...)
			var payload struct {
				Skipped *struct {
					Count int      `json:"count"`
					Paths []string `json:"paths"`
				} `json:"skipped"`
				NextSteps []string `json:"next_steps"`
			}
			if err := json.Unmarshal([]byte(js), &payload); err != nil {
				t.Fatalf("json: %v\n%s", err, js)
			}
			if payload.Skipped == nil || payload.Skipped.Count != len(c.want) || len(payload.Skipped.Paths) != len(c.want) {
				t.Fatalf("json skipped = %+v, want %d entries", payload.Skipped, len(c.want))
			}
			for i, p := range c.want {
				if !strings.HasPrefix(payload.Skipped.Paths[i], p+": ") {
					t.Errorf("json skipped entry %d = %q, want %s: <reason>", i, payload.Skipped.Paths[i], p)
				}
			}

			text, _, _ := runCLI(t, append(c.args, "--format", "text")...)
			if !strings.Contains(firstLines(text, 1), fmt.Sprintf("%d skipped", len(c.want))) {
				t.Errorf("text header does not say %d skipped: %q", len(c.want), firstLines(text, 1))
			}
			for _, p := range c.want {
				if !strings.Contains(text, p+": ") {
					t.Errorf("text output does not name %s:\n%s", p, text)
				}
			}
		})
	}
}

// The other half: a search that read everything carries no skipped field, no
// help line and no text note, in any format.
func TestSearchCleanTreeHasNoSkippedMarker(t *testing.T) {
	dir := capFixture(t, 3)

	for _, args := range [][]string{
		{"search-code", "--path", dir, "--pattern", "needle"},
		{"search-code", "--path", dir, "--pattern", "absent"},
		{"search-files", "--path", dir, "--pattern", "*.hit"},
	} {
		for _, format := range []string{"toon", "json", "text"} {
			out, _, _ := runCLI(t, append(args, "--format", format)...)
			// Lowercase "skipped" never occurs in the temp path, which carries
			// the test name with a capital S.
			if strings.Contains(out, "skipped") || strings.Contains(out, "Not read") {
				t.Errorf("%v --format %s mentions skipped with nothing skipped:\n%s", args, format, out)
			}
		}
	}
}
