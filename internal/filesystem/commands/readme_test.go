package commands

import (
	"os"
	"strings"
	"testing"
)

const (
	readmePath      = "../../../README.md"
	perfResultsPath = "../../../benchmarks/results-performance.md"
)

// readmeSections is the README standard's top-level order. A README may leave
// a section out, but every `## ` heading must be one of these and they must
// appear in this order. The old README grew "Support" and "Why Go" sections
// with no slot in the standard; this keeps the layout from drifting again.
var readmeSections = []string{
	"Demo",
	"Why",
	"Install",
	"Quick start",
	"Usage",
	"Configuration",
	"Output and exit codes",
	"Performance",
	"How it works",
	"Limits",
	"Documentation",
	"Development",
	"Changelog",
	"License",
}

// h2Headings returns the `## ` headings of a markdown body, skipping fenced
// code blocks so a shell comment such as "## build" is not read as a heading.
func h2Headings(body string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "## ")))
		}
	}
	return out
}

// sectionOrderProblems returns one message per heading that is not in the
// standard or that appears out of the standard's order.
func sectionOrderProblems(headings []string) []string {
	rank := map[string]int{}
	for i, s := range readmeSections {
		rank[s] = i
	}
	var out []string
	last := -1
	for _, h := range headings {
		r, ok := rank[h]
		if !ok {
			out = append(out, "README has a section \""+h+"\" the standard does not list")
			continue
		}
		if r <= last {
			out = append(out, "README section \""+h+"\" is out of the standard's order")
		}
		last = r
	}
	return out
}

// Defends acceptance criterion 1 of the README epic: the standard's sections,
// in its order, with nothing added. Reading the headings by eye is how the old
// README ended up with Support between Install and Usage.
func TestReadmeSectionsFollowTheStandard(t *testing.T) {
	body, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}
	headings := h2Headings(string(body))
	if len(headings) == 0 {
		t.Fatal("README has no ## headings; the parser or the path is wrong")
	}
	for _, msg := range sectionOrderProblems(headings) {
		t.Error(msg)
	}
}

// The order check must be able to fail, or the test above proves nothing.
func TestSectionOrderCheckFlagsBadLayouts(t *testing.T) {
	if p := sectionOrderProblems([]string{"Install", "Support", "Usage"}); len(p) != 1 {
		t.Errorf("an unlisted section gave %d problems, want 1: %v", len(p), p)
	}
	if p := sectionOrderProblems([]string{"Usage", "Install"}); len(p) != 1 {
		t.Errorf("a reversed pair gave %d problems, want 1: %v", len(p), p)
	}
	if p := sectionOrderProblems([]string{"Install", "Install"}); len(p) != 1 {
		t.Errorf("a repeated section gave %d problems, want 1: %v", len(p), p)
	}
	if p := sectionOrderProblems(readmeSections); len(p) != 0 {
		t.Errorf("the standard's own order gave problems: %v", p)
	}
	if h := h2Headings("```\n## not a heading\n```\n## Install\n"); len(h) != 1 || h[0] != "Install" {
		t.Errorf("h2Headings read a fenced line as a heading: %v", h)
	}
}

// section returns the body of the `## name` section, up to the next `## `.
func section(body, name string) (string, bool) {
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## "+name {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), true
}

// copiedPerfLines returns the lines of the README's Performance section that
// carry measured data (table rows and the stamp bullets) and do not appear
// verbatim in the generated results file.
func copiedPerfLines(readmeSection, results string) (checked int, missing []string) {
	have := map[string]bool{}
	for _, line := range strings.Split(results, "\n") {
		have[strings.TrimSpace(line)] = true
	}
	for _, line := range strings.Split(readmeSection, "\n") {
		line = strings.TrimSpace(line)
		isRow := strings.HasPrefix(line, "|")
		isStamp := strings.HasPrefix(line, "- **") && !strings.HasPrefix(line, "- **Raw results")
		if !isRow && !isStamp {
			continue
		}
		checked++
		if !have[line] {
			missing = append(missing, line)
		}
	}
	return checked, missing
}

// Defends acceptance criterion 7: no number in the README is typed by hand.
// The old README quoted 78x, 103x and 29x from a run nobody could reproduce
// from the repo. Every table row and stamp line under Performance must be a
// verbatim copy of benchmarks/results-performance.md, which the benchmark
// script generates, so a re-run that changes a number fails here until the
// README is re-copied.
func TestReadmePerformanceIsCopiedFromResults(t *testing.T) {
	body, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}
	results, err := os.ReadFile(perfResultsPath)
	if err != nil {
		t.Fatalf("reading results: %v", err)
	}
	perf, ok := section(string(body), "Performance")
	if !ok {
		t.Fatal("README has no ## Performance section")
	}
	checked, missing := copiedPerfLines(perf, string(results))
	if checked < 5 {
		t.Errorf("README Performance has %d table or stamp lines; want the table and the tools, date, machine and method lines", checked)
	}
	for _, line := range missing {
		t.Errorf("README Performance line is not in %s: %s", perfResultsPath, line)
	}
}

// The copy check must be able to fail: a hand-edited number is caught.
func TestPerfCopyCheckFlagsAnEditedNumber(t *testing.T) {
	results := "| Cold start | 1.79 ms | 151.0 ms |\n- **Date:** 2026-11-11 (UTC)\n"
	if _, missing := copiedPerfLines(results, results); len(missing) != 0 {
		t.Errorf("an exact copy was flagged: %v", missing)
	}
	edited := "| Cold start | 1.7 ms | 151.0 ms |\n- **Date:** 2026-11-11 (UTC)\n"
	if _, missing := copiedPerfLines(edited, results); len(missing) != 1 {
		t.Errorf("an edited number gave %d problems, want 1: %v", len(missing), missing)
	}
}
