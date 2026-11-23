package commands

import (
	"errors"
	"fmt"

	goaxi "github.com/samestrin/go-axi"
	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

func addEditCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(editBlockCmd())
	rootCmd.AddCommand(editBlocksCmd())
	rootCmd.AddCommand(editMultipleBlocksCmd())
	rootCmd.AddCommand(safeEditCmd())
	rootCmd.AddCommand(editFileCmd())
	rootCmd.AddCommand(searchAndReplaceCmd())
}

// outputEditError reports an edit failure. An ambiguous match carries its own
// recovery: the generic --help pointer would leave the caller guessing that the
// two ways out are --replace-all and a longer old string. An unmatched edit
// does too: the old text is not in the file as given, and the fix is to copy it
// from the file's current text. Still exit 1, as any tool failure: the edit
// was attempted against the file and could not apply.
func outputEditError(err error, oldFlag, path string) {
	if errors.Is(err, core.ErrNoMatch) {
		emitDiagnostic(activeFmt, activeCompact, err, goaxi.ExitError, []string{
			"See the file's current text, then copy " + oldFlag + " from it exactly: llm-filesystem read-file --path " + path,
		})
		return
	}
	var amb *core.AmbiguousMatchError
	if !errors.As(err, &amb) {
		OutputError(err)
		return
	}
	emitDiagnostic(activeFmt, activeCompact, err, goaxi.ExitError, []string{
		"Replace every occurrence: re-run the same command with --replace-all",
		"Or make " + oldFlag + " longer, with surrounding lines, so it matches exactly once",
	})
}

const (
	editPairsExample  = `[{"old_string":"x","new_string":"y"}]`
	multiEditsExample = `[{"old_text":"x","new_text":"y","mode":"replace"},{"mode":"insert_after","line_number":3,"new_text":"z"}]`
)

// decodeEditPairs decodes edit-blocks' --edits and requires old_string in
// every edit. An edit with nothing to find is a malformed invocation (exit 2),
// not an edit that was attempted and failed.
func decodeEditPairs(raw string) ([]core.EditPair, error) {
	var edits []core.EditPair
	if err := decodePayload(raw, &edits); err != nil {
		return nil, err
	}
	for i, e := range edits {
		if e.OldString == "" {
			return nil, fmt.Errorf("edit %d: old_string is required", i+1)
		}
	}
	return edits, nil
}

// decodeMultiEdits decodes edit-multiple-blocks' --edits and requires what each
// mode needs. A line mode without line_number used to come back as status
// invalid_line with exit 0 and the file unchanged.
func decodeMultiEdits(raw string) ([]core.MultiEditOperation, error) {
	var edits []core.MultiEditOperation
	if err := decodePayload(raw, &edits); err != nil {
		return nil, err
	}
	for i, e := range edits {
		switch e.Mode {
		case "", "replace":
			if e.OldText == "" {
				return nil, fmt.Errorf("edit %d: old_text is required for replace mode", i+1)
			}
		case "insert_before", "insert_after", "delete_line":
			if e.LineNumber < 1 {
				return nil, fmt.Errorf("edit %d: line_number (1-based) is required for %s mode", i+1, e.Mode)
			}
		}
	}
	return edits, nil
}

const replaceAllUsage = "Replace every occurrence of the old text (default: refuse unless it matches exactly once)"

func editBlockCmd() *cobra.Command {
	var path, oldString, newString string
	var replaceAll bool

	cmd := &cobra.Command{
		Use:   "edit-block",
		Short: "Replace a block of text in a file",
		Long:  "Replaces --old with --new. --old must match exactly once; an ambiguous match is refused unless --replace-all is set.",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.EditBlock(core.EditBlockOptions{
				Path:        path,
				OldString:   oldString,
				NewString:   newString,
				ReplaceAll:  replaceAll,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				outputEditError(err, "--old", path)
				return
			}
			OutputResult(result, func() string {
				return fmt.Sprintf("Edited %s: %d change(s)", result.Path, result.Changes)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().StringVar(&oldString, "old", "", "Text to find and replace (required)")
	cmd.Flags().StringVar(&newString, "new", "", "Replacement text")
	cmd.Flags().BoolVar(&replaceAll, "replace-all", false, replaceAllUsage)
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("old")

	return cmd
}

func editBlocksCmd() *cobra.Command {
	var path, editsJSON string
	var replaceAll bool

	cmd := &cobra.Command{
		Use:   "edit-blocks",
		Short: "Apply multiple edits to a file",
		Long:  "Applies multiple block edits to a single file. Each old_string must match exactly once in the content the earlier edits leave; an ambiguous edit refuses the whole batch unless --replace-all is set.",
		Run: func(cmd *cobra.Command, args []string) {
			edits, err := decodeEditPairs(editsJSON)
			if err != nil {
				refusePayload("invalid edits JSON", err, editPairsExample)
				return
			}

			result, err := core.EditBlocks(core.EditBlocksOptions{
				Path:        path,
				Edits:       edits,
				ReplaceAll:  replaceAll,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				outputEditError(err, "old_string", path)
				return
			}
			OutputResult(result, func() string {
				return fmt.Sprintf("Edited %s: %d change(s)", result.Path, result.Changes)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().StringVar(&editsJSON, "edits", "", "JSON array of edits: "+editPairsExample)
	cmd.Flags().BoolVar(&replaceAll, "replace-all", false, replaceAllUsage)
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("edits")

	return cmd
}

func editMultipleBlocksCmd() *cobra.Command {
	var path, editsJSON string
	var backup, replaceAll bool

	cmd := &cobra.Command{
		Use:   "edit-multiple-blocks",
		Short: "Edit multiple parts of a file with different modes",
		Long:  "Applies multiple edits with modes: replace, insert_before, insert_after, delete_line. A replace edit's old_text must match exactly once; an ambiguous edit refuses the whole batch unless --replace-all is set.",
		Run: func(cmd *cobra.Command, args []string) {
			edits, err := decodeMultiEdits(editsJSON)
			if err != nil {
				refusePayload("invalid edits JSON", err, multiEditsExample)
				return
			}

			result, err := core.EditMultipleBlocks(core.EditMultipleBlocksOptions{
				Path:        path,
				Edits:       edits,
				Backup:      backup,
				ReplaceAll:  replaceAll,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				outputEditError(err, "old_text", path)
				return
			}
			OutputResult(result, func() string {
				msg := fmt.Sprintf("Edited %s: %d change(s)", result.Path, result.TotalChanges)
				if result.BackupCreated != nil {
					msg += fmt.Sprintf("\nBackup: %s", *result.BackupCreated)
				}
				return msg
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().StringVar(&editsJSON, "edits", "", "JSON array of edits: "+multiEditsExample)
	cmd.Flags().BoolVar(&backup, "backup", true, "Create backup before editing")
	cmd.Flags().BoolVar(&replaceAll, "replace-all", false, replaceAllUsage)
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("edits")

	return cmd
}

func safeEditCmd() *cobra.Command {
	var path, oldString, newString string
	var backup, dryRun, replaceAll bool

	cmd := &cobra.Command{
		Use:   "safe-edit",
		Short: "Safe edit with backup and dry-run support",
		Long:  "Performs a safe edit with optional backup creation and dry-run mode. --old must match exactly once; an ambiguous match is refused unless --replace-all is set.",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.SafeEdit(core.SafeEditOptions{
				Path:        path,
				OldString:   oldString,
				NewString:   newString,
				Backup:      backup,
				DryRun:      dryRun,
				ReplaceAll:  replaceAll,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				outputEditError(err, "--old", path)
				return
			}
			OutputResult(result, func() string {
				msg := fmt.Sprintf("Edited %s: %d change(s)", result.Path, result.Changes)
				if result.Backup != "" {
					msg += fmt.Sprintf("\nBackup: %s", result.Backup)
				}
				if dryRun {
					msg = "[DRY RUN] " + msg
				}
				return msg
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().StringVar(&oldString, "old", "", "Text to find and replace (required)")
	cmd.Flags().StringVar(&newString, "new", "", "Replacement text")
	cmd.Flags().BoolVar(&backup, "backup", true, "Create backup before editing")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying")
	cmd.Flags().BoolVar(&replaceAll, "replace-all", false, replaceAllUsage)
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("old")

	return cmd
}

func editFileCmd() *cobra.Command {
	var path, operation, content string
	var line int

	cmd := &cobra.Command{
		Use:   "edit-file",
		Short: "Line-based file editing",
		Long:  "Performs line-based operations: insert, replace, or delete",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.EditFile(core.EditFileOptions{
				Path:        path,
				Operation:   operation,
				Line:        line,
				Content:     content,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				return fmt.Sprintf("%s at line %d: %s", operation, line, result.Message)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().StringVar(&operation, "operation", "", "Operation: insert, replace, delete (required)")
	cmd.Flags().IntVar(&line, "line", 0, "Line number to operate on (required)")
	cmd.Flags().StringVar(&content, "content", "", "Content for insert/replace operations")
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("operation")
	cmd.MarkFlagRequired("line")

	return cmd
}

func searchAndReplaceCmd() *cobra.Command {
	var path, pattern, replacement string
	var regex, dryRun bool
	var fileTypes []string

	cmd := &cobra.Command{
		Use:   "search-and-replace",
		Short: "Search and replace across files",
		Long:  "Performs search and replace across multiple files with regex support",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.SearchAndReplace(core.SearchReplaceOptions{
				Path:        path,
				Pattern:     pattern,
				Replacement: replacement,
				Regex:       regex,
				DryRun:      dryRun,
				FileTypes:   fileTypes,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				msg := fmt.Sprintf("Modified %d files, %d total changes",
					result.FilesModified, result.TotalChanges)
				if dryRun {
					msg = "[DRY RUN] " + msg
				}
				return msg
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Directory to search in (required)")
	cmd.Flags().StringVar(&pattern, "pattern", "", "Search pattern (required)")
	cmd.Flags().StringVar(&replacement, "replacement", "", "Replacement text (required)")
	cmd.Flags().BoolVar(&regex, "regex", false, "Use regex pattern matching")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying")
	cmd.Flags().StringSliceVar(&fileTypes, "file-types", nil, "File extensions to include")
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("pattern")
	cmd.MarkFlagRequired("replacement")

	return cmd
}
