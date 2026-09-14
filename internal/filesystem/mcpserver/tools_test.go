package mcpserver

import (
	"strings"
	"testing"
)

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
