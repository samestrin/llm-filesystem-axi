package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// openCodePluginCommand is the command the plugin runs, read back from its
// `const command = ...;` line. The value is a JSON string literal, which is
// also a JavaScript one, so decoding it is exactly what the plugin will see.
func openCodePluginCommand(t *testing.T, dir string) string {
	t.Helper()
	src := readOrEmpty(t, filepath.Join(dir, setupOpenCodeRel))
	for _, line := range strings.Split(src, "\n") {
		if rest, ok := strings.CutPrefix(line, "const command = "); ok {
			var cmd string
			if err := json.Unmarshal([]byte(strings.TrimSuffix(rest, ";")), &cmd); err != nil {
				t.Fatalf("the command line is not a string literal: %v\n%s", err, line)
			}
			return cmd
		}
	}
	t.Fatalf("no command line in the plugin:\n%s", src)
	return ""
}

// Epic 10 T2's "done when": in a temp HOME, the OpenCode plugin is written
// once, repaired after the binary moves, unchanged on a second run, and nothing
// is written under HOME. The binary is off PATH here, so the plugin must name it
// by its absolute path; a bare name would run whatever PATH finds, or nothing.
func TestSetupWritesTheOpenCodePluginOnceRepairsAfterAMoveAndLeavesItAlone(t *testing.T) {
	dir, home := setupProject(t)
	t.Setenv("PATH", t.TempDir())
	first := fakeBinary(t, t.TempDir())

	doc, code := runSetupJSON(t)
	if code != -1 {
		t.Fatalf("first run exit %d", code)
	}
	if got := statusOf(doc, setupOpenCodeRel); got != "written" {
		t.Errorf("first run reported %s %q, want written", setupOpenCodeRel, got)
	}
	if got := openCodePluginCommand(t, dir); got != first {
		t.Fatalf("plugin command = %q, want %s", got, first)
	}
	assertNothingWritten(t, filepath.Join(dir, ".opencode", "plugins"), "llm-filesystem.js")
	assertNothingWritten(t, home)

	info, err := os.Stat(filepath.Join(dir, setupOpenCodeRel))
	if err != nil {
		t.Fatal(err)
	}
	before := readOrEmpty(t, filepath.Join(dir, setupOpenCodeRel))
	doc, code = runSetupJSON(t)
	if code != -1 {
		t.Fatalf("second run exit %d", code)
	}
	if got := statusOf(doc, setupOpenCodeRel); got != "unchanged" {
		t.Errorf("second run reported %q, want unchanged", got)
	}
	if after, _ := os.Stat(filepath.Join(dir, setupOpenCodeRel)); !after.ModTime().Equal(info.ModTime()) {
		t.Error("the plugin was rewritten on the second run")
	}
	if readOrEmpty(t, filepath.Join(dir, setupOpenCodeRel)) != before {
		t.Error("the plugin changed on the second run")
	}

	// The binary moves (a reinstall elsewhere): the plugin follows it.
	moved := fakeBinary(t, t.TempDir())
	doc, code = runSetupJSON(t)
	if code != -1 {
		t.Fatalf("run after the move exit %d", code)
	}
	if got := statusOf(doc, setupOpenCodeRel); got != "written" {
		t.Errorf("run after the move reported %q, want written", got)
	}
	if got := openCodePluginCommand(t, dir); got != moved {
		t.Errorf("plugin command after the move = %q, want %s", got, moved)
	}

	if doc, _ = runSetupJSON(t); statusOf(doc, setupOpenCodeRel) != "unchanged" {
		t.Errorf("run after the repair reported %q, want unchanged", statusOf(doc, setupOpenCodeRel))
	}
	assertNothingWritten(t, home)
}

// The spec's portable command, as for Codex: the bare name only when PATH finds
// this very binary. The plugin runs the command without a shell, so a path
// with a space or a quote in it is carried as it is, not shell-quoted.
func TestSetupOpenCodePluginUsesTheBareNameOnlyWhenPATHFindsThisBinary(t *testing.T) {
	t.Run("PATH finds this binary", func(t *testing.T) {
		dir, _ := setupProject(t)
		bin := t.TempDir()
		t.Setenv("PATH", bin)
		fakeBinary(t, bin)

		if _, code := runSetupJSON(t); code != -1 {
			t.Fatalf("exit %d", code)
		}
		if got := openCodePluginCommand(t, dir); got != "llm-filesystem" {
			t.Errorf("plugin command = %q, want llm-filesystem", got)
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
		if got := openCodePluginCommand(t, dir); got != self {
			t.Errorf("plugin command = %q, want %s", got, self)
		}
	})

	t.Run("a path with a space and a quote", func(t *testing.T) {
		dir, _ := setupProject(t)
		t.Setenv("PATH", t.TempDir())
		self := fakeBinary(t, filepath.Join(t.TempDir(), `my "tools" o'clock`))

		if _, code := runSetupJSON(t); code != -1 {
			t.Fatalf("exit %d", code)
		}
		if got := openCodePluginCommand(t, dir); got != self {
			t.Errorf("plugin command = %q, want %s", got, self)
		}
		if doc, _ := runSetupJSON(t); statusOf(doc, setupOpenCodeRel) != "unchanged" {
			t.Errorf("second run reported %q, want unchanged", statusOf(doc, setupOpenCodeRel))
		}
	})
}

// Spec lines 181-183: inject the home view as system context rather than add a
// custom tool. The plugin is run under node against a fake llm-filesystem, the
// way OpenCode calls it: the factory with the project directory, then the
// system-transform hook with a system list to add to. The fake prints the
// directory it ran in, which proves the home view is the project's own.
func TestTheOpenCodePluginAddsTheHomeViewToTheSystemContext(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the plugin cannot be run")
	}
	dir, _ := setupProject(t)
	t.Setenv("PATH", t.TempDir())
	bin := fakeBinary(t, t.TempDir())
	writeFile(t, bin, "#!/bin/sh\necho \"home view of $(pwd)\"\n")

	if _, code := runSetupJSON(t); code != -1 {
		t.Fatalf("exit %d", code)
	}
	src := readOrEmpty(t, filepath.Join(dir, setupOpenCodeRel))
	if strings.Contains(src, "tool:") || strings.Contains(src, "tool(") {
		t.Errorf("the plugin defines a tool; the spec asks for system context only:\n%s", src)
	}

	run := func(t *testing.T) []string {
		t.Helper()
		driver := `
const mod = await import(process.argv[1]);
const factories = Object.values(mod).filter((v) => typeof v === "function");
if (factories.length !== 1) throw new Error("want one plugin export, got " + factories.length);
const hooks = await factories[0]({ directory: process.argv[2] });
const keys = Object.keys(hooks);
if (keys.join() !== "experimental.chat.system.transform") throw new Error("hooks: " + keys.join());
const output = { system: ["base prompt"] };
await hooks["experimental.chat.system.transform"]({}, output);
console.log(JSON.stringify(output.system));
`
		out, err := exec.Command(node, "--input-type=module", "-e", driver, filepath.Join(dir, setupOpenCodeRel), dir).CombinedOutput()
		if err != nil {
			t.Fatalf("node: %v\n%s", err, out)
		}
		var system []string
		if err := json.Unmarshal(out, &system); err != nil {
			t.Fatalf("driver output: %v\n%s", err, out)
		}
		return system
	}

	if got := run(t); len(got) != 2 || got[0] != "base prompt" || got[1] != "home view of "+dir {
		t.Errorf("system = %q, want the base prompt then the home view of %s", got, dir)
	}

	// A home view that fails adds nothing and does not break the session.
	writeFile(t, bin, "#!/bin/sh\necho partial; exit 1\n")
	if got := run(t); len(got) != 1 || got[0] != "base prompt" {
		t.Errorf("system after a failing home view = %q, want only the base prompt", got)
	}
}

// A file at the plugin's path that setup did not write belongs to the user.
// Overwriting it would destroy their plugin, so the run is refused with exit 1,
// the file is left byte-identical, and nothing else is written: every file is
// planned before any is touched.
func TestSetupRefusesAnOpenCodePluginItDidNotWrite(t *testing.T) {
	dir, home := setupProject(t)
	path := filepath.Join(dir, setupOpenCodeRel)
	own := "export const Mine = async () => ({})\n"
	writeFile(t, path, own)

	stdout, _, code := runCLI(t, "setup", "--format", "json")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(stdout, setupOpenCodeRel) {
		t.Errorf("the refusal does not name the file: %s", stdout)
	}
	if got := readOrEmpty(t, path); got != own {
		t.Errorf("the user's plugin changed:\n%s", got)
	}
	assertNothingWritten(t, dir, ".opencode")
	assertNothingWritten(t, home)
}
