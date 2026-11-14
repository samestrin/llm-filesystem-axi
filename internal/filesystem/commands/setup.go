package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/samestrin/llm-filesystem-axi/integrations/claude-code/skill"
	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

// setupHookCommand is what Claude Code runs at session start. Its stdout joins
// the session's context, so it is the bare run: the home view. The owner chose
// the bare run over a routing-only summary (epic 05, question 1).
const setupHookCommand = "llm-filesystem"

// setupExecutable is indirected so a test can decide where "this binary" lives;
// under go test the real one is the test binary.
var setupExecutable = os.Executable

// renameFile is indirected so a test can fail the second of the two writes,
// which no real filesystem does on demand.
var renameFile = os.Rename

// Statuses a file can be reported with.
const (
	setupWritten    = "written"
	setupUnchanged  = "unchanged"
	setupWouldWrite = "would-write"
	setupFailed     = "failed"
	setupSkipped    = "skipped" // not attempted, because an earlier file failed
)

type setupFile struct {
	Path   string `json:"path"` // relative to dir
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type setupResult struct {
	Dir     string      `json:"dir"`
	DryRun  bool        `json:"dry_run,omitempty"`
	Success bool        `json:"success"`
	Files   []setupFile `json:"files"`
}

// setupPlan is one file's planned content, worked out before anything is
// written.
type setupPlan struct {
	rel     string
	content []byte
	mode    fs.FileMode
	changed bool
}

func setupCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Register llm-filesystem with Claude Code and Codex in this project",
		Long: `Merge a SessionStart hook that runs llm-filesystem into .claude/settings.json,
write the Agent Skill to .claude/skills/llm-filesystem/SKILL.md, and merge a
SessionStart hook into .codex/hooks.json for Codex.

The Codex hook runs llm-filesystem by name when PATH finds this same binary,
and by its absolute path otherwise. A later run points an existing
llm-filesystem hook in .codex/hooks.json at the binary's current location, so
a moved or reinstalled binary is repaired rather than registered twice.

Every other key and hook in either file is kept. A file that is not a JSON
object, or whose "hooks" or "hooks.SessionStart" has the wrong type, is
refused and left as it is.

Only the working directory is written to. The user-wide ~/.claude and ~/.codex
are never touched: running setup in the home directory is refused, and so is a
.claude or .codex symlink that leads out of the project. Every file is planned
before any is written, and each is written to a temporary file and renamed into
place.

Codex (checked on 0.166.0, where the hooks feature is on by default) reads
.codex/hooks.json only in a project it trusts, and lists a new or changed hook
as untrusted until you review it. Session-end capture is not set up.

Safe to run again: a second run changes no bytes and says so. Never prompts.

Examples:
  llm-filesystem setup --dry-run
  llm-filesystem setup`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cwd, err := getwd()
			if err != nil {
				setupRefused(fmt.Errorf("cannot determine the working directory: %w", err))
				return
			}
			dir, err := setupProjectDir(cwd)
			if err != nil {
				setupRefused(err)
				return
			}
			plans, err := planSetup(dir)
			if err != nil {
				setupRefused(err)
				return
			}

			res := applySetup(dir, plans, dryRun)
			OutputResultAXI(res, nil, func() []string { return setupSteps(res) }, func() string {
				return setupText(res)
			})

			// The result first, so the caller learns which file did get
			// written; then exit 1, because a half-done setup is not a success.
			if !res.Success {
				exitFunc(int(goaxi.ExitError))
				return
			}
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be written, and write nothing")
	return cmd
}

// setupRefused reports a refusal made before anything was written.
func setupRefused(err error) {
	emitDiagnostic(activeFmt, activeCompact, err, goaxi.ExitError, []string{
		"Nothing was written. Fix the problem above, then preview: llm-filesystem setup --dry-run",
		"Check the flags and what they mean: llm-filesystem setup --help",
	})
}

// setupProjectDir resolves the working directory's symlinks and checks the
// result, which is where setup writes, against --allowed-dirs and the home
// directory. Checking the path as given was not enough: a link inside the
// sandbox can lead out of it.
func setupProjectDir(cwd string) (string, error) {
	dir, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the working directory: %w", err)
	}
	// The sandbox roots are resolved too, so one that is itself reached
	// through a link (macOS's /var is /private/var) still contains its own
	// directories.
	allowed := GetAllowedDirs()
	for i, a := range allowed {
		if r, err := filepath.EvalSymlinks(a); err == nil {
			allowed[i] = r
		}
	}
	if err := core.ValidatePath(dir, allowed); err != nil {
		return "", fmt.Errorf("working directory %s: %w", cwd, err)
	}
	// In the home directory, .claude/settings.json is the user-wide settings
	// file, which setup never edits.
	if home, err := os.UserHomeDir(); err == nil {
		if h, err := filepath.EvalSymlinks(home); err == nil && h == dir {
			return "", fmt.Errorf("%s is the home directory; setup writes project settings only, never ~/.claude", cwd)
		}
	}
	return dir, nil
}

// planSetup works out every file without writing any, so a hooks file that
// cannot be merged or a link that leads out of the project fails the run before
// anything has changed.
func planSetup(dir string) ([]setupPlan, error) {
	settingsRel := filepath.Join(".claude", "settings.json")
	skillRel := filepath.Join(".claude", "skills", "llm-filesystem", "SKILL.md")
	codexRel := filepath.Join(".codex", "hooks.json")
	for _, rel := range []string{settingsRel, skillRel, codexRel} {
		if err := checkInsideProject(dir, filepath.Join(dir, rel)); err != nil {
			return nil, err
		}
		// The target file itself must not be a symlink. The write is a rename,
		// which would replace the link with a regular file and leave whatever it
		// pointed to untouched — a "written" that did not happen where the user's
		// layout says it did (debt a502b329). Writing through the link instead
		// would edit a file setup never planned for, so the decision is to refuse
		// and let the user resolve their own layout. An in-project link such as
		// .claude -> dotclaude is a directory and still followed.
		if info, err := os.Lstat(filepath.Join(dir, rel)); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink; refusing to overwrite the link or write through it — remove or repoint it, then run setup again", rel)
		}
	}

	settings, err := planHooksFile(filepath.Join(dir, settingsRel), setupHookCommand, false)
	if err != nil {
		return nil, err
	}
	settings.rel = settingsRel

	codexCommand, err := setupPortableCommand()
	if err != nil {
		return nil, err
	}
	codex, err := planHooksFile(filepath.Join(dir, codexRel), codexCommand, true)
	if err != nil {
		return nil, err
	}
	codex.rel = codexRel

	skillPlan := setupPlan{rel: skillRel, content: []byte(skill.Content), mode: 0o644}
	existing, err := os.ReadFile(filepath.Join(dir, skillRel))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		skillPlan.changed = true
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", skillRel, err)
	default:
		skillPlan.changed = !bytes.Equal(existing, skillPlan.content)
		skillPlan.mode = fileMode(filepath.Join(dir, skillRel))
	}

	return []setupPlan{settings, skillPlan, codex}, nil
}

// setupPortableCommand is the hook command for the running binary: its bare
// name when PATH finds this same file, so a global install survives an upgrade
// in place, and its absolute path otherwise, so the hook never runs a different
// llm-filesystem that PATH happens to find first. The path is not resolved
// through symlinks: a package manager's link outlives the versioned file it
// points at.
func setupPortableCommand() (string, error) {
	exe, err := setupExecutable()
	if err == nil {
		exe, err = filepath.Abs(exe)
	}
	if err != nil {
		return "", fmt.Errorf("cannot find this binary's own path for the hook command: %w", err)
	}
	if found, err := exec.LookPath(setupHookCommand); err == nil {
		a, errA := os.Stat(found)
		b, errB := os.Stat(exe)
		if errA == nil && errB == nil && os.SameFile(a, b) {
			return setupHookCommand, nil
		}
	}
	return shellQuote(exe), nil
}

// shellQuote single-quotes a path the shell running the hook would otherwise
// split or expand, such as one with a space in it.
func shellQuote(s string) string {
	unsafe := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+:,@%=", r))
	})
	if s != "" && unsafe < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// isSetupHookCommand reports whether a hook command is one setup wrote: the
// bare name, or an absolute path to a file named llm-filesystem, possibly
// single-quoted. Path repair rewrites only these, so a user's own hook that
// merely mentions the tool is never touched.
func isSetupHookCommand(cmd string) bool {
	if cmd == setupHookCommand {
		return true
	}
	if n := len(cmd); n >= 2 && cmd[0] == '\'' && cmd[n-1] == '\'' {
		cmd = cmd[1 : n-1]
		if strings.Contains(cmd, "'") {
			return false
		}
	}
	return filepath.IsAbs(cmd) && strings.TrimSuffix(filepath.Base(cmd), ".exe") == setupHookCommand
}

// applySetup writes the planned files in order. After a failure the rest are
// not attempted, and every file is still reported, so the caller knows exactly
// which ones changed.
func applySetup(dir string, plans []setupPlan, dryRun bool) setupResult {
	res := setupResult{Dir: dir, DryRun: dryRun, Success: true, Files: []setupFile{}}
	for _, p := range plans {
		f := setupFile{Path: p.rel, Status: setupUnchanged}
		switch {
		case !p.changed:
		case dryRun:
			f.Status = setupWouldWrite
		case !res.Success:
			f.Status = setupSkipped
		default:
			if err := writeFileByRename(dir, filepath.Join(dir, p.rel), p.content, p.mode); err != nil {
				f.Status, f.Error = setupFailed, err.Error()
				res.Success = false
			} else {
				f.Status = setupWritten
			}
		}
		res.Files = append(res.Files, f)
	}
	return res
}

// writeFileByRename writes content to a temporary file beside path and renames
// it into place, so a reader sees the old file or the new one, never a mix.
//
// dir is the resolved project directory, re-checked after the temporary file
// exists: checkInsideProject ran before anything was written, and nothing
// stopped a directory in the path being swapped for a symlink in between — the
// check-to-use race (debt 88b33e5b). The temporary file's directory is where
// the rename would land, so checking it there closes the window the plan left.
func writeFileByRename(dir, path string, content []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	name := tmp.Name()
	// The re-check: the directory the rename will land in is the directory the
	// temporary file just went into. If a component of it was swapped for a
	// symlink leading out of the project since the plan, refuse now, and the
	// os.Remove below takes the temporary file with it.
	if err := checkInsideProject(dir, filepath.Dir(path)); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("write %s: %w", path, err)
	}
	_, err = tmp.Write(content)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = renameFile(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// fileMode is path's permission bits, so a merge keeps a mode the user chose.
func fileMode(path string) fs.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}

// checkInsideProject refuses a target that a symlink would send outside dir,
// such as a .claude directory linked to ~/.claude. It resolves the deepest
// part of target that exists; the rest does not exist yet and is created as
// plain directories.
func checkInsideProject(dir, target string) error {
	existing, rest := target, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", existing, err)
	}
	resolved = filepath.Join(resolved, rest)
	if !strings.HasPrefix(resolved, dir+string(filepath.Separator)) {
		return fmt.Errorf("%s resolves to %s, outside the working directory; refusing to write through it", target, resolved)
	}
	return nil
}

// planHooksFile computes a merged hooks file (Claude Code's settings.json or
// Codex's hooks.json, which share the hooks layout) without writing. Values are
// kept as the raw JSON they were read as, so key order, number literals and
// string escapes survive; only whitespace is normalised, and only when the file
// has to change at all. When the hook is already registered the file is left
// byte for byte alone. With repair, a hook setup wrote earlier that names a
// different command is rewritten in place to name command.
func planHooksFile(path, command string, repair bool) (setupPlan, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return setupPlan{}, fmt.Errorf("read %s: %w", path, err)
	}
	exists := err == nil

	var root jsonObject
	if exists {
		if root, err = parseJSONObject(raw); err != nil {
			return setupPlan{}, fmt.Errorf("%s: %w; refusing to overwrite", path, err)
		}
	}

	var hooks jsonObject
	if v, ok := root.get("hooks"); ok {
		if hooks, err = parseJSONObject(v); err != nil {
			return setupPlan{}, fmt.Errorf(`%s: "hooks": %w; refusing to overwrite`, path, err)
		}
	}
	var groups []json.RawMessage
	if v, ok := hooks.get("SessionStart"); ok {
		if err := json.Unmarshal(v, &groups); err != nil || groups == nil {
			return setupPlan{}, fmt.Errorf(`%s: "hooks.SessionStart" is not a list; refusing to overwrite`, path)
		}
	}
	repaired := repair && repairSessionStart(groups, command)
	registered := hasSessionStartCommand(groups, command)
	if registered && !repaired {
		return setupPlan{content: raw, mode: fileMode(path)}, nil
	}

	if !registered {
		hook := jsonObject{}
		hook.set("type", jsonString("command"))
		hook.set("command", jsonString(command))
		group := jsonObject{}
		group.set("hooks", joinJSONList([]json.RawMessage{hook.encode()}))
		groups = append(groups, group.encode())
	}
	hooks.set("SessionStart", joinJSONList(groups))
	root.set("hooks", hooks.encode())

	var out bytes.Buffer
	if err := json.Indent(&out, root.encode(), "", "  "); err != nil {
		return setupPlan{}, err
	}
	out.WriteByte('\n')

	mode := fs.FileMode(0o644)
	if exists {
		mode = fileMode(path)
	}
	return setupPlan{content: out.Bytes(), mode: mode, changed: true}, nil
}

// repairSessionStart rewrites, in place, every SessionStart hook setup wrote
// that names a command other than want, keeping the hook's other fields and
// every other hook as they were. It reports whether anything changed.
func repairSessionStart(groups []json.RawMessage, want string) bool {
	repaired := false
	for i, g := range groups {
		group, err := parseJSONObject(g)
		if err != nil {
			continue
		}
		var hooks []json.RawMessage
		if v, ok := group.get("hooks"); !ok || json.Unmarshal(v, &hooks) != nil {
			continue
		}
		changed := false
		for j, h := range hooks {
			hook, err := parseJSONObject(h)
			if err != nil {
				continue
			}
			var cmd string
			if v, ok := hook.get("command"); !ok || json.Unmarshal(v, &cmd) != nil {
				continue
			}
			if cmd == want || !isSetupHookCommand(cmd) {
				continue
			}
			hook.set("command", jsonString(want))
			hooks[j] = hook.encode()
			changed = true
		}
		if changed {
			group.set("hooks", joinJSONList(hooks))
			groups[i] = group.encode()
			repaired = true
		}
	}
	return repaired
}

// joinJSONList joins raw values into a JSON list by hand: json.Marshal would
// HTML-escape the user's existing hooks, turning a command such as
// "echo hi >&2" into "echo hi >&2".
func joinJSONList(items []json.RawMessage) json.RawMessage {
	var list bytes.Buffer
	list.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			list.WriteByte(',')
		}
		list.Write(item)
	}
	list.WriteByte(']')
	return list.Bytes()
}

// jsonString encodes s without HTML escaping, so a command keeps its
// characters as written.
func jsonString(s string) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")) // Encode appends a newline
}

func hasSessionStartCommand(groups []json.RawMessage, command string) bool {
	for _, g := range groups {
		var group struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if json.Unmarshal(g, &group) != nil {
			continue
		}
		for _, h := range group.Hooks {
			if h.Command == command {
				return true
			}
		}
	}
	return false
}

// jsonObject is a JSON object that remembers its key order. encoding/json
// sorts map keys, which would reorder a user's settings file on every merge.
type jsonObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseJSONObject(raw []byte) (jsonObject, error) {
	obj := jsonObject{vals: map[string]json.RawMessage{}}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return obj, errors.New("not a JSON object")
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return obj, errors.New("not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return obj, fmt.Errorf("invalid JSON: %w", err)
		}
		key := tok.(string) // inside an object, the decoder only yields string keys
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return obj, fmt.Errorf("invalid JSON: %w", err)
		}
		obj.set(key, v)
	}
	if _, err := dec.Token(); err != nil {
		return obj, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return obj, errors.New("trailing data after the JSON object")
	}
	return obj, nil
}

func (o jsonObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

func (o *jsonObject) set(key string, v json.RawMessage) {
	if o.vals == nil {
		o.vals = map[string]json.RawMessage{}
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// encode writes the object compactly; json.Indent lays it out afterwards. Keys
// are re-marshalled without HTML escaping; values are copied verbatim.
func (o jsonObject) encode() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(jsonString(k))
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

func setupSteps(res setupResult) []string {
	count := map[string]int{}
	for _, f := range res.Files {
		count[f.Status]++
	}
	switch {
	case !res.Success:
		return []string{
			"Files reported written are in place; fix the error on the failed one, then run again: llm-filesystem setup",
		}
	case count[setupWouldWrite] > 0:
		return []string{"Write these files: llm-filesystem setup"}
	case count[setupWritten] > 0:
		return []string{
			"Start a new Claude Code or Codex session in this directory to load the hooks and the skill",
			"Codex runs the hook only in a project it trusts, once you review the new hook",
			"See what the hook will print: llm-filesystem",
		}
	default:
		return []string{"Already set up; see what the hook prints at session start: llm-filesystem"}
	}
}

func setupText(res setupResult) string {
	var sb strings.Builder
	for _, f := range res.Files {
		sb.WriteString(f.Status + " " + f.Path)
		if f.Error != "" {
			sb.WriteString(": " + f.Error)
		}
		sb.WriteByte('\n')
	}
	switch {
	case !res.Success:
		sb.WriteString("setup did not finish in " + res.Dir)
	case res.DryRun:
		sb.WriteString("dry run: nothing written in " + res.Dir)
	default:
		sb.WriteString("llm-filesystem is set up in " + res.Dir)
	}
	return sb.String()
}
