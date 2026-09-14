package commands

import (
	"fmt"
	"strings"

	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

// fullSuggestionLimit is the size above which a truncated read stops
// RECOMMENDING --full.
//
// --full means "return everything", and for a caller who asks for it that is a
// fair trade. But pulling a whole file costs a multiple of its size in memory
// once it is read, copied, marshalled and encoded — 300MB in gives roughly
// 1.8GB resident — and the tool SUGGESTING that on a large file is a different
// thing from the caller choosing it. Above this size the resume is offered
// instead.
//
// A var rather than a const so the test can lower it, instead of writing an
// 11MB fixture to exercise the branch.
var fullSuggestionLimit int64 = 10 << 20 // 10 MB

func addReadCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(readFileCmd())
	rootCmd.AddCommand(readMultipleFilesCmd())
	rootCmd.AddCommand(extractLinesCmd())
}

func readFileCmd() *cobra.Command {
	var path string
	var startOffset, lineStart, lineCount int
	var maxSize int64

	cmd := &cobra.Command{
		Use:   "read-file",
		Short: "Read a file",
		Long:  "Reads a file with optional line range or byte offset",
		Run: func(cmd *cobra.Command, args []string) {
			// Size limit: 0 = use default, -1 = no limit, >0 = custom.
			//
			// --full extends from "all fields" to "all fields and all bytes":
			// one flag meaning "do not reduce what you return" beats a second
			// flag an agent has to discover. An explicit --max-size is more
			// specific, so it wins — the same precedence --format has over the
			// legacy --json.
			sizeLimit := maxSize
			if !cmd.Flags().Changed("max-size") {
				sizeLimit = 0
				if activeFull {
					sizeLimit = -1
				}
			}

			result, err := core.ReadFile(core.ReadFileOptions{
				Path:             path,
				StartOffset:      startOffset,
				LineStart:        lineStart,
				LineCount:        lineCount,
				AllowedDirs:      GetAllowedDirs(),
				SizeCheckMaxSize: sizeLimit,
			})
			if err != nil {
				OutputError(err)
				return
			}

			OutputResultAXI(result, nil,
				func() []string {
					if !result.Truncated {
						return nil
					}

					var steps []string
					if result.NextOffset > 0 {
						steps = append(steps, fmt.Sprintf(
							"Continue: llm-filesystem read-file --path %s --start-offset %d",
							result.Path, result.NextOffset))
					}

					// --full is only SUGGESTED for a file small enough that
					// pulling it whole is a reasonable thing to do. Above the
					// threshold it costs a multiple of the file in memory, and
					// a tool recommending that is a different matter from a
					// caller choosing it.
					if result.TotalSize <= fullSuggestionLimit {
						steps = append(steps,
							"Whole file: llm-filesystem read-file --path "+result.Path+" --full")
					} else {
						steps = append(steps, fmt.Sprintf(
							"This file is %d bytes; read it in windows rather than whole.",
							result.TotalSize))
					}
					return steps
				},
				func() string {
					return result.Content
				})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path to read (required)")
	cmd.Flags().IntVar(&startOffset, "start-offset", 0, "Starting byte offset")
	cmd.Flags().Int64Var(&maxSize, "max-size", 0, "Maximum JSON output size in chars (0 = default 70000, -1 = no limit)")
	cmd.Flags().IntVar(&lineStart, "line-start", 0, "Starting line number")
	cmd.Flags().IntVar(&lineCount, "line-count", 0, "Number of lines to read")
	cmd.MarkFlagRequired("path")

	return cmd
}

func readMultipleFilesCmd() *cobra.Command {
	var paths []string
	var maxTotalSize int64

	cmd := &cobra.Command{
		Use:   "read-multiple-files",
		Short: "Read multiple files simultaneously",
		Long:  "Reads multiple files concurrently and returns their contents",
		Run: func(cmd *cobra.Command, args []string) {
			// Same precedence as read-file: an explicit --max-total-size beats
			// --full, and --full means "do not reduce what you return".
			sizeLimit := maxTotalSize
			if !cmd.Flags().Changed("max-total-size") {
				sizeLimit = 0
				if activeFull {
					sizeLimit = -1
				}
			}

			result, err := core.ReadMultipleFiles(core.ReadMultipleFilesOptions{
				Paths:                 paths,
				AllowedDirs:           GetAllowedDirs(),
				SizeCheckMaxTotalSize: sizeLimit,
			})
			if err != nil {
				OutputError(err)
				return
			}

			OutputResultAXI(result, nil, func() []string {
				if !result.Truncated {
					return nil
				}
				// Name the files that did not fit, so the continuation is a
				// concrete command rather than a puzzle.
				var pending []string
				for _, f := range result.Files {
					if f.Truncated {
						pending = append(pending, f.Path)
					}
				}
				steps := []string{"Whole files: add --full"}
				if len(pending) > 0 {
					steps = append([]string{"Re-request what did not fit: llm-filesystem read-multiple-files --paths " +
						strings.Join(pending, ",")}, steps...)
				}
				return steps
			}, func() string {
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("Read %d files (%d success, %d failed)\n",
					len(result.Files), result.Success, result.Failed))
				for _, f := range result.Files {
					if f.Error != "" {
						sb.WriteString(fmt.Sprintf("\n--- %s (ERROR: %s) ---\n", f.Path, f.Error))
					} else {
						sb.WriteString(fmt.Sprintf("\n--- %s (%d bytes, %d lines) ---\n%s",
							f.Path, f.Size, f.Lines, f.Content))
					}
				}
				return sb.String()
			})
		},
	}

	cmd.Flags().StringSliceVar(&paths, "paths", nil, "File paths to read (comma-separated)")
	cmd.Flags().Int64Var(&maxTotalSize, "max-total-size", 0, "Maximum combined JSON output size in chars (0 = default 70000, -1 = no limit)")
	cmd.MarkFlagRequired("paths")

	return cmd
}

func extractLinesCmd() *cobra.Command {
	var path, pattern string
	var lineNumbers []int
	var startLine, endLine, contextLines int

	cmd := &cobra.Command{
		Use:   "extract-lines",
		Short: "Extract specific lines from a file",
		Long:  "Extracts lines by number, range, or pattern from a file",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.ExtractLines(core.ExtractLinesOptions{
				Path:         path,
				LineNumbers:  lineNumbers,
				StartLine:    startLine,
				EndLine:      endLine,
				Pattern:      pattern,
				ContextLines: contextLines,
				AllowedDirs:  GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				return result.Content
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "File path (required)")
	cmd.Flags().IntSliceVar(&lineNumbers, "lines", nil, "Specific line numbers to extract")
	cmd.Flags().IntVar(&startLine, "start", 0, "Start line for range extraction")
	cmd.Flags().IntVar(&endLine, "end", 0, "End line for range extraction")
	cmd.Flags().StringVar(&pattern, "pattern", "", "Pattern to match for extraction")
	cmd.Flags().IntVar(&contextLines, "context", 0, "Context lines around pattern matches")
	cmd.MarkFlagRequired("path")

	return cmd
}
