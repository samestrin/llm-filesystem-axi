package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBinary writes an executable named llm-filesystem in dir and makes setup
// believe it is the running binary, so a test decides where "this binary"
// lives and whether PATH finds it. Under go test the real executable is the
// test binary, which PATH never finds.
func fakeBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "llm-filesystem")
	writeFile(t, path, "#!/bin/sh\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	pretendExecutable(t, path)
	return path
}

func pretendExecutable(t *testing.T, path string) {
	t.Helper()
	prev := setupExecutable
	setupExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { setupExecutable = prev })
}

// codexSessionStartCommands lists every SessionStart hook command in a Codex
// hooks file. The file has the same shape as Claude Code's settings hooks.
func codexSessionStartCommands(t *testing.T, dir string) []string {
	t.Helper()
	return sessionStartCommands(t, readOrEmpty(t, filepath.Join(dir, setupCodexRel)))
}

// Epic 10 T1's "done when": in a temp HOME, .codex/hooks.json is written once,
// repaired after the binary moves, unchanged on a second run, and nothing is
// written under HOME. The binary is off PATH here, so the hook must name it by
// its absolute path; a bare name would run whatever PATH finds, or nothing.
func TestSetupWritesCodexHooksOnceRepairsAfterAMoveAndLeavesThemAlone(t *testing.T) {
	dir, home := setupProject(t)
	t.Setenv("PATH", t.TempDir())
	first := fakeBinary(t, t.TempDir())

	doc, code := runSetupJSON(t)
	if code != -1 {
		t.Fatalf("first run exit %d", code)
	}
	if got := statusOf(doc, setupCodexRel); got != "written" {
		t.Errorf("first run reported %s %q, want written", setupCodexRel, got)
	}
	if cmds := codexSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != first {
		t.Fatalf("SessionStart commands = %q, want exactly [%s]", cmds, first)
	}
	assertNothingWritten(t, home)

	info, err := os.Stat(filepath.Join(dir, setupCodexRel))
	if err != nil {
		t.Fatal(err)
	}
	before := readOrEmpty(t, filepath.Join(dir, setupCodexRel))
	doc, code = runSetupJSON(t)
	if code != -1 {
		t.Fatalf("second run exit %d", code)
	}
	if got := statusOf(doc, setupCodexRel); got != "unchanged" {
		t.Errorf("second run reported %q, want unchanged", got)
	}
	if after, _ := os.Stat(filepath.Join(dir, setupCodexRel)); !after.ModTime().Equal(info.ModTime()) {
		t.Error("hooks.json was rewritten on the second run")
	}
	if readOrEmpty(t, filepath.Join(dir, setupCodexRel)) != before {
		t.Error("hooks.json changed on the second run")
	}

	// The binary moves (a reinstall elsewhere): the hook follows it, and the
	// old path is replaced rather than kept beside the new one.
	moved := fakeBinary(t, t.TempDir())
	doc, code = runSetupJSON(t)
	if code != -1 {
		t.Fatalf("run after the move exit %d", code)
	}
	if got := statusOf(doc, setupCodexRel); got != "written" {
		t.Errorf("run after the move reported %q, want written", got)
	}
	if cmds := codexSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != moved {
		t.Errorf("SessionStart commands after the move = %q, want exactly [%s]", cmds, moved)
	}

	if doc, _ = runSetupJSON(t); statusOf(doc, setupCodexRel) != "unchanged" {
		t.Errorf("run after the repair reported %q, want unchanged", statusOf(doc, setupCodexRel))
	}
	assertNothingWritten(t, home)
}

// The spec's portable command: the bare name when PATH finds this very binary,
// so a global install survives an upgrade in place; the absolute path when PATH
// finds a different llm-filesystem, which the hook must not run by accident.
func TestSetupCodexHookUsesTheBareNameOnlyWhenPATHFindsThisBinary(t *testing.T) {
	t.Run("PATH finds this binary", func(t *testing.T) {
		dir, _ := setupProject(t)
		bin := t.TempDir()
		t.Setenv("PATH", bin)
		fakeBinary(t, bin)

		if _, code := runSetupJSON(t); code != -1 {
			t.Fatalf("exit %d", code)
		}
		if cmds := codexSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != "llm-filesystem" {
			t.Errorf("SessionStart commands = %q, want [llm-filesystem]", cmds)
		}
	})

	t.Run("PATH finds a different llm-filesystem", func(t *testing.T) {
		dir, _ := setupProject(t)
		other := t.TempDir()
		t.Setenv("PATH", other)
		fakeBinary(t, other) // the one on PATH
		self := fakeBinary(t, t.TempDir())

		if _, code := runSetupJSON(t); code != -1 {
			t.Fatalf("exit %d", code)
		}
		if cmds := codexSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != self {
			t.Errorf("SessionStart commands = %q, want [%s]", cmds, self)
		}
	})

	// A bare-name hook written while the binary was on PATH is repaired to the
	// absolute path once PATH no longer leads to it.
	t.Run("a bare hook is repaired when PATH stops finding this binary", func(t *testing.T) {
		dir, _ := setupProject(t)
		t.Setenv("PATH", t.TempDir())
		self := fakeBinary(t, t.TempDir())
		writeFile(t, filepath.Join(dir, setupCodexRel), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"llm-filesystem"}]}]}}`)

		doc, code := runSetupJSON(t)
		if code != -1 || statusOf(doc, setupCodexRel) != "written" {
			t.Fatalf("exit %d, status %q", code, statusOf(doc, setupCodexRel))
		}
		if cmds := codexSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != self {
			t.Errorf("SessionStart commands = %q, want [%s]", cmds, self)
		}
	})
}

// Codex runs a hook command through a shell, so an absolute path with a space
// in it has to be quoted, and the quoted form must still be recognised as ours
// on the next run, or every run would append another hook.
func TestSetupCodexHookQuotesAPathTheShellWouldSplit(t *testing.T) {
	dir, _ := setupProject(t)
	t.Setenv("PATH", t.TempDir())
	bin := filepath.Join(t.TempDir(), "my tools")
	self := fakeBinary(t, bin)

	if _, code := runSetupJSON(t); code != -1 {
		t.Fatalf("exit %d", code)
	}
	want := "'" + self + "'"
	if cmds := codexSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != want {
		t.Fatalf("SessionStart commands = %q, want [%s]", cmds, want)
	}
	if doc, _ := runSetupJSON(t); statusOf(doc, setupCodexRel) != "unchanged" {
		t.Errorf("second run reported %q, want unchanged", statusOf(doc, setupCodexRel))
	}

	moved := fakeBinary(t, t.TempDir())
	if _, code := runSetupJSON(t); code != -1 {
		t.Fatalf("exit %d after the move", code)
	}
	if cmds := codexSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != moved {
		t.Errorf("SessionStart commands after the move = %q, want exactly [%s]", cmds, moved)
	}
}

// A user's hooks.json holds their other hooks. Every one of them survives, the
// stale llm-filesystem hook is repaired in place with its other fields (here a
// timeout) kept, and no second llm-filesystem hook is appended beside it.
func TestSetupKeepsOtherCodexHooksAndRepairsOursInPlace(t *testing.T) {
	dir, _ := setupProject(t)
	t.Setenv("PATH", t.TempDir())
	self := fakeBinary(t, t.TempDir())
	existing := `{
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "guard.sh"}]}],
    "SessionStart": [
      {"matcher": "startup", "hooks": [{"type": "command", "command": "echo hi >&2"}]},
      {"hooks": [{"type": "command", "command": "/old/place/llm-filesystem", "timeout": 30}]}
    ]
  },
  "note": "1 < 2"
}
`
	writeFile(t, filepath.Join(dir, setupCodexRel), existing)

	if doc, code := runSetupJSON(t); code != -1 || statusOf(doc, setupCodexRel) != "written" {
		t.Fatalf("exit %d, status %q", code, statusOf(doc, setupCodexRel))
	}
	got := readOrEmpty(t, filepath.Join(dir, setupCodexRel))
	if cmds := codexSessionStartCommands(t, dir); strings.Join(cmds, ",") != "echo hi >&2,"+self {
		t.Errorf("SessionStart commands = %q, want the other hook kept and ours repaired in place", cmds)
	}

	var before, after struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
		Note  string                     `json:"note"`
	}
	if err := json.Unmarshal([]byte(existing), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got), &after); err != nil {
		t.Fatalf("hooks.json is not JSON: %v\n%s", err, got)
	}
	if compact(t, after.Hooks["PreToolUse"]) != compact(t, before.Hooks["PreToolUse"]) {
		t.Errorf("PreToolUse changed: %s", after.Hooks["PreToolUse"])
	}
	if after.Note != "1 < 2" || strings.Contains(got, `\`+"u00") {
		t.Errorf("another key was lost or re-escaped:\n%s", got)
	}
	var groups []struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Timeout int `json:"timeout"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(after.Hooks["SessionStart"], &groups); err != nil || len(groups) != 2 {
		t.Fatalf("SessionStart = %s", after.Hooks["SessionStart"])
	}
	if groups[0].Matcher != "startup" || groups[1].Hooks[0].Timeout != 30 {
		t.Errorf("the matcher or the timeout was dropped:\n%s", got)
	}
}

// A hooks.json setup cannot parse belongs to the user, as settings.json does:
// refused with exit 1, left byte-identical, and nothing else written, because
// every file is planned before any is touched.
func TestSetupRefusesCodexHooksItCannotMerge(t *testing.T) {
	cases := map[string]string{
		"not json":                  "hooks = []\n",
		"an array":                  "[]\n",
		"hooks is a list":           `{"hooks":[]}`,
		"SessionStart is an object": `{"hooks":{"SessionStart":{}}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir, home := setupProject(t)
			path := filepath.Join(dir, setupCodexRel)
			writeFile(t, path, content)

			_, _, code := runCLI(t, "setup")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			if got := readOrEmpty(t, path); got != content {
				t.Errorf("hooks.json changed:\n%s", got)
			}
			assertNothingWritten(t, dir, ".codex")
			assertNothingWritten(t, filepath.Join(dir, ".codex"), "hooks.json")
			assertNothingWritten(t, home)
		})
	}
}

// Recognising "ours" is what path repair rests on: too loose and setup rewrites
// a user's own hook; too strict and every run appends a duplicate.
func TestIsSetupHookCommand(t *testing.T) {
	ours := []string{"llm-filesystem", "/usr/local/bin/llm-filesystem", "'/opt/my tools/llm-filesystem'"}
	notOurs := []string{"llm-filesystem --help", "llm-filesystem-mcp", "/bin/llm-filesystem-mcp", "bin/llm-filesystem", "echo llm-filesystem", "'llm-filesystem", ""}
	for _, c := range ours {
		if !isSetupHookCommand(c) {
			t.Errorf("%q not recognised as the setup hook", c)
		}
	}
	for _, c := range notOurs {
		if isSetupHookCommand(c) {
			t.Errorf("%q wrongly recognised as the setup hook", c)
		}
	}
}
