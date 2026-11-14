package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/llm-filesystem-axi/integrations/claude-code/skill"
)

var (
	setupSettingsRel = filepath.Join(".claude", "settings.json")
	setupSkillRel    = filepath.Join(".claude", "skills", "llm-filesystem", "SKILL.md")
	setupCodexRel    = filepath.Join(".codex", "hooks.json")
	setupOpenCodeRel = filepath.Join(".opencode", "plugins", "llm-filesystem.js")
)

// setupProject makes an empty project directory, moves into it, and points HOME
// at a separate empty directory, so every test can also check that the
// user-wide ~/.claude was left alone.
func setupProject(t *testing.T) (dir, home string) {
	t.Helper()
	dir, home = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(dir)
	return dir, home
}

type setupDoc struct {
	Dir    string `json:"dir"`
	DryRun bool   `json:"dry_run"`
	Files  []struct {
		Path   string `json:"path"`
		Status string `json:"status"`
		Error  string `json:"error"`
	} `json:"files"`
	Success bool `json:"success"`
}

// runSetupJSON runs setup with --format json and decodes what it printed.
func runSetupJSON(t *testing.T, args ...string) (setupDoc, int) {
	t.Helper()
	stdout, stderr, code := runCLI(t, append([]string{"setup", "--format", "json"}, args...)...)
	var doc setupDoc
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("setup output is not JSON: %v\nstdout = %s\nstderr = %s", err, stdout, stderr)
	}
	return doc, code
}

func statusOf(doc setupDoc, rel string) string {
	for _, f := range doc.Files {
		if f.Path == rel {
			return f.Status
		}
	}
	return "<not reported>"
}

func readOrEmpty(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// assertNothingWritten fails if dir holds anything but the names in keep.
func assertNothingWritten(t *testing.T, dir string, keep ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, k := range keep {
		allowed[k] = true
	}
	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("%s: found %q; nothing should have been written there", dir, e.Name())
		}
	}
}

// sessionStartCommands lists every SessionStart hook command in a settings file.
func sessionStartCommands(t *testing.T, settings string) []string {
	t.Helper()
	var doc struct {
		Hooks struct {
			SessionStart []struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"SessionStart"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(settings), &doc); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, settings)
	}
	var cmds []string
	for _, g := range doc.Hooks.SessionStart {
		for _, h := range g.Hooks {
			if h.Type != "command" {
				t.Errorf("hook %q has type %q, want command", h.Command, h.Type)
			}
			cmds = append(cmds, h.Command)
		}
	}
	return cmds
}

// Epic 05 acceptance criterion 1: in an empty directory setup writes the hook
// and the skill. The skill is compared with the file in the repository, not
// with the embedded copy, because "one copy" is the point: if the embed ever
// pointed somewhere else this would go red.
func TestSetupWritesHookAndSkillInAnEmptyProject(t *testing.T) {
	// Read before setupProject moves into another directory.
	shipped, err := os.ReadFile("../../../integrations/claude-code/skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	dir, home := setupProject(t)

	doc, code := runSetupJSON(t)
	if code != -1 {
		t.Fatalf("exit %d, want 0", code)
	}

	cmds := sessionStartCommands(t, readOrEmpty(t, filepath.Join(dir, setupSettingsRel)))
	if len(cmds) != 1 || cmds[0] != "llm-filesystem" {
		t.Errorf("SessionStart commands = %q, want exactly [llm-filesystem]", cmds)
	}

	if got := readOrEmpty(t, filepath.Join(dir, setupSkillRel)); got != string(shipped) {
		t.Errorf("SKILL.md differs from integrations/claude-code/skill/SKILL.md\ngot:\n%s", got)
	}
	if skill.Content != string(shipped) {
		t.Error("the embedded skill is not the shipped file")
	}

	for _, rel := range []string{setupSettingsRel, setupSkillRel} {
		if got := statusOf(doc, rel); got != "written" {
			t.Errorf("%s reported %q, want written", rel, got)
		}
	}
	if !doc.Success || doc.DryRun {
		t.Errorf("success = %v, dry_run = %v; want true, false", doc.Success, doc.DryRun)
	}
	assertNothingWritten(t, home)
}

// Epic 05 acceptance criterion 1, second half: a second run changes no bytes
// and says so. The modification times are checked as well as the bytes, since
// rewriting a file with identical content is still a write the user did not
// ask for and would wake every file watcher in the project.
func TestSetupRerunLeavesBothFilesAlone(t *testing.T) {
	dir, _ := setupProject(t)

	if _, code := runSetupJSON(t); code != -1 {
		t.Fatalf("first run exit %d", code)
	}
	before := map[string]os.FileInfo{}
	contents := map[string]string{}
	for _, rel := range []string{setupSettingsRel, setupSkillRel} {
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		before[rel] = info
		contents[rel] = readOrEmpty(t, filepath.Join(dir, rel))
	}

	doc, code := runSetupJSON(t)
	if code != -1 {
		t.Fatalf("second run exit %d", code)
	}
	for _, rel := range []string{setupSettingsRel, setupSkillRel} {
		if got := statusOf(doc, rel); got != "unchanged" {
			t.Errorf("%s reported %q on the second run, want unchanged", rel, got)
		}
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(before[rel].ModTime()) {
			t.Errorf("%s was rewritten on the second run", rel)
		}
		if got := readOrEmpty(t, filepath.Join(dir, rel)); got != contents[rel] {
			t.Errorf("%s changed on the second run", rel)
		}
	}
}

// The status of each file has to reach the caller in every format, with a
// next step, not only in JSON. TOON rows and text lines are checked per line,
// so a path that happens to contain a status word cannot satisfy the check.
func TestSetupReportsEachPathInEveryFormat(t *testing.T) {
	setupProject(t)

	stdout, _, code := runCLI(t, "setup", "--dry-run")
	if code != -1 {
		t.Fatalf("toon dry run exit %d", code)
	}
	for _, rel := range []string{setupSettingsRel, setupSkillRel, setupCodexRel, setupOpenCodeRel} {
		if !lineHasBoth(stdout, rel, "would-write") {
			t.Errorf("TOON does not report %s as would-write:\n%s", rel, stdout)
		}
	}
	if !hasTOONField(stdout, "dry_run") || !hasTOONField(stdout, "help") {
		t.Errorf("TOON is missing dry_run or help[]:\n%s", stdout)
	}

	if _, _, code := runCLI(t, "setup"); code != -1 {
		t.Fatalf("real run exit %d", code)
	}

	stdout, _, code = runCLI(t, "setup", "--format", "text")
	if code != -1 {
		t.Fatalf("text exit %d", code)
	}
	for _, rel := range []string{setupSettingsRel, setupSkillRel, setupCodexRel, setupOpenCodeRel} {
		if !lineHasBoth(stdout, rel, "unchanged") {
			t.Errorf("text does not report %s as unchanged:\n%s", rel, stdout)
		}
	}

	stdout, _, _ = runCLI(t, "setup", "--format", "json")
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatal(err)
	}
	if steps, _ := doc["next_steps"].([]any); len(steps) == 0 {
		t.Errorf("JSON carries no next_steps:\n%s", stdout)
	}
}

func lineHasBoth(out, a, b string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, a) && strings.Contains(line, b) {
			return true
		}
	}
	return false
}

// A user's settings file holds their permissions, model and other hooks.
// Losing any of it to a merge is the worst thing setup could do, so every key
// is compared value for value, including an existing SessionStart hook, and
// the key order is kept so the diff a user sees is only the addition.
func TestSetupKeepsEveryExistingSettingsKey(t *testing.T) {
	dir, _ := setupProject(t)
	existing := `{
  "model": "opus",
  "permissions": {"allow": ["Bash(ls:*)"], "deny": []},
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "guard.sh"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "echo hello >&2"}]}]
  },
  "env": {"A": "1 < 2"}
}
`
	writeFile(t, filepath.Join(dir, setupSettingsRel), existing)

	if doc, code := runSetupJSON(t); code != -1 || statusOf(doc, setupSettingsRel) != "written" {
		t.Fatalf("exit %d, settings status %q", code, statusOf(doc, setupSettingsRel))
	}
	got := readOrEmpty(t, filepath.Join(dir, setupSettingsRel))

	var before, after map[string]json.RawMessage
	if err := json.Unmarshal([]byte(existing), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got), &after); err != nil {
		t.Fatalf("merged settings are not JSON: %v\n%s", err, got)
	}
	for _, key := range []string{"model", "permissions", "env"} {
		if compact(t, after[key]) != compact(t, before[key]) {
			t.Errorf("%q changed: %s -> %s", key, before[key], after[key])
		}
	}
	var beforeHooks, afterHooks map[string]json.RawMessage
	_ = json.Unmarshal(before["hooks"], &beforeHooks)
	_ = json.Unmarshal(after["hooks"], &afterHooks)
	if compact(t, afterHooks["PreToolUse"]) != compact(t, beforeHooks["PreToolUse"]) {
		t.Errorf("PreToolUse changed: %s", afterHooks["PreToolUse"])
	}
	if cmds := sessionStartCommands(t, got); strings.Join(cmds, ",") != "echo hello >&2,llm-filesystem" {
		t.Errorf("SessionStart commands = %q, want the existing hook kept and ours appended", cmds)
	}
	if strings.Index(got, `"model"`) > strings.Index(got, `"permissions"`) ||
		strings.Index(got, `"hooks"`) > strings.Index(got, `"env"`) {
		t.Errorf("key order changed:\n%s", got)
	}
	if strings.Contains(got, `\`+"u00") {
		t.Errorf("a value was re-escaped:\n%s", got)
	}
}

func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A settings file that already runs llm-filesystem at session start is left
// byte for byte as the user wrote it, formatting included; only the missing
// skill is written.
func TestSetupLeavesARegisteredHookAlone(t *testing.T) {
	dir, _ := setupProject(t)
	existing := `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"llm-filesystem"}]}]}}`
	writeFile(t, filepath.Join(dir, setupSettingsRel), existing)

	doc, code := runSetupJSON(t)
	if code != -1 {
		t.Fatalf("exit %d", code)
	}
	if got := readOrEmpty(t, filepath.Join(dir, setupSettingsRel)); got != existing {
		t.Errorf("settings.json was rewritten:\n%s", got)
	}
	if statusOf(doc, setupSettingsRel) != "unchanged" || statusOf(doc, setupSkillRel) != "written" {
		t.Errorf("statuses: settings %q, skill %q", statusOf(doc, setupSettingsRel), statusOf(doc, setupSkillRel))
	}
}

// --dry-run must write nothing at all, not even the .claude directory, and say
// what it would have written.
func TestSetupDryRunWritesNothing(t *testing.T) {
	dir, home := setupProject(t)

	doc, code := runSetupJSON(t, "--dry-run")
	if code != -1 {
		t.Fatalf("exit %d", code)
	}
	assertNothingWritten(t, dir)
	assertNothingWritten(t, home)
	if !doc.DryRun {
		t.Error("dry_run is not set on a dry run")
	}
	for _, rel := range []string{setupSettingsRel, setupSkillRel, setupCodexRel, setupOpenCodeRel} {
		if got := statusOf(doc, rel); got != "would-write" {
			t.Errorf("%s reported %q, want would-write", rel, got)
		}
	}
}

// --allowed-dirs is the sandbox every other command honours. A setup that
// wrote outside it would be the one command able to escape it.
func TestSetupRefusesAWorkingDirectoryOutsideAllowedDirs(t *testing.T) {
	dir, _ := setupProject(t)
	allowed := t.TempDir()

	_, _, code := runCLI(t, "setup", "--allowed-dirs", allowed)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertNothingWritten(t, dir)

	// And inside the sandbox it works, so the refusal above is about the
	// sandbox and not about the flag being present.
	if _, _, code := runCLI(t, "setup", "--allowed-dirs", dir); code != -1 {
		t.Errorf("exit %d inside --allowed-dirs, want 0", code)
	}
}

// The sandbox check has to look at where setup actually writes. A working
// directory reached through a link inside the sandbox can resolve outside it;
// checking only the path as given let setup write there (review of PR #29).
func TestSetupRefusesAWorkingDirectoryLinkedOutOfAllowedDirs(t *testing.T) {
	_, home := setupProject(t)
	allowed, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)

	_, _, code := runCLI(t, "setup", "--allowed-dirs", allowed)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertNothingWritten(t, outside)
	assertNothingWritten(t, allowed, "link")
	assertNothingWritten(t, home)
}

// The other side of the check above: a sandbox named through a link, as macOS
// names every temporary directory (/var is /private/var), still contains the
// directory it points at. Comparing a resolved cwd with an unresolved root
// would refuse it.
func TestSetupAcceptsAnAllowedDirReachedThroughALink(t *testing.T) {
	dir, _ := setupProject(t)
	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runCLI(t, "setup", "--allowed-dirs", link); code != -1 {
		t.Fatalf("exit %d, want 0", code)
	}
	if readOrEmpty(t, filepath.Join(dir, setupSkillRel)) != skill.Content {
		t.Error("the skill was not written")
	}
}

// Run in the home directory, .claude/settings.json IS the user-wide settings
// file. The epic puts that out of scope, so setup refuses rather than editing
// every project's hooks at once.
func TestSetupRefusesTheHomeDirectory(t *testing.T) {
	_, home := setupProject(t)
	t.Chdir(home)

	_, _, code := runCLI(t, "setup")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertNothingWritten(t, home)
}

// A settings file setup cannot parse belongs to the user. Rewriting it would
// destroy whatever they meant, so each of these is refused with exit 1, the
// file is left byte-identical, and the skill is not written either: both files
// are planned before either is touched.
func TestSetupRefusesSettingsItCannotMerge(t *testing.T) {
	cases := map[string]string{
		"empty file":                "",
		"not json":                  "model = opus\n",
		"an array":                  "[]\n",
		"a string":                  `"settings"`,
		"trailing data":             `{"model":"opus"} {}`,
		"truncated":                 `{"model":`,
		"hooks is a list":           `{"hooks":[]}`,
		"hooks is a string":         `{"hooks":"llm-filesystem"}`,
		"hooks is null":             `{"hooks":null}`,
		"SessionStart is an object": `{"hooks":{"SessionStart":{}}}`,
		"SessionStart is null":      `{"hooks":{"SessionStart":null}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := setupProject(t)
			path := filepath.Join(dir, setupSettingsRel)
			writeFile(t, path, content)

			_, _, code := runCLI(t, "setup")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			if got := readOrEmpty(t, path); got != content {
				t.Errorf("settings.json changed:\n%s", got)
			}
			assertNothingWritten(t, filepath.Join(dir, ".claude"), "settings.json")
		})
	}
}

// A .claude directory linked to ~/.claude, or a skills directory linked
// elsewhere, would turn a project setup into a write somewhere else. Each link
// that leaves the working directory is refused with nothing written on either
// side of it.
func TestSetupRefusesASymlinkLeadingOutOfTheProject(t *testing.T) {
	cases := map[string]struct {
		link   string // relative to the project
		target func(outside string) string
	}{
		".claude":                             {".claude", func(o string) string { return o }},
		".claude/skills/llm-filesystem":       {filepath.Join(".claude", "skills", "llm-filesystem"), func(o string) string { return o }},
		".claude/settings.json":               {setupSettingsRel, func(o string) string { return filepath.Join(o, "settings.json") }},
		".codex":                              {".codex", func(o string) string { return o }},
		".codex/hooks.json":                   {setupCodexRel, func(o string) string { return filepath.Join(o, "hooks.json") }},
		".opencode":                           {".opencode", func(o string) string { return o }},
		".opencode/plugins":                   {filepath.Join(".opencode", "plugins"), func(o string) string { return o }},
		".opencode/plugins/llm-filesystem.js": {setupOpenCodeRel, func(o string) string { return filepath.Join(o, "llm-filesystem.js") }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, home := setupProject(t)
			outside := t.TempDir()
			link := filepath.Join(dir, c.link)
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(c.target(outside), link); err != nil {
				t.Fatal(err)
			}

			_, _, code := runCLI(t, "setup")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			assertNothingWritten(t, outside)
			assertNothingWritten(t, home)
			if _, err := os.Lstat(filepath.Join(dir, setupSettingsRel)); err == nil && c.link != setupSettingsRel {
				t.Error("settings.json was written although the skill path was refused")
			}
		})
	}
}

// A link that stays inside the project is the user's own layout, not an
// escape, and must keep working.
func TestSetupFollowsASymlinkInsideTheProject(t *testing.T) {
	dir, _ := setupProject(t)
	if err := os.Mkdir(filepath.Join(dir, "dotclaude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dotclaude", filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runCLI(t, "setup"); code != -1 {
		t.Fatalf("exit %d, want 0", code)
	}
	if readOrEmpty(t, filepath.Join(dir, "dotclaude", "skills", "llm-filesystem", "SKILL.md")) != skill.Content {
		t.Error("the skill was not written through the in-project link")
	}
}

// Each file is written to a temporary file in its own directory and renamed
// over the target, so a crash leaves either the old file or the new one, never
// half of each. The recorder proves the rename happens, and from where.
func TestSetupWritesEachFileByRename(t *testing.T) {
	dir, _ := setupProject(t)
	type move struct{ from, to string }
	var moves []move
	prev := renameFile
	renameFile = func(from, to string) error {
		moves = append(moves, move{from, to})
		return prev(from, to)
	}
	t.Cleanup(func() { renameFile = prev })

	if _, _, code := runCLI(t, "setup"); code != -1 {
		t.Fatalf("exit %d", code)
	}
	// macOS reports t.TempDir() under /var, a symlink to /private/var; the
	// rename sees the resolved path, so compare against that.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(real, setupSettingsRel), filepath.Join(real, setupSkillRel), filepath.Join(real, setupCodexRel), filepath.Join(real, setupOpenCodeRel)}
	if len(moves) != len(want) {
		t.Fatalf("renames = %v, want one per file", moves)
	}
	for i, m := range moves {
		if m.to != want[i] {
			t.Errorf("rename %d went to %s, want %s", i, m.to, want[i])
		}
		if filepath.Dir(m.from) != filepath.Dir(m.to) || m.from == m.to {
			t.Errorf("rename %d came from %s, want a temporary file beside the target", i, m.from)
		}
	}
	assertNothingWritten(t, filepath.Join(dir, ".claude"), "settings.json", "skills")
	assertNothingWritten(t, filepath.Join(dir, ".claude", "skills", "llm-filesystem"), "SKILL.md")
	assertNothingWritten(t, filepath.Join(dir, ".codex"), "hooks.json")
	assertNothingWritten(t, filepath.Join(dir, ".opencode", "plugins"), "llm-filesystem.js")
}

// A failure on the second file must not hide that the first was written: the
// user now has a hook and no skill, and has to be told exactly that. The run
// exits 1 and leaves no temporary file behind.
func TestSetupReportsTheFirstFileWrittenWhenTheSecondFails(t *testing.T) {
	dir, _ := setupProject(t)
	prev := renameFile
	renameFile = func(from, to string) error {
		if filepath.Base(to) == "SKILL.md" {
			return errors.New("disk full")
		}
		return prev(from, to)
	}
	t.Cleanup(func() { renameFile = prev })

	doc, code := runSetupJSON(t)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if doc.Success {
		t.Error("success is true after a failed write")
	}
	if got := statusOf(doc, setupSettingsRel); got != "written" {
		t.Errorf("settings.json reported %q, want written", got)
	}
	if got := statusOf(doc, setupSkillRel); got != "failed" {
		t.Errorf("SKILL.md reported %q, want failed", got)
	}
	for _, f := range doc.Files {
		if f.Path == setupSkillRel && !strings.Contains(f.Error, "disk full") {
			t.Errorf("SKILL.md error = %q, want the cause", f.Error)
		}
	}
	if cmds := sessionStartCommands(t, readOrEmpty(t, filepath.Join(dir, setupSettingsRel))); len(cmds) != 1 {
		t.Errorf("settings.json was not actually written: %q", cmds)
	}
	assertNothingWritten(t, filepath.Join(dir, ".claude", "skills", "llm-filesystem"))
}

// When the first file fails, the second is not attempted, and the report says
// so rather than leaving it out.
func TestSetupStopsAfterTheFirstFileFails(t *testing.T) {
	dir, _ := setupProject(t)
	prev := renameFile
	renameFile = func(from, to string) error { return errors.New("read-only file system") }
	t.Cleanup(func() { renameFile = prev })

	doc, code := runSetupJSON(t)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if got := statusOf(doc, setupSettingsRel); got != "failed" {
		t.Errorf("settings.json reported %q, want failed", got)
	}
	if got := statusOf(doc, setupSkillRel); got != "skipped" {
		t.Errorf("SKILL.md reported %q, want skipped", got)
	}
	if _, err := os.Stat(filepath.Join(dir, setupSkillRel)); err == nil {
		t.Error("SKILL.md was written after settings.json failed")
	}
	assertNothingWritten(t, filepath.Join(dir, ".claude"))
}

// setup takes no arguments. An argument is a usage error, so it exits 2 and
// writes nothing, rather than being ignored.
func TestSetupRejectsArguments(t *testing.T) {
	dir, _ := setupProject(t)
	if _, _, code := runCLI(t, "setup", "somewhere"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	assertNothingWritten(t, dir)
}

// A file mode the user chose, such as 0600 on a settings file holding tokens,
// survives the merge.
func TestSetupKeepsTheSettingsFileMode(t *testing.T) {
	dir, _ := setupProject(t)
	path := filepath.Join(dir, setupSettingsRel)
	writeFile(t, path, `{}`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, code := runCLI(t, "setup"); code != -1 {
		t.Fatalf("exit %d", code)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

// Debt a502b329: a SKILL.md that is a symlink to a file inside the project, with
// different content, was silently replaced by a regular file — the rename
// clobbers the link while the file it pointed to is left stale. That reports
// "written" for a write that did not happen where the user's layout said it
// would. The decision recorded here: REFUSE. Writing through the link would mean
// setup editing a file it did not plan for; replacing it means the user's link
// layout is destroyed. Both are surprises, so neither happens: exit 1, the link
// and its target untouched. The same rule covers a symlinked settings.json, the
// other file written by rename.
func TestSetupRefusesToReplaceASymlinkedTargetFile(t *testing.T) {
	cases := map[string]struct {
		link      string // relative to the project
		targetRel string // relative to the project, inside it
		content   string
	}{
		"SKILL.md":          {setupSkillRel, filepath.Join("own-skill.md"), "the user's own skill"},
		"settings.json":     {setupSettingsRel, filepath.Join("own-settings.json"), "{}"},
		"llm-filesystem.js": {setupOpenCodeRel, filepath.Join("own-plugin.js"), "export const Mine = async () => ({})\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir, _ := setupProject(t)
			target := filepath.Join(dir, c.targetRel)
			writeFile(t, target, c.content)
			link := filepath.Join(dir, c.link)
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}

			stdout, _, code := runCLI(t, "setup", "--format", "json")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			if !strings.Contains(stdout, "symlink") {
				t.Errorf("the refusal must name the symlink, got %q", stdout)
			}
			// Side effects, not exit codes: the link must still be a link, and
			// the file it points to must be byte-identical.
			if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s is no longer a symlink", c.link)
			}
			if got := readOrEmpty(t, target); got != c.content {
				t.Errorf("the link target changed:\n%s", got)
			}
			if got := readOrEmpty(t, link); got != c.content {
				t.Errorf("reading through the link changed:\n%s", got)
			}
		})
	}
}

// Debt 88b33e5b: checkInsideProject ran before anything was written, but
// nothing stopped a directory in the path being swapped for a symlink between
// that check and the rename — the check-to-use race. A write could then land
// outside the project while every check had passed. writeFileByRename now
// re-checks after CreateTemp, because the temporary file's directory is where
// the rename will land. This exercises the re-check directly, since the race
// itself cannot be scheduled from a test: .claude is already a symlink out of
// the project by the time the write runs, so CreateTemp put the temporary file
// outside — the re-check must refuse, remove that temporary file, and write
// nothing on either side.
func TestSetupRechecksTheWriteDirectoryAfterCreatingTheTempFile(t *testing.T) {
	dir, _ := setupProject(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	err = writeFileByRename(real, filepath.Join(real, ".claude", "settings.json"), []byte("{}"), 0o644)
	if err == nil {
		t.Fatal("the write succeeded although the target directory had been swapped for a symlink out of the project")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("error = %q, want it to name the escape", err.Error())
	}
	assertNothingWritten(t, outside) // includes the temporary file
}
