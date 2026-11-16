package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// claudeSessionStartCommands lists every SessionStart hook command in the
// project's .claude/settings.json.
func claudeSessionStartCommands(t *testing.T, dir string) []string {
	t.Helper()
	return sessionStartCommands(t, readOrEmpty(t, filepath.Join(dir, setupSettingsRel)))
}

// Fresh-context review finding 1 (EST-6608): setup wrote the bare name into
// .claude/settings.json even when PATH did not find this binary, so Claude
// Code's hook ran nothing (or a different llm-filesystem) while the Codex hook
// and the OpenCode plugin got the absolute path. In a temp HOME with the binary
// off PATH: the hook names the absolute path, a moved binary is repaired in
// place rather than registered twice, and a second run is a no-op.
func TestSetupWritesClaudeHookOnceRepairsAfterAMoveAndLeavesItAlone(t *testing.T) {
	dir, home := setupProject(t)
	t.Setenv("PATH", t.TempDir())
	first := fakeBinary(t, t.TempDir())

	doc, code := runSetupJSON(t)
	if code != -1 {
		t.Fatalf("first run exit %d", code)
	}
	if got := statusOf(doc, setupSettingsRel); got != "written" {
		t.Errorf("first run reported %s %q, want written", setupSettingsRel, got)
	}
	if cmds := claudeSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != first {
		t.Fatalf("SessionStart commands = %q, want exactly [%s]", cmds, first)
	}
	assertNothingWritten(t, home)

	info, err := os.Stat(filepath.Join(dir, setupSettingsRel))
	if err != nil {
		t.Fatal(err)
	}
	before := readOrEmpty(t, filepath.Join(dir, setupSettingsRel))
	if doc, _ = runSetupJSON(t); statusOf(doc, setupSettingsRel) != "unchanged" {
		t.Errorf("second run reported %q, want unchanged", statusOf(doc, setupSettingsRel))
	}
	if after, _ := os.Stat(filepath.Join(dir, setupSettingsRel)); !after.ModTime().Equal(info.ModTime()) {
		t.Error("settings.json was rewritten on the second run")
	}
	if readOrEmpty(t, filepath.Join(dir, setupSettingsRel)) != before {
		t.Error("settings.json changed on the second run")
	}

	moved := fakeBinary(t, t.TempDir())
	doc, code = runSetupJSON(t)
	if code != -1 || statusOf(doc, setupSettingsRel) != "written" {
		t.Fatalf("run after the move: exit %d, status %q; want written", code, statusOf(doc, setupSettingsRel))
	}
	if cmds := claudeSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != moved {
		t.Errorf("SessionStart commands after the move = %q, want exactly [%s]", cmds, moved)
	}
	if doc, _ = runSetupJSON(t); statusOf(doc, setupSettingsRel) != "unchanged" {
		t.Errorf("run after the repair reported %q, want unchanged", statusOf(doc, setupSettingsRel))
	}
	assertNothingWritten(t, home)
}

// The bare name stays when PATH finds this very binary, so a global install
// survives an upgrade in place; a bare hook written then is repaired to the
// absolute path once PATH stops leading to it.
func TestSetupClaudeHookUsesTheBareNameOnlyWhenPATHFindsThisBinary(t *testing.T) {
	t.Run("PATH finds this binary", func(t *testing.T) {
		dir, _ := setupProject(t)
		bin := t.TempDir()
		t.Setenv("PATH", bin)
		fakeBinary(t, bin)

		if _, code := runSetupJSON(t); code != -1 {
			t.Fatalf("exit %d", code)
		}
		if cmds := claudeSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != "llm-filesystem" {
			t.Errorf("SessionStart commands = %q, want [llm-filesystem]", cmds)
		}
	})

	t.Run("a bare hook is repaired when PATH stops finding this binary", func(t *testing.T) {
		dir, _ := setupProject(t)
		t.Setenv("PATH", t.TempDir())
		self := fakeBinary(t, t.TempDir())
		writeFile(t, filepath.Join(dir, setupSettingsRel), `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"llm-filesystem"}]}]}}`)

		doc, code := runSetupJSON(t)
		if code != -1 || statusOf(doc, setupSettingsRel) != "written" {
			t.Fatalf("exit %d, status %q", code, statusOf(doc, setupSettingsRel))
		}
		if cmds := claudeSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != self {
			t.Errorf("SessionStart commands = %q, want [%s]", cmds, self)
		}
	})

	// Claude Code runs the hook through a shell too, so a path with a space is
	// quoted, and the quoted form is recognised as ours on the next run.
	t.Run("a path the shell would split is quoted", func(t *testing.T) {
		dir, _ := setupProject(t)
		t.Setenv("PATH", t.TempDir())
		self := fakeBinary(t, filepath.Join(t.TempDir(), "my tools"))

		if _, code := runSetupJSON(t); code != -1 {
			t.Fatalf("exit %d", code)
		}
		if cmds := claudeSessionStartCommands(t, dir); len(cmds) != 1 || cmds[0] != "'"+self+"'" {
			t.Fatalf("SessionStart commands = %q, want ['%s']", cmds, self)
		}
		if doc, _ := runSetupJSON(t); statusOf(doc, setupSettingsRel) != "unchanged" {
			t.Errorf("second run reported %q, want unchanged", statusOf(doc, setupSettingsRel))
		}
	})
}
