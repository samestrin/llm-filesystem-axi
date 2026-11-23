package commands

import (
	"strings"

	"github.com/spf13/cobra"
)

// commandExamples are the Examples block of each command's --help (AXI
// principle 10). Only setup had one, while the audit marked the principle met.
//
// Kept as one table rather than a field in each constructor, so the set is
// readable in one place. TestEveryCommandHasExamples requires an entry for
// every command and parses each line against the command's real flags and,
// for a JSON flag, the strict decoder the command uses: an example that would
// be refused fails the build rather than misleading the agent that copies it.
var commandExamples = map[string][]string{
	"read-file": {
		"llm-filesystem read-file --path main.go",
		"llm-filesystem read-file --path app.log --line-start 200 --line-count 50",
		"llm-filesystem read-file --path big.json --full",
	},
	"read-multiple-files": {
		"llm-filesystem read-multiple-files --paths go.mod,main.go",
		"llm-filesystem read-multiple-files --paths a.go,b.go --max-total-size -1",
	},
	"extract-lines": {
		"llm-filesystem extract-lines --path main.go --start 10 --end 20",
		"llm-filesystem extract-lines --path app.log --pattern ERROR --context 2",
		"llm-filesystem extract-lines --path main.go --lines 1,5,9",
	},
	"write-file": {
		`llm-filesystem write-file --path notes.txt --content "hello"`,
		`llm-filesystem write-file --path app.log --content "one more line" --append`,
	},
	"write-multiple-files": {
		`llm-filesystem write-multiple-files --files '[{"path":"a.txt","content":"x"},{"path":"b/c.txt","content":"y"}]'`,
		`llm-filesystem write-multiple-files --files '[{"path":"empty.txt","content":""}]'`,
	},
	"large-write-file": {
		`llm-filesystem large-write-file --path data.csv --content "a,b,c"`,
		`llm-filesystem large-write-file --path data.csv --content "d,e,f" --append --backup=false`,
	},
	"get-file-info": {
		"llm-filesystem get-file-info --path main.go",
		"llm-filesystem get-file-info --path src",
	},
	"create-directory": {
		"llm-filesystem create-directory --path build/out",
		"llm-filesystem create-directory --path logs --recursive=false",
	},
	"edit-block": {
		`llm-filesystem edit-block --path main.go --old "foo()" --new "bar()"`,
		`llm-filesystem edit-block --path main.go --old "oldName" --new "newName" --replace-all`,
	},
	"edit-blocks": {
		`llm-filesystem edit-blocks --path main.go --edits '[{"old_string":"foo","new_string":"bar"},{"old_string":"baz","new_string":"qux"}]'`,
		`llm-filesystem edit-blocks --path main.go --edits '[{"old_string":"// TODO remove\n","new_string":""}]'`,
	},
	"edit-multiple-blocks": {
		`llm-filesystem edit-multiple-blocks --path main.go --edits '[{"old_text":"foo","new_text":"bar"},{"mode":"insert_after","line_number":3,"new_text":"// added"}]'`,
		`llm-filesystem edit-multiple-blocks --path main.go --edits '[{"mode":"delete_line","line_number":10}]' --backup=false`,
	},
	"safe-edit": {
		`llm-filesystem safe-edit --path config.yml --old "debug: false" --new "debug: true" --dry-run`,
		`llm-filesystem safe-edit --path config.yml --old "debug: false" --new "debug: true" --backup=false`,
	},
	"edit-file": {
		`llm-filesystem edit-file --path main.go --operation insert --line 5 --content "// note"`,
		`llm-filesystem edit-file --path main.go --operation replace --line 5 --content "// changed"`,
		"llm-filesystem edit-file --path main.go --operation delete --line 5",
	},
	"search-and-replace": {
		`llm-filesystem search-and-replace --path ./src --pattern "oldName" --replacement "newName" --dry-run`,
		`llm-filesystem search-and-replace --path ./src --pattern "v[0-9]+" --replacement "v2" --regex --file-types .go`,
	},
	"list-directory": {
		"llm-filesystem list-directory --path .",
		"llm-filesystem list-directory --path src --sort-by size --reverse --page-size 20",
		`llm-filesystem list-directory --path . --pattern "*.go" --show-hidden`,
	},
	"get-directory-tree": {
		"llm-filesystem get-directory-tree --path . --depth 2",
		`llm-filesystem get-directory-tree --path src --include-files --pattern "*.go"`,
	},
	"search-files": {
		`llm-filesystem search-files --path ./src --pattern "*.go"`,
		`llm-filesystem search-files --path . --pattern "config" --recursive=false --show-hidden`,
	},
	"search-code": {
		`llm-filesystem search-code --path ./src --pattern "TODO"`,
		`llm-filesystem search-code --path . --pattern "func [A-Z]" --regex --file-types .go --context 2`,
		`llm-filesystem search-code --path . --pattern "error" --ignore-case --max-results 50`,
	},
	"copy-file": {
		"llm-filesystem copy-file --source a.txt --dest b.txt",
		"llm-filesystem copy-file --source src --dest backup/src --confirm",
	},
	"move-file": {
		"llm-filesystem move-file --source old.txt --dest new.txt",
		"llm-filesystem move-file --source draft.md --dest docs/final.md --confirm",
	},
	"delete-file": {
		"llm-filesystem delete-file --path old.txt --confirm",
		"llm-filesystem delete-file --path build --recursive --confirm",
	},
	"batch-file-operations": {
		`llm-filesystem batch-file-operations --operations '[{"operation":"copy","source":"a.txt","destination":"b.txt"},{"operation":"move","source":"c.txt","destination":"d/c.txt"}]'`,
		`llm-filesystem batch-file-operations --operations '[{"operation":"delete","source":"tmp","recursive":true}]' --confirm`,
	},
	"get-disk-usage": {
		"llm-filesystem get-disk-usage --path .",
		"llm-filesystem get-disk-usage --path node_modules",
	},
	"find-large-files": {
		"llm-filesystem find-large-files --path . --min-size 1048576",
		"llm-filesystem find-large-files --path . --min-size 10485760 --limit 10",
	},
	"compress-files": {
		"llm-filesystem compress-files --paths src,README.md --output release.zip",
		"llm-filesystem compress-files --paths logs --output logs.tar.gz",
		"llm-filesystem compress-files --paths logs --output logs.bin --archive-format tgz",
	},
	"extract-archive": {
		"llm-filesystem extract-archive --archive release.zip --dest out",
		"llm-filesystem extract-archive --archive big.tar.gz --dest out --max-bytes -1",
	},
	"sync-directories": {
		"llm-filesystem sync-directories --source src --dest backup --dry-run",
		"llm-filesystem sync-directories --source src --dest backup",
	},
	"list-allowed-directories": {
		"llm-filesystem list-allowed-directories",
		"llm-filesystem list-allowed-directories --allowed-dirs ~/project,/tmp",
	},
	"setup": {
		"llm-filesystem setup --dry-run",
		"llm-filesystem setup",
	},
}

// applyExamples sets each command's Example from commandExamples.
func applyExamples(root *cobra.Command) {
	for _, c := range root.Commands() {
		if ex, ok := commandExamples[c.Name()]; ok {
			c.Example = "  " + strings.Join(ex, "\n  ")
		}
	}
}
