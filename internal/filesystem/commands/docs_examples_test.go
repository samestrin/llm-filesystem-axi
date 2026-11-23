package commands

import (
	"os"
	"regexp"
	"strings"
	"testing"
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

	for _, row := range rows {
		name, example := row[1], row[2]
		t.Run(name, func(t *testing.T) {
			checkExample(t, name, example)
		})
	}
}

// payloadFlags are the JSON flags, each with the strict decoder its command
// uses, so an example's payload is held to what the command accepts.
var payloadFlags = map[string]func(string) error{
	"write-multiple-files/files":       func(s string) error { _, err := decodeWriteEntries(s); return err },
	"edit-blocks/edits":                func(s string) error { _, err := decodeEditPairs(s); return err },
	"edit-multiple-blocks/edits":       func(s string) error { _, err := decodeMultiEdits(s); return err },
	"batch-file-operations/operations": func(s string) error { _, err := decodeBatchOperations(s); return err },
}

// checkExample fails unless example runs the command name, every flag exists
// on it and parses, and every JSON payload passes the command's decoder.
func checkExample(t *testing.T, name, example string) {
	t.Helper()

	words := shellWords(t, example)
	if len(words) < 2 || words[1] != name {
		t.Fatalf("example %q does not run the command %q", example, name)
	}
	root := RootCmd()
	cmd, rest, err := root.Find(words[1:])
	if err != nil || cmd.Name() != name {
		t.Fatalf("%q does not resolve to %s: %v", example, name, err)
	}
	// Global flags (--allowed-dirs, --full) live on the root; merge them as
	// cobra does at execution, or they read as unknown here.
	cmd.Flags().AddFlagSet(root.PersistentFlags())
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatalf("%q: %v", example, err)
	}
	if cmd.Flags().NArg() > 0 {
		t.Fatalf("%q: stray arguments %v", example, cmd.Flags().Args())
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
}

// axi.md review round 2, item 7: AXI principle 10 asks for examples in every
// command's help, and the audit marked it met, but only setup had any. Every
// command must carry at least two, and each must be one the command accepts.
func TestEveryCommandHasExamples(t *testing.T) {
	root := RootCmd()
	checked := 0
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		t.Run(c.Name(), func(t *testing.T) {
			var lines []string
			for _, l := range strings.Split(c.Example, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			if len(lines) < 2 {
				t.Fatalf("%s --help has %d examples, want at least 2", c.Name(), len(lines))
			}
			for _, l := range lines {
				checkExample(t, c.Name(), l)
			}
		})
		checked++
	}
	if checked < 25 {
		t.Fatalf("checked %d commands; the walk is wrong and this test would pass vacuously", checked)
	}
	// A table entry for a command that no longer exists would be dead text.
	for name := range commandExamples {
		if c, _, err := root.Find([]string{name}); err != nil || c.Name() != name {
			t.Errorf("commandExamples names %q, which is not a command", name)
		}
	}
}
