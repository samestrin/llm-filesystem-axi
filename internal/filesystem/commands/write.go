package commands

import (
	"fmt"

	goaxi "github.com/samestrin/go-axi"
	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

func addWriteCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(writeFileCmd())
	rootCmd.AddCommand(largeWriteFileCmd())
	rootCmd.AddCommand(getFileInfoCmd())
	rootCmd.AddCommand(writeMultipleFilesCmd())
	rootCmd.AddCommand(createDirectoryCmd())
}

func writeFileCmd() *cobra.Command {
	var path, content string
	var createDirs, appendMode bool

	cmd := &cobra.Command{
		Use:   "write-file",
		Short: "Write content to a file",
		Long:  "Writes or modifies a file with the specified content",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.WriteFile(core.WriteFileOptions{
				Path:        path,
				Content:     content,
				CreateDirs:  createDirs,
				Append:      appendMode,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				action := "Updated"
				if result.Created {
					action = "Created"
				}
				return fmt.Sprintf("%s %s (%d bytes)", action, result.Path, result.Size)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().StringVar(&content, "content", "", "Content to write (required)")
	cmd.Flags().BoolVar(&createDirs, "create-dirs", true, "Create parent directories if needed")
	cmd.Flags().BoolVar(&appendMode, "append", false, "Append to file instead of overwrite")
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("content")

	return cmd
}

func largeWriteFileCmd() *cobra.Command {
	var path, content string
	var createDirs, appendMode, backup, verifyWrite bool

	cmd := &cobra.Command{
		Use:   "large-write-file",
		Short: "Write large files with backup and verification",
		Long:  "Reliably writes large files with streaming, backup, and verification",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.LargeWriteFile(core.LargeWriteFileOptions{
				Path:        path,
				Content:     content,
				CreateDirs:  createDirs,
				Append:      appendMode,
				Backup:      backup,
				VerifyWrite: verifyWrite,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				action := "Updated"
				if result.Created {
					action = "Created"
				}
				msg := fmt.Sprintf("%s %s (%d bytes)", action, result.Path, result.Size)
				if result.Backup != "" {
					msg += fmt.Sprintf("\nBackup: %s", result.Backup)
				}
				return msg
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().StringVar(&content, "content", "", "Content to write (required)")
	cmd.Flags().BoolVar(&createDirs, "create-dirs", true, "Create parent directories if needed")
	cmd.Flags().BoolVar(&appendMode, "append", false, "Append to file instead of overwrite")
	cmd.Flags().BoolVar(&backup, "backup", true, "Create backup before writing")
	cmd.Flags().BoolVar(&verifyWrite, "verify", true, "Verify write after completion")
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("content")

	return cmd
}

func writeMultipleFilesCmd() *cobra.Command {
	var filesJSON string

	cmd := &cobra.Command{
		Use:   "write-multiple-files",
		Short: "Write multiple files in a single operation",
		Long:  "Writes multiple files sequentially with auto-mkdir for parent directories",
		Run: func(cmd *cobra.Command, args []string) {
			entries, err := decodeWriteEntries(filesJSON)
			if err != nil {
				refusePayload("invalid --files JSON", err, filesExample)
				return
			}

			result, err := core.WriteMultipleFiles(core.WriteMultipleFilesOptions{
				Files:       entries,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				return fmt.Sprintf("Wrote %d files, %d failed", result.Success, result.Failed)
			})

			// The result first, so the caller learns which entries failed,
			// then exit 1, as compress-files does: a run with failed entries
			// did not succeed, and the MCP server reads isError from this code.
			if result.Failed > 0 {
				exitFunc(int(goaxi.ExitError))
				return
			}
		},
	}

	cmd.Flags().StringVar(&filesJSON, "files", "", "JSON array of objects (required): "+filesExample)
	cmd.MarkFlagRequired("files")

	return cmd
}

const filesExample = `[{"path":"a.txt","content":"text"}]`

// decodeWriteEntries decodes --files and requires both keys in every entry.
//
// A missing content is refused rather than read as "": an empty file is a
// legitimate request, but only when it is asked for by name. Decoding through
// pointers is what tells the two apart.
func decodeWriteEntries(raw string) ([]core.WriteFileEntry, error) {
	var payload []struct {
		Path    *string `json:"path"`
		Content *string `json:"content"`
	}
	if err := decodePayload(raw, &payload); err != nil {
		return nil, err
	}
	entries := make([]core.WriteFileEntry, len(payload))
	for i, p := range payload {
		if p.Path == nil || *p.Path == "" {
			return nil, fmt.Errorf("entry %d: path is required", i)
		}
		if p.Content == nil {
			return nil, fmt.Errorf("entry %d: content is required (use \"\" for an empty file)", i)
		}
		entries[i] = core.WriteFileEntry{Path: *p.Path, Content: *p.Content}
	}
	return entries, nil
}

func getFileInfoCmd() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "get-file-info",
		Short: "Get detailed file information",
		Long:  "Gets detailed information about a file or directory",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.GetFileInfo(core.GetFileInfoOptions{
				Path:        path,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				fileType := "File"
				if result.IsDir {
					fileType = "Directory"
				}
				return fmt.Sprintf("%s: %s\nSize: %d\nMode: %s\nModified: %s",
					fileType, result.Path, result.Size, result.Mode, result.Modified)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Path to get info for (required)")
	cmd.MarkFlagRequired("path")

	return cmd
}

func createDirectoryCmd() *cobra.Command {
	var path string
	var recursive bool

	cmd := &cobra.Command{
		Use:   "create-directory",
		Short: "Create a directory",
		Long:  "Creates a directory, optionally with parent directories",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.CreateDirectory(core.CreateDirectoryOptions{
				Path:        path,
				Recursive:   recursive,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				if result.Exists {
					return fmt.Sprintf("Directory already exists: %s", result.Path)
				}
				return fmt.Sprintf("Created directory: %s", result.Path)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Directory path to create (required)")
	cmd.Flags().BoolVar(&recursive, "recursive", true, "Create parent directories")
	cmd.MarkFlagRequired("path")

	return cmd
}
