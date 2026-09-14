package mcpserver

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The MCP routing table must match the served tools EXACTLY, in both
// directions, for the same reason the CLI one must match the binary: a missing
// entry tells the agent a capability does not exist, and a stale entry sends it
// at a tool that is not there.
//
// This file is separate from the CLI guidance because the advice genuinely
// differs. An MCP client passes JSON arguments and cannot use --confirm or
// --start-offset at all, so flag-based instructions are not merely unhelpful to
// it, they are wrong.
func TestEveryMCPToolIsDocumentedForAgents(t *testing.T) {
	const doc = "../../../integrations/mcp/AGENTS.md"

	body, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("reading %s: %v", doc, err)
	}

	documented := map[string]bool{}
	rows := regexp.MustCompile("(?m)^\\|\\s*`([a-z][a-z_]+)`\\s*\\|").FindAllSubmatch(body, -1)
	for _, m := range rows {
		documented[string(m[1])] = true
	}
	if len(documented) == 0 {
		t.Fatalf("%s lists no tools; rows must be shaped | `name` | description |", doc)
	}

	actual := map[string]bool{}
	for _, d := range GetToolDefinitions() {
		actual[d.Name] = true
	}

	for name := range actual {
		if !documented[name] {
			t.Errorf("%s does not list %q — an agent reading it concludes the tool is absent", doc, name)
		}
	}
	for name := range documented {
		if !actual[name] {
			t.Errorf("%s lists %q, which the server does not serve", doc, name)
		}
	}
}

// An MCP client never reads CLAUDE.md, and never runs --help. Anything an agent
// must know to use a tool CORRECTLY therefore has to live in the tool's own
// description, or it does not reach that agent at all.
//
// Two CLI behaviours are surprising enough that omitting them is a trap rather
// than a gap.

// A read over the budget returns a PREFIX, not the whole file. An agent that
// does not know this reasons over partial content believing it complete — which
// is the failure this project exists to remove, arriving through the other door.
func TestReadToolDescriptionsMentionTruncation(t *testing.T) {
	missing := map[string]bool{
		ToolPrefix + "read_file":           true,
		ToolPrefix + "read_multiple_files": true,
	}

	for _, d := range GetToolDefinitions() {
		if !missing[d.Name] {
			continue
		}
		delete(missing, d.Name)

		if !strings.Contains(strings.ToLower(d.Description), "truncat") {
			t.Errorf("%s does not tell the caller its reads can truncate: %q",
				d.Name, d.Description)
		}
	}

	for name := range missing {
		t.Errorf("%s is not in the tool definitions; this test is guarding nothing", name)
	}
}

// Deletion is gated on the CLI, and the server supplies the confirmation itself.
// That is the right design — the tool call IS the confirmation — but it means an
// agent is destroying files through a door it was never told was locked. Saying
// so is the difference between a safeguard and a surprise.
func TestDestructiveToolDescriptionsMentionConfirmation(t *testing.T) {
	missing := map[string]bool{
		ToolPrefix + "delete_file":           true,
		ToolPrefix + "batch_file_operations": true,
	}

	for _, d := range GetToolDefinitions() {
		if !missing[d.Name] {
			continue
		}
		delete(missing, d.Name)

		if !strings.Contains(strings.ToLower(d.Description), "confirm") {
			t.Errorf("%s does not say the operation is confirmation-gated: %q",
				d.Name, d.Description)
		}
	}

	for name := range missing {
		t.Errorf("%s is not in the tool definitions; this test is guarding nothing", name)
	}
}
