package commands

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
)

const commandsDoc = "../../../docs/llm-filesystem-commands.md"

// exampleRowRe matches a command-table row whose last cell is an example:
// | `name` | description | `llm-filesystem name ...` |
var exampleRowRe = regexp.MustCompile("(?m)^\\|\\s*`([a-z][a-z-]+)`\\s*\\|.*\\|\\s*`(llm-filesystem [^`]*)`\\s*\\|\\s*$")

// shellWords splits an example the way a POSIX shell would for the quoting
// the docs use: single quotes, double quotes and plain words.
func shellWords(t *testing.T, s string) []string {
	t.Helper()

	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' || c == '"':
			end := strings.IndexByte(s[i+1:], c)
			if end < 0 {
				t.Fatalf("unbalanced %c in %q", c, s)
			}
			cur.WriteString(s[i+1 : i+1+end])
			i += end + 1
			inWord = true
		case c == ' ':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

// axi.md review round 1, item 8: the doc said every example was checked
// against the binary, but nothing ran them, and `find-large-files --min-size
// "1MB"` exits 2 because the flag takes bytes. docs/ also once shipped an
// edit-blocks payload with wrong keys that silently did nothing.
//
// Running the examples would need a fixture for every path they name, so this
// checks what goes wrong in practice: every flag exists on that command and its
// value parses, and every JSON payload passes the strict decoder the command
// itself uses.
func TestCommandsDocExamplesParse(t *testing.T) {
	body, err := os.ReadFile(commandsDoc)
	if err != nil {
		t.Fatalf("reading %s: %v", commandsDoc, err)
	}
	rows := exampleRowRe.FindAllStringSubmatch(string(body), -1)
	if len(rows) < 25 {
		t.Fatalf("found %d example rows in %s; the row pattern is wrong and this test would pass vacuously", len(rows), commandsDoc)
	}

	payloadFlags := map[string]func(string) error{
		"write-multiple-files/files": func(s string) error { _, err := decodeWriteEntries(s); return err },
		"edit-blocks/edits":          func(s string) error { _, err := decodeEditPairs(s); return err },
		"edit-multiple-blocks/edits": func(s string) error { _, err := decodeMultiEdits(s); return err },
		"batch-file-operations/operations": func(s string) error {
			var ops []core.BatchOperation
			return decodePayload(s, &ops)
		},
	}

	for _, row := range rows {
		name, example := row[1], row[2]
		t.Run(name, func(t *testing.T) {
			words := shellWords(t, example)
			if len(words) < 2 || words[1] != name {
				t.Fatalf("example %q does not run the row's command %q", example, name)
			}
			root := RootCmd()
			cmd, rest, err := root.Find(words[1:])
			if err != nil || cmd.Name() != name {
				t.Fatalf("%q does not resolve to %s: %v", example, name, err)
			}
			if err := cmd.ParseFlags(rest); err != nil {
				t.Fatalf("%q: %v", example, err)
			}
			for key, decode := range payloadFlags {
				cmdName, flag, _ := strings.Cut(key, "/")
				if cmdName != name {
					continue
				}
				f := cmd.Flags().Lookup(flag)
				if f == nil || !f.Changed {
					t.Fatalf("%q does not show --%s", example, flag)
				}
				if err := decode(f.Value.String()); err != nil {
					t.Errorf("%q: --%s payload is refused: %v", example, flag, err)
				}
			}
		})
	}
}
