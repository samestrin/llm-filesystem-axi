package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The command lists in integrations/*/AGENTS.md are checked against the binary
// in both directions. The prose around them is not, and that is where the
// documentation kept inventing things.
//
// docs/llm-filesystem-migration.md shipped for months asserting nine response
// fields that have never existed in this codebase - context_before,
// context_after, ripgrep_used, search_time_ms, continuation_token,
// auto_chunked, chunk_index, total_chunks and has_more - alongside a claim that
// search used ripgrep, which it does not. Anyone writing code against that
// document was writing against nothing, and nothing in CI could notice.
//
// This closes that gap for the shape of identifier that actually gets invented:
// a multi-word snake_case name in backticks, which is what a response field, an
// MCP argument or a tool is called here. Single words are deliberately not
// checked - "context", "items" and "total" are also ordinary English, and
// flagging them would bury a real failure in noise.
//
// CHANGELOG.md is excluded on purpose. A changelog describes the past,
// including fields that were removed and fields that were documented but never
// existed, and it has to be able to name them to say so.
func TestDocumentedFieldNamesExist(t *testing.T) {
	known := knownIdentifiers(t)

	// A token that is not an identifier this tool has, but is not a claim
	// about one either. Keep this list short; every entry is a hole.
	allowed := map[string]bool{
		"o200k_base": true, // the tiktoken encoding the benchmark uses
		"next_steps": true, // injected at render time, so it carries no struct tag
	}

	backticked := regexp.MustCompile("`([a-z][a-z0-9]*(?:_[a-z0-9]+)+)`")

	type claim struct {
		token string
		file  string
	}
	var bad []claim

	for _, doc := range referenceDocs(t) {
		body, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		for _, m := range backticked.FindAllStringSubmatch(string(body), -1) {
			tok := m[1]
			if known[tok] || allowed[tok] {
				continue
			}
			bad = append(bad, claim{tok, doc})
		}
	}

	sort.Slice(bad, func(i, j int) bool { return bad[i].token < bad[j].token })
	for _, c := range bad {
		t.Errorf("%s names `%s`, which is not a field, MCP argument, tool or command this tool has — "+
			"a reader writing code against it gets nothing", c.file, c.token)
	}
}

// referenceDocs is everything a user or agent is expected to follow. CHANGELOG
// is not here; see the comment on TestDocumentedFieldNamesExist.
func referenceDocs(t *testing.T) []string {
	t.Helper()

	var docs []string
	add := func(pattern string) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("globbing %s: %v", pattern, err)
		}
		docs = append(docs, matches...)
	}
	add("../../../README.md")
	add("../../../AGENTS.md")
	add("../../../docs/*.md")
	add("../../../integrations/*/AGENTS.md")

	if len(docs) == 0 {
		t.Fatal("found no reference documentation; the paths above are wrong")
	}
	return docs
}

// knownIdentifiers is every snake_case name this tool actually answers to:
// struct tags it emits, arguments the MCP server reads, tool names it exposes,
// and its own command names.
//
// These are read out of the source rather than listed here, so the set cannot
// drift the way a hand-maintained list would - which is the failure this whole
// file exists to prevent.
func knownIdentifiers(t *testing.T) map[string]bool {
	t.Helper()

	known := map[string]bool{}

	jsonTag := regexp.MustCompile(`json:"([a-z0-9_]+)`)
	mcpArg := regexp.MustCompile(`get(?:Int|String|Bool|Float)\(args,\s*"([a-z0-9_]+)"`)
	toolName := regexp.MustCompile(`ToolPrefix \+ "([a-z0-9_]+)"`)

	root := "../.."
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, re := range []*regexp.Regexp{jsonTag, mcpArg} {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				known[m[1]] = true
			}
		}
		for _, m := range toolName.FindAllStringSubmatch(text, -1) {
			known[m[1]] = true                   // bare, as docs/ writes it
			known["llm_filesystem_"+m[1]] = true // prefixed, as integrations/mcp writes it
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning source: %v", err)
	}

	// Command names, from the binary rather than the source.
	for _, c := range RootCmd().Commands() {
		known[strings.ReplaceAll(c.Name(), "-", "_")] = true
	}

	if len(known) < 50 {
		t.Fatalf("only %d identifiers found; the source scan is broken and this test would pass vacuously", len(known))
	}
	return known
}
