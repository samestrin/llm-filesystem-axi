package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// axi.md review round 2, item 1: an edit with no new_string or new_text
// decoded the missing key to "" and DELETED the matched text, exiting 0.
// `edit-blocks --edits '[{"old_string":"src"}]'` rewrote "src\n" to "\n", and
// an insert_after with no new_text inserted an empty line. A replacement that
// was not given is a malformed invocation: exit 2, file untouched.
func TestEditsWithoutReplacementAreRefused(t *testing.T) {
	const precious = "alpha\nbeta\n"

	cases := []struct {
		name, cmd, edits string
	}{
		{"edit-blocks no new_string", "edit-blocks", `[{"old_string":"alpha"}]`},
		{"edit-blocks second edit no new_string", "edit-blocks", `[{"old_string":"alpha","new_string":"A"},{"old_string":"beta"}]`},
		{"edit-multiple-blocks replace no new_text", "edit-multiple-blocks", `[{"old_text":"alpha"}]`},
		{"edit-multiple-blocks insert_after no new_text", "edit-multiple-blocks", `[{"mode":"insert_after","line_number":1}]`},
		{"edit-multiple-blocks insert_before no new_text", "edit-multiple-blocks", `[{"mode":"insert_before","line_number":1}]`},
		// Item 8: an unknown mode was attempted and reported "none of 1
		// edits applied" with exit 1, as if the tool had tried and broken.
		{"edit-multiple-blocks unknown mode", "edit-multiple-blocks", `[{"mode":"append","new_text":"x"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keep := writeKeep(t, precious)

			stdout, _, code := runCLI(t, tc.cmd, "--path", keep, "--edits", tc.edits)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d): %q", code, goaxi.ExitUsage, stdout)
			}
			if !strings.Contains(stdout, "invalid edits JSON") {
				t.Errorf("stdout = %q, want the payload refusal", stdout)
			}
			assertUntouched(t, keep, precious)
		})
	}

	// The fix must not refuse what was asked for by name: an explicit empty
	// replacement deletes, and delete_line needs no text at all.
	for _, tc := range []struct {
		name, cmd, edits, want string
	}{
		{"edit-blocks explicit empty new_string", "edit-blocks", `[{"old_string":"alpha","new_string":""}]`, "\nbeta\n"},
		{"edit-multiple-blocks explicit empty new_text", "edit-multiple-blocks", `[{"old_text":"alpha","new_text":""}]`, "\nbeta\n"},
		{"edit-multiple-blocks delete_line", "edit-multiple-blocks", `[{"mode":"delete_line","line_number":1}]`, "beta\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keep := writeKeep(t, precious)

			args := []string{tc.cmd, "--path", keep, "--edits", tc.edits}
			if tc.cmd == "edit-multiple-blocks" {
				args = append(args, "--backup=false")
			}
			stdout, _, code := runCLI(t, args...)

			if code != -1 {
				t.Errorf("exit = %d, want 0: %q", code, stdout)
			}
			assertUntouched(t, keep, tc.want)
		})
	}
}

// Item 2: batch-file-operations refused only unknown keys up front. A missing
// key ran: an entry with no "operation" failed as "unknown operation:", and a
// valid entry followed by a copy with no destination RAN THE FIRST ENTRY, then
// failed, leaving a partial change. A copy with no destination and no
// --confirm got "overwrites an existing file", because the empty destination
// normalized to the working directory. Every entry is now checked before any
// runs, and the refusal is a usage error.
func TestBatchValidatesEveryEntryBeforeRunning(t *testing.T) {
	cases := []struct {
		name, bad, want string
		confirm         bool
	}{
		{"missing operation", `{"source":"SRC","destination":"OUT2"}`, "operation 2: operation is required", true},
		{"unknown operation", `{"operation":"frobnicate","source":"SRC"}`, `operation 2: unknown operation \"frobnicate\"`, true},
		{"missing source", `{"operation":"delete"}`, "operation 2: source is required", true},
		{"copy missing destination", `{"operation":"copy","source":"SRC"}`, "operation 2: destination is required for copy", true},
		{"move missing destination", `{"operation":"move","source":"SRC"}`, "operation 2: destination is required for move", true},
		{"copy missing destination unconfirmed", `{"operation":"copy","source":"SRC"}`, "operation 2: destination is required for copy", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "s.txt")
			if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			out1 := filepath.Join(dir, "m1.txt")
			bad := strings.NewReplacer("SRC", src, "OUT2", filepath.Join(dir, "m2.txt")).Replace(tc.bad)
			ops := `[{"operation":"copy","source":"` + src + `","destination":"` + out1 + `"},` + bad + `]`

			args := []string{"batch-file-operations", "--operations", ops}
			if tc.confirm {
				args = append(args, "--confirm")
			}
			stdout, _, code := runCLI(t, args...)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d): %q", code, goaxi.ExitUsage, stdout)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("stdout = %q, want %q", stdout, tc.want)
			}
			// The side effect: the valid first entry must not have run.
			if _, err := os.Stat(out1); err == nil {
				t.Error("the first entry ran before the batch was refused")
			}
			if _, err := os.Stat(src); err != nil {
				t.Errorf("source was touched: %v", err)
			}
		})
	}

	// A delete needs no destination; the check must not invent one.
	t.Run("delete without destination still runs", func(t *testing.T) {
		victim := writeKeep(t, "x")

		stdout, _, code := runCLI(t, "batch-file-operations", "--confirm", "--operations",
			`[{"operation":"delete","source":"`+victim+`"}]`)

		if code != -1 {
			t.Errorf("exit = %d, want 0: %q", code, stdout)
		}
		if _, err := os.Stat(victim); err == nil {
			t.Error("the delete did not run")
		}
	})
}

// Item 3: destinationExists used os.Stat, which follows a symlink, so a
// dangling symlink at the destination read as "nothing there" and was replaced
// by a regular file with exit 0 and no --confirm. delete-file already uses
// Lstat for the same reason. Each command that shares the gate is tested.
func TestDanglingSymlinkDestinationNeedsConfirm(t *testing.T) {
	setup := func(t *testing.T) (src, link string) {
		t.Helper()
		dir := t.TempDir()
		src = filepath.Join(dir, "src.txt")
		if err := os.WriteFile(src, []byte("new"), 0o600); err != nil {
			t.Fatal(err)
		}
		link = filepath.Join(dir, "link")
		if err := os.Symlink(filepath.Join(dir, "gone"), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		return src, link
	}

	for _, name := range []string{"copy-file", "move-file", "batch copy", "batch move"} {
		t.Run(name, func(t *testing.T) {
			src, link := setup(t)

			var args []string
			switch name {
			case "copy-file", "move-file":
				args = []string{name, "--source", src, "--dest", link}
			default:
				op := strings.TrimPrefix(name, "batch ")
				args = []string{"batch-file-operations", "--operations",
					`[{"operation":"` + op + `","source":"` + src + `","destination":"` + link + `"}]`}
			}
			stdout, _, code := runCLI(t, args...)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d): %q", code, goaxi.ExitUsage, stdout)
			}
			if !strings.Contains(stdout, "--confirm") {
				t.Errorf("stdout = %q, want the --confirm refusal", stdout)
			}
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("the symlink was replaced (err %v)", err)
			}
			if _, err := os.Stat(src); err != nil {
				t.Errorf("source was moved: %v", err)
			}
		})
	}
}

// Item 4: a run with failed entries exited 1 but printed no help[], so the
// caller had no next step. compress-files names what it left out; these three
// now name the failed entries and how to retry only those.
func TestFailedEntriesNameARetry(t *testing.T) {
	t.Run("read-multiple-files", func(t *testing.T) {
		dir := t.TempDir()
		ok := filepath.Join(dir, "ok.txt")
		if err := os.WriteFile(ok, []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		missing := filepath.Join(dir, "missing.txt")

		stdout, _, code := runCLI(t, "read-multiple-files", "--paths", ok+","+missing)

		if code != int(goaxi.ExitError) {
			t.Errorf("exit = %d, want ExitError", code)
		}
		if !hasTOONField(stdout, "help") {
			t.Errorf("stdout = %q, want a help field", stdout)
		}
		if !strings.Contains(stdout, "llm-filesystem read-multiple-files --paths "+missing) {
			t.Errorf("stdout = %q, want a retry naming only %s", stdout, missing)
		}
	})

	t.Run("write-multiple-files", func(t *testing.T) {
		dir := t.TempDir()
		allowed := filepath.Join(dir, "allowed")
		if err := os.Mkdir(allowed, 0o755); err != nil {
			t.Fatal(err)
		}
		good := filepath.Join(allowed, "a.txt")
		outside := filepath.Join(dir, "outside.txt")

		stdout, _, code := runCLI(t, "--allowed-dirs", allowed, "write-multiple-files", "--files",
			`[{"path":"`+good+`","content":"x"},{"path":"`+outside+`","content":"y"}]`)

		if code != int(goaxi.ExitError) {
			t.Errorf("exit = %d, want ExitError", code)
		}
		if !hasTOONField(stdout, "help") {
			t.Errorf("stdout = %q, want a help field", stdout)
		}
		if !strings.Contains(stdout, "entry 2 ("+outside) {
			t.Errorf("stdout = %q, want the failed entry named by number and path", stdout)
		}
		if strings.Contains(stdout, "entry 1 (") {
			t.Errorf("stdout = %q, names the entry that succeeded", stdout)
		}
		if !strings.Contains(stdout, "only entry 2") {
			t.Errorf("stdout = %q, want a retry of only the failed entry", stdout)
		}
	})

	t.Run("batch-file-operations", func(t *testing.T) {
		dir := t.TempDir()
		victim := writeKeep(t, "x")
		missing := filepath.Join(dir, "missing.txt")

		stdout, _, code := runCLI(t, "batch-file-operations", "--confirm", "--operations",
			`[{"operation":"delete","source":"`+victim+`"},{"operation":"copy","source":"`+missing+`","destination":"`+filepath.Join(dir, "out.txt")+`"}]`)

		if code != int(goaxi.ExitError) {
			t.Errorf("exit = %d, want ExitError", code)
		}
		if !hasTOONField(stdout, "help") {
			t.Errorf("stdout = %q, want a help field", stdout)
		}
		if !strings.Contains(stdout, "operation 2 (copy "+missing) {
			t.Errorf("stdout = %q, want the failed operation named by number", stdout)
		}
		if !strings.Contains(stdout, "only operation 2") {
			t.Errorf("stdout = %q, want a retry of only the failed operation", stdout)
		}
	})
}

// Items 5 and 8: an unknown key printed the raw decoder text with no entry
// number, so in a long payload the caller could not tell which entry to fix;
// and encoding/json matches keys case-insensitively, so {"Path","Content"}
// was accepted although the documented keys are lower case.
func TestPayloadKeyErrorsNameTheEntry(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"edits unknown key", []string{"edit-blocks", "--path", a, "--edits",
			`[{"old_string":"alpha","new_string":"A"},{"old_text":"x","new_string":"y"}]`}, `edit 2: unknown key "old_text"`},
		{"multi edits unknown key", []string{"edit-multiple-blocks", "--path", a, "--backup=false", "--edits",
			`[{"old_text":"alpha","new_text":"A"},{"old_string":"x","new_text":"y"}]`}, `edit 2: unknown key "old_string"`},
		{"files unknown key", []string{"write-multiple-files", "--files",
			`[{"path":"` + b + `","content":"x"},{"path":"` + b + `","text":"y"}]`}, `entry 2: unknown key "text"`},
		{"operations unknown key", []string{"batch-file-operations", "--operations",
			`[{"operation":"copy","source":"` + a + `","destination":"` + b + `"},{"operation":"copy","source":"` + a + `","dest":"x"}]`}, `operation 2: unknown key "dest"`},
		{"files key in wrong case", []string{"write-multiple-files", "--files",
			`[{"Path":"` + b + `","content":"x"}]`}, `entry 1: unknown key "Path" (keys are case-sensitive: "path")`},
		{"edits key in wrong case", []string{"edit-blocks", "--path", a, "--edits",
			`[{"Old_String":"alpha","new_string":"A"}]`}, `edit 1: unknown key "Old_String" (keys are case-sensitive: "old_string")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// JSON, so the quotes around the key are not escaped as in TOON.
			stdout, _, code := runCLI(t, append(tc.args, "--json")...)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d): %q", code, goaxi.ExitUsage, stdout)
			}
			var doc struct{ Message string }
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("stdout is not JSON: %v: %q", err, stdout)
			}
			if !strings.Contains(doc.Message, tc.want) {
				t.Errorf("message = %q, want %q", doc.Message, tc.want)
			}
			assertUntouched(t, a, "alpha\n")
			if _, err := os.Stat(b); err == nil {
				t.Error("an entry ran before the payload was refused")
			}
		})
	}
}

// Item 6: batch counted operations from 0 ("operation 0 is a delete") while
// edits count from 1. Both now count from 1, as a person reading the list does.
func TestBatchCountsOperationsFromOne(t *testing.T) {
	dir := t.TempDir()
	victim := writeKeep(t, "x")
	src := writeKeep(t, "y")

	stdout, _, code := runCLI(t, "batch-file-operations", "--operations",
		`[{"operation":"copy","source":"`+src+`","destination":"`+filepath.Join(dir, "c.txt")+`"},{"operation":"delete","source":"`+victim+`"}]`)

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage: %q", code, stdout)
	}
	if !strings.Contains(stdout, "operation 2 is a delete") {
		t.Errorf("stdout = %q, want the delete named as operation 2", stdout)
	}
	assertUntouched(t, victim, "x")
}
