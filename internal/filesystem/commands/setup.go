package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
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
		Short: "Register llm-filesystem with Claude Code in this project",
		Long: `Merge a SessionStart hook that runs llm-filesystem into .claude/settings.json,
and write the Agent Skill to .claude/skills/llm-filesystem/SKILL.md.

Every other key and hook in settings.json is kept. A settings.json that is not
a JSON object, or whose "hooks" or "hooks.SessionStart" has the wrong type, is
refused and left as it is.

Only the working directory is written to. The user-wide ~/.claude is never
touched: running setup in the home directory is refused, and so is a .claude
symlink that leads out of the project. Both files are planned before either is
written, and each is written to a temporary file and renamed into place.

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

// planSetup works out both files without writing either, so a settings file
// that cannot be merged or a link that leads out of the project fails the run
// before anything has changed.
func planSetup(dir string) ([]setupPlan, error) {
	settingsRel := filepath.Join(".claude", "settings.json")
	skillRel := filepath.Join(".claude", "skills", "llm-filesystem", "SKILL.md")
	for _, rel := range []string{settingsRel, skillRel} {
		if err := checkInsideProject(dir, filepath.Join(dir, rel)); err != nil {
			return nil, err
		}
	}

	settings, err := planSettings(filepath.Join(dir, settingsRel))
	if err != nil {
		return nil, err
	}
	settings.rel = settingsRel

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

	return []setupPlan{settings, skillPlan}, nil
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
			if err := writeFileByRename(filepath.Join(dir, p.rel), p.content, p.mode); err != nil {
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
func writeFileByRename(path string, content []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	name := tmp.Name()
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

// planSettings computes the merged settings without writing. Values are kept as
// the raw JSON they were read as, so key order, number literals and string
// escapes survive; only whitespace is normalised, and only when the file has to
// change at all. When the hook is already registered the file is left byte for
// byte alone.
func planSettings(path string) (setupPlan, error) {
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
	if hasSessionStartCommand(groups, setupHookCommand) {
		return setupPlan{content: raw, mode: fileMode(path)}, nil
	}

	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	group, _ := json.Marshal(map[string][]hook{"hooks": {{Type: "command", Command: setupHookCommand}}})
	groups = append(groups, group)
	// Joined by hand: json.Marshal would HTML-escape the user's existing hooks,
	// turning a command such as "echo hi >&2" into "echo hi >&2".
	var list bytes.Buffer
	list.WriteByte('[')
	for i, g := range groups {
		if i > 0 {
			list.WriteByte(',')
		}
		list.Write(g)
	}
	list.WriteByte(']')
	hooks.set("SessionStart", list.Bytes())
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
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(k)
		b.Truncate(b.Len() - 1) // Encode appends a newline
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
			"Start a new Claude Code session in this directory to load the hook and the skill",
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
