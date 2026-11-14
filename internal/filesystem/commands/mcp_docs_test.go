package commands

import (
	"os"
	"strings"
	"testing"
)

// Epic 09 shipped a thin MCP server (llm-filesystem-mcp) beside the CLI, but
// the docs still said "there is no MCP server" in four places and told users
// to delete the "leftover" server binary the installer now installs. A reader
// following those docs would uninstall the server or conclude it does not
// exist. These tests pin the docs to the shipped reality, the same way the
// command-count and command-table guards pin the rest.

// mcpDenial reports whether body contains the phrase the epic removed,
// case-insensitively, matching `git grep -i "no MCP server"`.
func mcpDenial(body string) bool {
	return strings.Contains(strings.ToLower(body), "no mcp server")
}

// readmeOutsidePerformance returns the README with its Performance section
// removed. That section carries the dated v1.0.2 benchmark table, whose
// "not applicable: a CLI has no MCP server" row is a measurement of the old
// release and is kept on purpose (technical-debt row 80a48d48da0f7e20).
// Everywhere else in the README the phrase is a stale claim.
func readmeOutsidePerformance(body string) string {
	lines := strings.Split(body, "\n")
	var out []string
	inPerf := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "## Performance" {
			inPerf = true
			continue
		}
		if inPerf && strings.HasPrefix(line, "## ") {
			inPerf = false
		}
		if !inPerf {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// Defends epic 09 T4's "Done when": git grep -i "no MCP server" over VISION.md,
// docs/llm-filesystem-commands.md, docs/axi-audit.md and
// docs/axi-catalog-entry.yaml finds nothing, and the README has no such
// sentence outside its dated v1.0.2 benchmark table. docs/background.md is
// included because the EST-6459 review note found the same stale sentence
// there.
func TestNamedDocsNoLongerDenyTheMCPServer(t *testing.T) {
	for _, doc := range []string{
		"../../../VISION.md",
		"../../../docs/llm-filesystem-commands.md",
		"../../../docs/axi-audit.md",
		"../../../docs/axi-catalog-entry.yaml",
		"../../../docs/background.md",
	} {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		if mcpDenial(string(body)) {
			t.Errorf("%s still says \"no MCP server\"; the server ships beside the CLI since epic 09", doc)
		}
	}

	body, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}
	if mcpDenial(readmeOutsidePerformance(string(body))) {
		t.Error("README says \"no MCP server\" outside the dated v1.0.2 benchmark table; only that table may keep the phrase")
	}
}

// The denial guard must be able to fail, and the Performance exemption must
// exempt only the Performance section: a stale sentence anywhere else in the
// README is still caught, and a clean doc is never flagged.
func TestDenyGuardFlagsTheOldWording(t *testing.T) {
	if !mcpDenial("There is no MCP server; an agent that can run a command can run this.") {
		t.Error("the old sentence was not flagged")
	}
	if !mcpDenial("there is NO MCP SERVER here") {
		t.Error("the check is not case-insensitive")
	}
	if mcpDenial("A thin MCP server, llm-filesystem-mcp, ships beside the CLI.") {
		t.Error("a doc describing the server was flagged")
	}

	kept := "## Performance\n\n| MCP handshake | not applicable: a CLI has no MCP server |\n\n## Limits\n"
	if mcpDenial(readmeOutsidePerformance(kept)) {
		t.Error("the dated benchmark row inside Performance was flagged; it is kept on purpose")
	}
	moved := "## Limits\n\nThere is no MCP server.\n\n## Performance\n\n| MCP handshake | not applicable: a CLI has no MCP server |\n"
	if !mcpDenial(readmeOutsidePerformance(moved)) {
		t.Error("the same sentence outside Performance was not flagged; the exemption is too wide")
	}
	if !mcpDenial(readmeOutsidePerformance("## Install\n\nThere is no MCP server.\n")) {
		t.Error("a README without a Performance section was not checked at all")
	}
}

// Defends epic 09 T4's README requirement: the install and usage sections
// show the MCP server, with a client config snippet for Claude Code, Codex
// and OpenCode in each client's own format. The markers below are the keys
// that identify those formats (mcpServers JSON, [mcp_servers.<name>] TOML,
// and OpenCode's "type": "local" entry), so a snippet copied from the wrong
// client's docs fails here. Also defends the EST-6459 review note: the
// install section must not tell users to remove a "leftover" server the
// installer now installs, and the server's two env vars must be documented.
func TestReadmeInstallAndUsageShowTheMCPServer(t *testing.T) {
	body, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README: %v", err)
	}

	install, ok := section(string(body), "Install")
	if !ok {
		t.Fatal("README has no ## Install section")
	}
	if !strings.Contains(install, "llm-filesystem-mcp") {
		t.Error("README Install section does not mention llm-filesystem-mcp; the installer installs it beside the CLI")
	}
	if strings.Contains(install, "removed MCP server") {
		t.Error("README Install still calls the server \"removed\" and tells users to delete the binary the installer now installs")
	}

	usage, ok := section(string(body), "Usage")
	if !ok {
		t.Fatal("README has no ## Usage section")
	}
	for _, want := range []struct {
		client string
		marker string
	}{
		{"Claude Code", "mcpServers"},
		{"Codex", "[mcp_servers."},
		{"OpenCode", `"type": "local"`},
	} {
		if !strings.Contains(usage, want.client) {
			t.Errorf("README Usage section has no %s config snippet", want.client)
		}
		if !strings.Contains(usage, want.marker) {
			t.Errorf("README Usage section names %s but not its config marker %q; the snippet is missing or in another client's format", want.client, want.marker)
		}
	}
	if !strings.Contains(usage, "llm-filesystem-mcp") {
		t.Error("README Usage snippets do not run llm-filesystem-mcp")
	}

	config, ok := section(string(body), "Configuration")
	if !ok {
		t.Fatal("README has no ## Configuration section")
	}
	for _, env := range []string{"LLM_FILESYSTEM_BINARY", "LLM_FILESYSTEM_MCP_TIMEOUT"} {
		if !strings.Contains(config, env) {
			t.Errorf("README Configuration section does not document %s, the MCP server's own setting", env)
		}
	}
}

// Defends the VISION.md amendment the owner chose (epic 09 refine Q1): the
// CLI is the primary surface and the thin MCP server a sibling fallback, no
// longer "the only surface".
func TestVisionPresentsTheServerAsAFallback(t *testing.T) {
	body, err := os.ReadFile("../../../VISION.md")
	if err != nil {
		t.Fatalf("reading VISION.md: %v", err)
	}
	if strings.Contains(string(body), "the only surface") {
		t.Error("VISION.md still calls the CLI \"the only surface\"; the MCP server ships beside it")
	}
	if !strings.Contains(string(body), "llm-filesystem-mcp") {
		t.Error("VISION.md does not name llm-filesystem-mcp as the sibling fallback")
	}
}

// Defends the replacement of docs/llm-filesystem-commands.md's ## MCP
// section: it must describe the server that ships, including the two
// settings an operator needs to point it at a CLI or bound a call.
func TestCommandsDocMCPSectionDescribesTheServer(t *testing.T) {
	body, err := os.ReadFile("../../../docs/llm-filesystem-commands.md")
	if err != nil {
		t.Fatalf("reading docs/llm-filesystem-commands.md: %v", err)
	}
	mcpSection, ok := section(string(body), "MCP")
	if !ok {
		t.Fatal("docs/llm-filesystem-commands.md has no ## MCP section")
	}
	for _, want := range []string{"llm-filesystem-mcp", "LLM_FILESYSTEM_BINARY", "LLM_FILESYSTEM_MCP_TIMEOUT"} {
		if !strings.Contains(mcpSection, want) {
			t.Errorf("the ## MCP section does not name %s", want)
		}
	}
}

// Defends epic 09 T4's requirement that docs/axi-audit.md and
// docs/axi-catalog-entry.yaml describe the server: a catalog reader who
// never opens the README must still learn the tool has an MCP surface.
func TestAuditAndCatalogDescribeTheServer(t *testing.T) {
	for _, doc := range []string{
		"../../../docs/axi-audit.md",
		"../../../docs/axi-catalog-entry.yaml",
	} {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		if !strings.Contains(string(body), "llm-filesystem-mcp") {
			t.Errorf("%s does not describe llm-filesystem-mcp", doc)
		}
	}
}
