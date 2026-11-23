package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// writeKeep creates a file with known contents for a payload test to try to
// destroy.
func writeKeep(t *testing.T, contents string) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// assertUntouched fails unless p still holds want: the side effect, not the
// exit code, is what proves a refusal refused.
func assertUntouched(t *testing.T, p, want string) {
	t.Helper()

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("the file was removed: %v", err)
	}
	if string(got) != want {
		t.Errorf("file = %q, want it untouched (%q)", got, want)
	}
}

// axi.md review round 1, item 1: Go's json.Unmarshal ignores unknown keys, so
// an entry keyed {"path","text"} decoded to an empty content and
// write-multiple-files truncated an existing 10-byte file to 0 bytes, then
// reported "File written successfully" with exit 0.
//
// A payload that does not say what to write is a malformed invocation: exit 2,
// and nothing touched.
func TestWriteMultipleFilesRejectsMalformedEntries(t *testing.T) {
	const precious = "0123456789"

	cases := map[string]func(p string) string{
		"unknown key":     func(p string) string { return `[{"path":"` + p + `","text":"x"}]` },
		"missing content": func(p string) string { return `[{"path":"` + p + `"}]` },
		"missing path":    func(p string) string { return `[{"content":"x"}]` },
		"not json":        func(p string) string { return `nope` },
		// Only strict decoding refuses these two: every required key is
		// present and valid, so the per-field checks pass them through.
		"extra unknown key": func(p string) string { return `[{"path":"` + p + `","content":"ok","text":"x"}]` },
		"trailing data":     func(p string) string { return `[{"path":"` + p + `","content":"ok"}] {}` },
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			keep := writeKeep(t, precious)

			stdout, _, code := runCLI(t, "write-multiple-files", "--files", payload(keep))

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d): %q", code, goaxi.ExitUsage, stdout)
			}
			if !strings.Contains(stdout, "error: true") {
				t.Errorf("stdout = %q, want a structured error", stdout)
			}
			// The payload refusal, not some later check, must be what stopped it.
			if !strings.Contains(stdout, "invalid --files JSON") {
				t.Errorf("stdout = %q, want the payload refusal", stdout)
			}
			assertUntouched(t, keep, precious)
		})
	}

	// The fix must not refuse an empty file that was asked for by name.
	t.Run("explicit empty content still writes", func(t *testing.T) {
		keep := writeKeep(t, precious)

		_, _, code := runCLI(t, "write-multiple-files", "--files", `[{"path":"`+keep+`","content":""}]`)

		if code != -1 && code != int(goaxi.ExitOK) {
			t.Errorf("exit = %d, want 0 for a well-formed payload", code)
		}
		assertUntouched(t, keep, "")
	})
}

// Item 4: a malformed --edits payload exited 1, which tells the agent the tool
// tried and broke, so it retries the same command. AGENTS.md and AXI section 6
// reserve exit 2 for a malformed invocation. Unknown keys are malformed too:
// {old,new} used to decode to empty edits and only core's later check caught it.
func TestMalformedEditsPayloadIsAUsageError(t *testing.T) {
	const precious = "alpha\nbeta\n"

	cases := []struct {
		name, cmd, edits string
	}{
		{"edit-blocks not json", "edit-blocks", `[{`},
		{"edit-blocks missing old_string", "edit-blocks", `[{"new_string":"x"}]`},
		{"edit-blocks unknown key", "edit-blocks", `[{"old":"alpha","new":"x"}]`},
		// Valid required keys plus one unknown: only strict decoding refuses it.
		{"edit-blocks extra unknown key", "edit-blocks", `[{"old_string":"alpha","new_string":"x","note":"y"}]`},
		{"edit-multiple-blocks extra unknown key", "edit-multiple-blocks", `[{"old_text":"alpha","new_text":"x","note":"y"}]`},
		{"edit-multiple-blocks not json", "edit-multiple-blocks", `{`},
		{"edit-multiple-blocks unknown key", "edit-multiple-blocks", `[{"old":"alpha","new_text":"x"}]`},
		{"edit-multiple-blocks replace without old_text", "edit-multiple-blocks", `[{"new_text":"x"}]`},
		// Following the shipped skill without line_number produced
		// status invalid_line, exit 0, file unchanged.
		{"edit-multiple-blocks insert without line_number", "edit-multiple-blocks", `[{"mode":"insert_before","new_text":"x"}]`},
		{"edit-multiple-blocks delete without line_number", "edit-multiple-blocks", `[{"mode":"delete_line"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keep := writeKeep(t, precious)

			stdout, _, code := runCLI(t, tc.cmd, "--path", keep, "--edits", tc.edits)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d): %q", code, goaxi.ExitUsage, stdout)
			}
			// The recovery step must show the expected shape, not --help.
			if strings.Contains(stdout, "--help") {
				t.Errorf("stdout = %q, want a specific next step rather than --help", stdout)
			}
			assertUntouched(t, keep, precious)
		})
	}
}

// A batch entry keyed "dest" instead of "destination" decoded to an empty
// destination. Reject it up front as malformed rather than attempting it.
//
// Each case carries a valid destination outside the working directory, and the
// diagnostic must be the payload refusal. An earlier version used only "dest",
// left destination empty, and passed because the empty path resolved to the
// working directory and the overwrite gate refused it instead (review of PR
// #85): deleting DisallowUnknownFields left it green.
func TestBatchRejectsUnknownOperationKeys(t *testing.T) {
	for name, extra := range map[string]string{
		"misspelled key": `"dest":"elsewhere"`,
		"unknown key":    `"foo":1`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.txt")
			if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(dir, "dst.txt")

			stdout, _, code := runCLI(t, "batch-file-operations", "--operations",
				`[{"operation":"copy","source":"`+src+`","destination":"`+dst+`",`+extra+`}]`)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d): %q", code, goaxi.ExitUsage, stdout)
			}
			if !strings.Contains(stdout, "invalid operations JSON") || !strings.Contains(stdout, "unknown field") {
				t.Errorf("stdout = %q, want the payload refusal naming the unknown field", stdout)
			}
			if _, err := os.Stat(dst); err == nil {
				t.Error("a malformed operation ran")
			}
		})
	}
}

// Item 3: a run in which entries failed exited 0 with no error marker, and the
// MCP server sets isError only from the exit code, so MCP clients were told it
// worked. compress, extract and sync already exit 1 when failed > 0, after
// printing the result so the caller learns which entries failed; these three
// must do the same.
func TestFailedEntriesExitOne(t *testing.T) {
	t.Run("write-multiple-files", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(dir, "file.txt")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		// A file cannot be a parent directory, so this entry fails.
		bad := filepath.Join(blocker, "child.txt")

		stdout, _, code := runCLI(t, "write-multiple-files", "--files", `[{"path":"`+bad+`","content":"x"}]`)

		if code != int(goaxi.ExitError) {
			t.Errorf("exit = %d, want ExitError (%d): %q", code, goaxi.ExitError, stdout)
		}
		if !hasTOONField(stdout, "failed") {
			t.Errorf("stdout = %q, want the result printed so the failure is named", stdout)
		}
	})

	t.Run("batch-file-operations unknown op", func(t *testing.T) {
		stdout, _, code := runCLI(t, "batch-file-operations", "--operations",
			`[{"operation":"frobnicate","source":"x"}]`)

		if code != int(goaxi.ExitError) {
			t.Errorf("exit = %d, want ExitError (%d): %q", code, goaxi.ExitError, stdout)
		}
		if !strings.Contains(stdout, "unknown operation") {
			t.Errorf("stdout = %q, want the failed entry named", stdout)
		}
	})

	t.Run("read-multiple-files missing file", func(t *testing.T) {
		dir := t.TempDir()
		ok := filepath.Join(dir, "ok.txt")
		if err := os.WriteFile(ok, []byte("hello"), 0o600); err != nil {
			t.Fatal(err)
		}
		missing := filepath.Join(dir, "missing.txt")

		stdout, _, code := runCLI(t, "read-multiple-files", "--paths", ok+","+missing)

		if code != int(goaxi.ExitError) {
			t.Errorf("exit = %d, want ExitError (%d): %q", code, goaxi.ExitError, stdout)
		}
		// The file that was read is still delivered.
		if !strings.Contains(stdout, "hello") {
			t.Errorf("stdout = %q, want the readable file's content", stdout)
		}
	})

	// The fix must not turn a clean run into a failure. runCLI reports -1
	// when nothing called exitFunc, which is how a clean run exits 0.
	t.Run("clean runs still exit 0", func(t *testing.T) {
		dir := t.TempDir()
		a := filepath.Join(dir, "a.txt")

		if _, _, code := runCLI(t, "write-multiple-files", "--files", `[{"path":"`+a+`","content":"x"}]`); code != -1 {
			t.Errorf("write-multiple-files exit = %d, want 0", code)
		}
		if _, _, code := runCLI(t, "read-multiple-files", "--paths", a); code != -1 {
			t.Errorf("read-multiple-files exit = %d, want 0", code)
		}
		b := filepath.Join(dir, "b.txt")
		if _, _, code := runCLI(t, "batch-file-operations", "--operations",
			`[{"operation":"copy","source":"`+a+`","destination":"`+b+`"}]`); code != -1 {
			t.Errorf("batch-file-operations exit = %d, want 0", code)
		}
	})
}

// Item 5: every tool failure ended in "Check the flags ... --help". AXI 9 asks
// for the command that fixes the problem. For a path that does not exist that
// is listing its parent directory, so the agent can see what is there.
func TestMissingPathSuggestsListingItsParent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope.txt")

	for _, args := range [][]string{
		{"read-file", "--path", missing},
		{"get-file-info", "--path", missing},
		{"copy-file", "--source", missing, "--dest", filepath.Join(dir, "out.txt")},
	} {
		t.Run(args[0], func(t *testing.T) {
			stdout, _, code := runCLI(t, args...)

			if code != int(goaxi.ExitError) {
				t.Errorf("exit = %d, want ExitError (%d)", code, goaxi.ExitError)
			}
			if !strings.Contains(stdout, "llm-filesystem list-directory --path "+dir) {
				t.Errorf("stdout = %q, want a list-directory step naming %s", stdout, dir)
			}
			if strings.Contains(stdout, "--help") {
				t.Errorf("stdout = %q, want the specific step instead of --help", stdout)
			}
		})
	}
}
