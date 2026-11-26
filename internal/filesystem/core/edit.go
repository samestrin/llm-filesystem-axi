package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// EditResult represents the result of an edit operation
// Unmatched counts edits that found no target. It is a field rather than only a
// sentence in Message because a caller must be able to detect a partial result
// without parsing prose — the whole reason this tool emits structured output.
// DryRun marks a preview in every format, the way SyncResult does; a marker in
// the TEXT renderer alone leaves TOON and JSON unable to tell a preview from an
// applied edit.
type EditResult struct {
	Path         string `json:"path"`
	Success      bool   `json:"success"`
	Changes      int    `json:"changes"`
	Unmatched    int    `json:"unmatched,omitempty"`
	Backup       string `json:"backup,omitempty"`
	Message      string `json:"message"`
	OldContent   string `json:"old_content,omitempty"`
	NewContent   string `json:"new_content,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
	Replacements int    `json:"replacements,omitempty"`
	InPlace      bool   `json:"in_place,omitempty"`
}

// AmbiguousMatchError refuses an edit whose old string occurs more than once.
// The edits used to run strings.Replace(..., 1) after strings.Contains, so the
// FIRST occurrence changed silently and the edit reported success; which one
// the caller meant is unknowable, to the caller and to the tool alike.
//
// Edit is the 1-based position of the edit in a multi-edit batch, or 0 for a
// single edit. Field names the input the caller must lengthen.
type AmbiguousMatchError struct {
	Edit    int
	Field   string
	Matches int
}

func (e *AmbiguousMatchError) Error() string {
	msg := fmt.Sprintf("%s matches %d times; it must match exactly once", e.Field, e.Matches)
	if e.Edit > 0 {
		msg = fmt.Sprintf("edit %d: %s", e.Edit, msg)
	}
	return msg
}

// ErrNoMatch marks an edit that changed nothing because its target was not in
// the file. Callers test for it with errors.Is to suggest reading the file's
// current text; the messages themselves are unchanged.
var ErrNoMatch = errors.New("no edit matched")

// noMatch carries an unmatched-edit message and matches ErrNoMatch.
type noMatch string

func (e noMatch) Error() string        { return string(e) }
func (e noMatch) Is(target error) bool { return target == ErrNoMatch }

// replaceMatches replaces old in content: exactly one occurrence, or every
// occurrence under replaceAll. It returns the number replaced, 0 when old does
// not occur, and an *AmbiguousMatchError when it occurs more than once without
// replaceAll.
func replaceMatches(content, old, new string, replaceAll bool, edit int, field string) (string, int, error) {
	n := strings.Count(content, old)
	switch {
	case n == 0:
		return content, 0, nil
	case n > 1 && !replaceAll:
		return content, 0, &AmbiguousMatchError{Edit: edit, Field: field, Matches: n}
	}
	return strings.ReplaceAll(content, old, new), n, nil
}

// The temp file's write, sync, chown, chmod and close, and the in-place write,
// are variables so a test can make each fail after the temp file exists, which
// no real test filesystem does.
var (
	writeTempFile = func(f *os.File, b []byte) (int, error) { return f.Write(b) }
	syncTempFile  = func(f *os.File) error { return f.Sync() }
	chownTempFile = func(f *os.File, uid, gid int) error { return f.Chown(uid, gid) }
	chmodTempFile = func(f *os.File, mode fs.FileMode) error { return f.Chmod(mode) }
	closeTempFile = func(f *os.File) error { return f.Close() }
	// syncInPlaceFile is the in-place write's fsync, separate so a test can
	// fail it after the file has been truncated and rewritten.
	syncInPlaceFile = func(f *os.File) error { return f.Sync() }
	// writeInPlace reports whether it truncated the file before failing.
	writeInPlace = func(path string, data []byte) (truncated bool, err error) {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return false, err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return true, err
		}
		if err := syncInPlaceFile(f); err != nil {
			_ = f.Close()
			return true, err
		}
		return true, f.Close()
	}
)

// writeFileAtomic replaces path with data, keeping mode and, where permitted,
// owner. inPlace is empty when the write was atomic, and otherwise says why it
// was not.
//
// Every edit used os.WriteFile, which truncates first and writes second: a
// crash or a full disk part-way left a truncated file where the original was.
// This writes a temp file beside the target, fsyncs it, sets the owner and
// mode and renames it into place, so a reader sees the old bytes or the new
// ones and never a mix. A symlink is resolved first: renaming over the link
// itself would replace it with a regular file and leave its target unedited.
//
// Renaming cannot keep a hard link: the edited name gets a new file and every
// other name keeps the old bytes. Nor can it work in a directory the caller
// cannot write, though the file itself is writable. In both cases the new
// bytes are still written to a temp file and fsynced first, then written over
// the original in place, and the reason is returned so the caller can say the
// write was not atomic (EST-8805). If that in-place write fails, the temp copy
// is kept and named in the error, since the original may now be partial.
func writeFileAtomic(path string, data []byte, mode fs.FileMode) (inPlace string, err error) {
	if resolved, rerr := filepath.EvalSymlinks(path); rerr == nil {
		path = resolved
	}
	info, statErr := os.Stat(path)
	exists := statErr == nil && info.Mode().IsRegular()
	if exists {
		if n := linkCount(path, info); n > 1 {
			inPlace = fmt.Sprintf("the file has %d hard links", n)
		}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".llmfs-edit-*")
	if err != nil && exists && errors.Is(err, fs.ErrPermission) {
		if inPlace == "" {
			inPlace = "its directory is not writable"
		}
		tmp, err = os.CreateTemp("", ".llmfs-edit-*")
	}
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	keepTemp := false
	defer func() {
		if err != nil {
			_ = tmp.Close()
			if !keepTemp {
				_ = os.Remove(tmpName)
			}
		}
	}()
	if _, err = writeTempFile(tmp, data); err != nil {
		return "", err
	}
	if err = syncTempFile(tmp); err != nil {
		return "", err
	}

	if inPlace != "" {
		if err = closeTempFile(tmp); err != nil {
			return "", err
		}
		var truncated bool
		if truncated, err = writeInPlace(path, data); err != nil {
			if !truncated {
				return "", fmt.Errorf("writing in place because %s: %w", inPlace, err)
			}
			keepTemp = true
			return "", fmt.Errorf("writing in place because %s: %w; %s may be partial, and the new content is in %s", inPlace, err, path, tmpName)
		}
		_ = os.Remove(tmpName)
		return inPlace, nil
	}

	// Best effort: only root may give a file away, so an unprivileged edit of
	// someone else's file ends up owned by the editor, as a rename always made
	// it. Chown comes before Chmod because it clears setuid and setgid.
	if exists {
		if uid, gid, ok := fileOwner(info); ok {
			_ = chownTempFile(tmp, uid, gid)
		}
	}
	// CreateTemp makes the file 0600; Chmod is not narrowed by umask, so the
	// result has exactly the original's mode.
	if err = chmodTempFile(tmp, mode); err != nil {
		return "", err
	}
	if err = closeTempFile(tmp); err != nil {
		return "", err
	}
	return "", os.Rename(tmpName, path)
}

// replaceFile rewrites an existing file atomically, keeping its mode. It
// returns why the write went in place, or "" when it was atomic.
func replaceFile(path string, data []byte) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return writeFileAtomic(path, data, fileMode(info))
}

// refuseEditThroughHardLinks is refuseHardLinks for an edit, which has the
// file's path but not its info.
func refuseEditThroughHardLinks(path, shown string, allowedDirs []string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return refuseHardLinks(path, shown, info, allowedDirs)
}

// inPlaceNote is the sentence an edit's message carries when its write was not
// atomic.
func inPlaceNote(reason string) string {
	return fmt.Sprintf("; written in place, not atomically, because %s", reason)
}

// fileMode is the part of a file's mode a rewrite must carry over.
func fileMode(info fs.FileInfo) fs.FileMode {
	return info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
}

// EditBlockOptions contains input parameters for EditBlock
type EditBlockOptions struct {
	ReplaceAll  bool
	Path        string
	OldString   string
	NewString   string
	AllowedDirs []string
}

// EditBlock replaces a single block of text in a file
func EditBlock(opts EditBlockOptions) (*EditResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}
	if opts.OldString == "" {
		return nil, usageErrorf("old_string is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := refuseEditThroughHardLinks(normalizedPath, opts.Path, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Read file content
	content, err := os.ReadFile(normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	newContent, n, err := replaceMatches(string(content), opts.OldString, opts.NewString, opts.ReplaceAll, 0, "old_string")
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, noMatch("old_string not found in file")
	}

	inPlace, err := replaceFile(normalizedPath, []byte(newContent))
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	result := &EditResult{
		Path:         normalizedPath,
		Success:      true,
		Changes:      1,
		Replacements: n,
		Message:      "Edit completed successfully",
	}
	if inPlace != "" {
		result.InPlace = true
		result.Message += inPlaceNote(inPlace)
	}
	return result, nil
}

// EditBlocksOptions contains input parameters for EditBlocks
type EditBlocksOptions struct {
	ReplaceAll  bool
	Path        string
	Edits       []EditPair
	AllowedDirs []string
}

// EditPair represents an old/new string pair for editing
type EditPair struct {
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// EditBlocks applies multiple edits to a single file
func EditBlocks(opts EditBlocksOptions) (*EditResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := refuseEditThroughHardLinks(normalizedPath, opts.Path, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Read file content
	content, err := os.ReadFile(normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(opts.Edits) == 0 {
		return nil, usageErrorf("edits is required")
	}

	contentStr := string(content)
	changes := 0
	unmatched := 0
	replacements := 0

	// EditBlock, the singular form, refuses both of these outright. This one
	// used to `continue` past them and then report "Applied 0 edits
	// successfully" — two sibling functions disagreeing about whether a no-op
	// is a success.
	//
	// The empty case is the one that bites through JSON: --edits decodes into
	// EditPair{old_string,new_string}, Go ignores unknown fields, so a payload
	// keyed {old,new} produces entirely empty edits and every one was skipped.
	for i, edit := range opts.Edits {
		if edit.OldString == "" {
			return nil, fmt.Errorf("edit %d: old_string is required", i+1)
		}

		// Checked against the content as the earlier edits left it, and one
		// ambiguous edit refuses the batch before anything is written.
		next, n, err := replaceMatches(contentStr, edit.OldString, edit.NewString, opts.ReplaceAll, i+1, "old_string")
		if err != nil {
			return nil, err
		}
		if n == 0 {
			unmatched++
			continue
		}

		contentStr = next
		changes++
		replacements += n
	}

	if changes == 0 {
		return nil, noMatch(fmt.Sprintf("old_string not found in file: none of %d edits matched", len(opts.Edits)))
	}

	// Only write when something changed. The previous version rewrote the file
	// even on a zero-change run, bumping its mtime for no reason.
	inPlace, err := replaceFile(normalizedPath, []byte(contentStr))
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// "Applied N of M" rather than "Applied N successfully": a partial result is
	// a success, but the caller has to be able to see it was partial without
	// reading prose.
	message := fmt.Sprintf("Applied %d of %d edits", changes, len(opts.Edits))
	if unmatched > 0 {
		message += fmt.Sprintf("; %d did not match", unmatched)
	}
	if inPlace != "" {
		message += inPlaceNote(inPlace)
	}

	return &EditResult{
		Path:         normalizedPath,
		Success:      true,
		Changes:      changes,
		Unmatched:    unmatched,
		Replacements: replacements,
		Message:      message,
		InPlace:      inPlace != "",
	}, nil
}

// SafeEditOptions contains input parameters for SafeEdit
type SafeEditOptions struct {
	ReplaceAll  bool
	Path        string
	OldString   string
	NewString   string
	Backup      bool
	DryRun      bool
	AllowedDirs []string
}

// SafeEdit performs a safe edit with backup and dry-run support
func SafeEdit(opts SafeEditOptions) (*EditResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}
	if opts.OldString == "" {
		return nil, usageErrorf("old_string is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := refuseEditThroughHardLinks(normalizedPath, opts.Path, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Read file content
	content, err := os.ReadFile(normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	contentStr := string(content)

	// Refused before the dry-run return too: a preview of a first-match edit
	// previews an edit the tool must not make.
	newContent, n, err := replaceMatches(contentStr, opts.OldString, opts.NewString, opts.ReplaceAll, 0, "old_string")
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, noMatch("old_string not found in file")
	}

	result := &EditResult{
		Path:         normalizedPath,
		Success:      true,
		Changes:      1,
		Replacements: n,
		OldContent:   contentStr,
		NewContent:   newContent,
		DryRun:       opts.DryRun,
	}

	if opts.DryRun {
		result.Message = "Dry run - no changes made"
		return result, nil
	}

	// Create backup if requested. A requested backup that failed must refuse
	// the edit: applying it would leave the caller without their safety copy.
	if opts.Backup {
		backupPath, err := createEditBackup(normalizedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create backup: %w", err)
		}
		result.Backup = backupPath
	}

	inPlace, err := replaceFile(normalizedPath, []byte(newContent))
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	result.Message = "Edit completed successfully"
	if inPlace != "" {
		result.InPlace = true
		result.Message += inPlaceNote(inPlace)
	}
	return result, nil
}

func createEditBackup(path string) (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.bak.%s", path, timestamp)

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// The original's mode, not 0644: a 0600 secret's backup was world-readable.
	if _, err := writeFileAtomic(backupPath, content, fileMode(info)); err != nil {
		return "", err
	}

	return backupPath, nil
}

// EditFileOptions contains input parameters for EditFile
type EditFileOptions struct {
	Path        string
	Operation   string
	Line        int
	Content     string
	AllowedDirs []string
}

// EditFile performs line-based file editing
func EditFile(opts EditFileOptions) (*EditResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}
	if opts.Operation == "" {
		return nil, usageErrorf("operation is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := refuseEditThroughHardLinks(normalizedPath, opts.Path, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Read file content
	fileContent, err := os.ReadFile(normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(fileContent), "\n")

	switch opts.Operation {
	case "insert":
		if opts.Line < 1 || opts.Line > len(lines)+1 {
			return nil, fmt.Errorf("line number out of range")
		}
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:opts.Line-1]...)
		newLines = append(newLines, opts.Content)
		newLines = append(newLines, lines[opts.Line-1:]...)
		lines = newLines

	case "delete":
		if opts.Line < 1 || opts.Line > len(lines) {
			return nil, fmt.Errorf("line number out of range")
		}
		lines = append(lines[:opts.Line-1], lines[opts.Line:]...)

	case "replace":
		if opts.Line < 1 || opts.Line > len(lines) {
			return nil, fmt.Errorf("line number out of range")
		}
		lines[opts.Line-1] = opts.Content

	default:
		return nil, usageErrorf("unknown operation %q (use insert, replace or delete)", opts.Operation)
	}

	newContent := strings.Join(lines, "\n")

	inPlace, err := replaceFile(normalizedPath, []byte(newContent))
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	result := &EditResult{
		Path:    normalizedPath,
		Success: true,
		Changes: 1,
		Message: fmt.Sprintf("Operation '%s' completed successfully", opts.Operation),
	}
	if inPlace != "" {
		result.InPlace = true
		result.Message += inPlaceNote(inPlace)
	}
	return result, nil
}

// SearchReplaceOptions contains input parameters for SearchAndReplace
type SearchReplaceOptions struct {
	Path        string
	Pattern     string
	Replacement string
	Regex       bool
	DryRun      bool
	FileTypes   []string
	AllowedDirs []string
}

// SearchReplaceResult represents results from search and replace.
// DryRun marks a preview in every format, as on EditResult.
type SearchReplaceResult struct {
	Path          string       `json:"path"`
	FilesModified int          `json:"files_modified"`
	TotalChanges  int          `json:"total_changes"`
	Results       []EditResult `json:"results"`
	DryRun        bool         `json:"dry_run,omitempty"`
}

// SearchAndReplace performs search and replace across files
func SearchAndReplace(opts SearchReplaceOptions) (*SearchReplaceResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}
	if opts.Pattern == "" {
		return nil, usageErrorf("pattern is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	var replaceFn func(s string) string

	if opts.Regex {
		re, err := regexp.Compile(opts.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}
		replaceFn = func(s string) string {
			return re.ReplaceAllString(s, opts.Replacement)
		}
	} else {
		replaceFn = func(s string) string {
			return strings.ReplaceAll(s, opts.Pattern, opts.Replacement)
		}
	}

	var results []EditResult
	totalChanges := 0

	walkFn := func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Repository metadata is not the caller's text: a replace across a
		// checkout rewrote .git/config (EST-8264).
		if info.IsDir() && info.Name() == ".git" && filePath != normalizedPath {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}

		// Check file type filter
		if len(opts.FileTypes) > 0 {
			ext := filepath.Ext(info.Name())
			matched := false
			for _, ft := range opts.FileTypes {
				if ext == ft || "."+ext == ft || ext == "."+ft {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		// Skip binary files
		if isBinaryFile(filePath) {
			return nil
		}

		// A file with another name, which may be outside the allowed
		// directories, is skipped like any file it cannot write (EST-8805).
		if refuseEditThroughHardLinks(filePath, filePath, opts.AllowedDirs) != nil {
			return nil
		}

		// Read file
		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil
		}

		contentStr := string(content)
		newContent := replaceFn(contentStr)

		if contentStr == newContent {
			return nil // No changes
		}

		changes := countChanges(contentStr, newContent, opts.Pattern, opts.Regex)
		totalChanges += changes

		inPlace := ""
		if !opts.DryRun {
			// replaceFile stats through a symlink. Walk's info is the link's
			// own Lstat (mode 0777), and keeping that mode chmodded the
			// target behind the link to rwxrwxrwx.
			if inPlace, err = replaceFile(filePath, []byte(newContent)); err != nil {
				return nil
			}
		}

		// A dry run writes nothing, so its per-file prose must not claim a write
		// happened. "Modified" under --dry-run is exactly the failure this repo
		// exists to prevent: reporting success for something that did not occur.
		message := "Modified"
		if opts.DryRun {
			message = "Would modify"
		}
		if inPlace != "" {
			message += inPlaceNote(inPlace)
		}

		results = append(results, EditResult{
			Path:    filePath,
			Success: true,
			Changes: changes,
			Message: message,
			InPlace: inPlace != "",
		})

		return nil
	}

	if err := filepath.Walk(normalizedPath, walkFn); err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return &SearchReplaceResult{
		Path:          normalizedPath,
		FilesModified: len(results),
		TotalChanges:  totalChanges,
		Results:       results,
		DryRun:        opts.DryRun,
	}, nil
}

func countChanges(old, new, pattern string, useRegex bool) int {
	if useRegex {
		re, _ := regexp.Compile(pattern)
		return len(re.FindAllString(old, -1))
	}
	return strings.Count(old, pattern)
}

// MultiEditOperation represents a single edit operation in EditMultipleBlocks
type MultiEditOperation struct {
	OldText    string `json:"old_text,omitempty"`
	NewText    string `json:"new_text,omitempty"`
	LineNumber int    `json:"line_number,omitempty"`
	Mode       string `json:"mode,omitempty"` // "replace", "insert_before", "insert_after", "delete_line"
}

// MultiEditResult represents the detailed result of EditMultipleBlocks (fast-filesystem parity)
type MultiEditResult struct {
	Message         string         `json:"message"`
	Path            string         `json:"path"`
	TotalEdits      int            `json:"total_edits"`
	SuccessfulEdits int            `json:"successful_edits"`
	Unmatched       int            `json:"unmatched,omitempty"`
	TotalChanges    int            `json:"total_changes"`
	OriginalLines   int            `json:"original_lines"`
	NewLines        int            `json:"new_lines"`
	EditResults     []EditOpResult `json:"edit_results"`
	BackupCreated   *string        `json:"backup_created"`
	BackupEnabled   bool           `json:"backup_enabled"`
	Size            int64          `json:"size"`
	SizeReadable    string         `json:"size_readable"`
	Timestamp       string         `json:"timestamp"`
	Replacements    int            `json:"replacements,omitempty"`
	InPlace         bool           `json:"in_place,omitempty"`
}

// EditOpResult represents the result of a single edit operation
type EditOpResult struct {
	EditIndex    int    `json:"edit_index"`
	Mode         string `json:"mode"`
	Status       string `json:"status"`
	LineNumber   int    `json:"line_number,omitempty"`
	OldText      string `json:"old_text_preview,omitempty"`
	NewText      string `json:"new_text_preview,omitempty"`
	InsertedLine string `json:"inserted_line,omitempty"`
	DeletedLine  string `json:"deleted_line,omitempty"`
	ChangesMade  int    `json:"changes_made"`
}

// EditMultipleBlocksOptions contains input parameters for EditMultipleBlocks
type EditMultipleBlocksOptions struct {
	ReplaceAll  bool
	Path        string
	Edits       []MultiEditOperation
	Backup      bool
	AllowedDirs []string
}

// EditMultipleBlocks applies multiple edits to a file with support for different modes
// Modes: replace (text matching), insert_before (line-based), insert_after (line-based), delete_line (line-based)
func EditMultipleBlocks(opts EditMultipleBlocksOptions) (*MultiEditResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := refuseEditThroughHardLinks(normalizedPath, opts.Path, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Read file content
	content, err := os.ReadFile(normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(opts.Edits) == 0 {
		return nil, usageErrorf("edits is required")
	}

	contentStr := string(content)
	originalLines := len(strings.Split(contentStr, "\n"))
	totalChanges := 0
	successfulEdits := 0
	replacements := 0
	editResults := make([]EditOpResult, 0, len(opts.Edits))

	// Process each edit
	for i, edit := range opts.Edits {
		mode := edit.Mode
		if mode == "" {
			mode = "replace"
		}

		opResult := EditOpResult{
			EditIndex:  i + 1,
			Mode:       mode,
			LineNumber: edit.LineNumber,
		}

		switch mode {
		case "replace":
			if edit.OldText == "" {
				return nil, fmt.Errorf("edit %d: old_text is required for replace mode", i+1)
			}
			next, n, err := replaceMatches(contentStr, edit.OldText, edit.NewText, opts.ReplaceAll, i+1, "old_text")
			if err != nil {
				return nil, err
			}
			if n > 0 {
				contentStr = next
				opResult.Status = "success"
				opResult.ChangesMade = 1
				opResult.OldText = truncatePreview(edit.OldText, 50)
				opResult.NewText = truncatePreview(edit.NewText, 50)
				totalChanges++
				successfulEdits++
				replacements += n
			} else {
				opResult.Status = "not_found"
				opResult.ChangesMade = 0
			}

		case "insert_before":
			if edit.LineNumber < 1 {
				opResult.Status = "invalid_line"
				opResult.ChangesMade = 0
				editResults = append(editResults, opResult)
				continue
			}
			lines := strings.Split(contentStr, "\n")
			if edit.LineNumber > len(lines)+1 {
				opResult.Status = "line_out_of_range"
				opResult.ChangesMade = 0
				editResults = append(editResults, opResult)
				continue
			}
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:edit.LineNumber-1]...)
			newLines = append(newLines, edit.NewText)
			newLines = append(newLines, lines[edit.LineNumber-1:]...)
			contentStr = strings.Join(newLines, "\n")
			opResult.Status = "success"
			opResult.ChangesMade = 1
			opResult.InsertedLine = truncatePreview(edit.NewText, 50)
			totalChanges++
			successfulEdits++

		case "insert_after":
			if edit.LineNumber < 1 {
				opResult.Status = "invalid_line"
				opResult.ChangesMade = 0
				editResults = append(editResults, opResult)
				continue
			}
			lines := strings.Split(contentStr, "\n")
			if edit.LineNumber > len(lines) {
				opResult.Status = "line_out_of_range"
				opResult.ChangesMade = 0
				editResults = append(editResults, opResult)
				continue
			}
			newLines := make([]string, 0, len(lines)+1)
			newLines = append(newLines, lines[:edit.LineNumber]...)
			newLines = append(newLines, edit.NewText)
			newLines = append(newLines, lines[edit.LineNumber:]...)
			contentStr = strings.Join(newLines, "\n")
			opResult.Status = "success"
			opResult.ChangesMade = 1
			opResult.InsertedLine = truncatePreview(edit.NewText, 50)
			totalChanges++
			successfulEdits++

		case "delete_line":
			if edit.LineNumber < 1 {
				opResult.Status = "invalid_line"
				opResult.ChangesMade = 0
				editResults = append(editResults, opResult)
				continue
			}
			lines := strings.Split(contentStr, "\n")
			if edit.LineNumber > len(lines) {
				opResult.Status = "line_out_of_range"
				opResult.ChangesMade = 0
				editResults = append(editResults, opResult)
				continue
			}
			opResult.DeletedLine = truncatePreview(lines[edit.LineNumber-1], 50)
			lines = append(lines[:edit.LineNumber-1], lines[edit.LineNumber:]...)
			contentStr = strings.Join(lines, "\n")
			opResult.Status = "success"
			opResult.ChangesMade = 1
			totalChanges++
			successfulEdits++
		}

		editResults = append(editResults, opResult)
	}

	if successfulEdits == 0 {
		return nil, noMatch(fmt.Sprintf("none of %d edits applied", len(opts.Edits)))
	}

	var backupPath *string
	if opts.Backup {
		bp, err := createEditBackup(normalizedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create backup: %w", err)
		}
		backupPath = &bp
	}

	inPlace, err := replaceFile(normalizedPath, []byte(contentStr))
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Get final file info
	finalInfo, _ := os.Stat(normalizedPath)
	var finalSize int64
	if finalInfo != nil {
		finalSize = finalInfo.Size()
	}

	newLines := len(strings.Split(contentStr, "\n"))

	unmatched := len(opts.Edits) - successfulEdits

	message := fmt.Sprintf("Applied %d of %d edits", successfulEdits, len(opts.Edits))
	if unmatched > 0 {
		message += fmt.Sprintf("; %d did not apply", unmatched)
	}
	if inPlace != "" {
		message += inPlaceNote(inPlace)
	}

	return &MultiEditResult{
		Message:         message,
		Path:            normalizedPath,
		TotalEdits:      len(opts.Edits),
		SuccessfulEdits: successfulEdits,
		Unmatched:       unmatched,
		TotalChanges:    totalChanges,
		OriginalLines:   originalLines,
		NewLines:        newLines,
		EditResults:     editResults,
		BackupCreated:   backupPath,
		BackupEnabled:   opts.Backup,
		Size:            finalSize,
		SizeReadable:    formatBytes(finalSize),
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		Replacements:    replacements,
		InPlace:         inPlace != "",
	}, nil
}

// truncatePreview truncates a string to maxLen characters with ellipsis
func truncatePreview(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// formatBytes formats bytes as human-readable string
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
