package commands

import (
	"fmt"
	"strings"

	goaxi "github.com/samestrin/go-axi"
	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

func addAdvancedCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(getDiskUsageCmd())
	rootCmd.AddCommand(findLargeFilesCmd())
	rootCmd.AddCommand(compressFilesCmd())
	rootCmd.AddCommand(extractArchiveCmd())
	rootCmd.AddCommand(syncDirectoriesCmd())
	rootCmd.AddCommand(listAllowedDirectoriesCmd())
}

func getDiskUsageCmd() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "get-disk-usage",
		Short: "Get disk usage for a path",
		Long:  "Calculates total disk usage for a directory",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.GetDiskUsage(core.DiskUsageOptions{
				Path:        path,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				return fmt.Sprintf("Path: %s\nTotal Size: %d bytes\nFiles: %d\nDirectories: %d",
					result.Path, result.TotalSize, result.TotalFiles, result.TotalDirs)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Path to analyze (required)")
	cmd.MarkFlagRequired("path")

	return cmd
}

func findLargeFilesCmd() *cobra.Command {
	var path string
	var minSize int64
	var limit int

	cmd := &cobra.Command{
		Use:   "find-large-files",
		Short: "Find large files",
		Long:  "Finds files larger than a specified size",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.FindLargeFiles(core.FindLargeFilesOptions{
				Path:        path,
				MinSize:     minSize,
				Limit:       limit,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResultAXI(result, nil,
				func() []string {
					// Nothing at or above --min-size, so offer the ways to widen
					// rather than leaving an empty list with no next step.
					// At --min-size 0 there are no files at all, and lowering the
					// threshold would only repeat the same empty answer.
					if result.Total > 0 {
						return nil
					}
					var steps []string
					if result.MinSize > 0 {
						steps = append(steps, fmt.Sprintf("Lower the threshold: --min-size is %d bytes", result.MinSize))
					}
					return append(steps, "See what is there: llm-filesystem get-directory-tree --path "+result.Path)
				},
				func() string {
					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("Found %d files >= %d bytes in %s\n\n",
						result.Total, result.MinSize, result.Path))
					for _, f := range result.Files {
						sb.WriteString(fmt.Sprintf("%s  %d bytes  %s\n", f.Path, f.Size, f.ModTime))
					}
					return sb.String()
				})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Directory to search (required)")
	cmd.Flags().Int64Var(&minSize, "min-size", 0, "Minimum file size in bytes")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum results to return")
	cmd.MarkFlagRequired("path")

	return cmd
}

func compressFilesCmd() *cobra.Command {
	var paths []string
	var output, archiveFormat string

	cmd := &cobra.Command{
		Use:   "compress-files",
		Short: "Compress files into an archive",
		Long:  "Creates a zip or tar.gz archive from files and directories",
		Run: func(cmd *cobra.Command, args []string) {
			kind, err := resolveArchiveType(output, archiveFormat, formatFlag)
			if err != nil {
				emitDiagnostic(activeFmt, activeCompact, err, goaxi.ExitUsage,
					[]string{"Name the archive type: --output a.zip, a.tar.gz or a.tgz, or pass --archive-format zip|tar.gz|tgz"})
				return
			}
			result, err := core.CompressFiles(core.CompressFilesOptions{
				Paths:       paths,
				Output:      output,
				Format:      kind,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResultAXI(result, nil,
				func() []string {
					if result.Failed == 0 {
						return nil
					}
					step := fmt.Sprintf("Not added (%d): %s",
						result.Failed, strings.Join(result.Failures, "; "))
					if more := result.Failed - len(result.Failures); more > 0 {
						step += fmt.Sprintf("; and %d more", more)
					}
					return []string{step}
				},
				func() string {
					overwrote := ""
					if result.Overwritten {
						overwrote = ", replaced the existing file"
					}
					return fmt.Sprintf("Created %s (%s format%s)\n%d files added, %d failed, %d bytes",
						result.Output, result.Format, overwrote,
						result.FilesAdded, result.Failed, result.Size)
				})

			// As in extract-archive: the result first, so the caller learns which
			// inputs were left out, then exit 1 for the partial run (epic 04 Q2).
			if result.Failed > 0 {
				exitFunc(int(goaxi.ExitError))
				return
			}
		},
	}

	cmd.Flags().StringSliceVar(&paths, "paths", nil, "Paths to compress (required)")
	cmd.Flags().StringVar(&output, "output", "", "Output archive path (required)")
	cmd.Flags().StringVar(&archiveFormat, "archive-format", "", "Archive type: zip, tar.gz, tgz (default: from --output's extension)")
	cmd.MarkFlagRequired("paths")
	cmd.MarkFlagRequired("output")

	return cmd
}

// archiveTypes are the values --archive-format accepts.
var archiveTypes = []string{"zip", "tar.gz", "tgz"}

func isArchiveType(s string) bool {
	for _, t := range archiveTypes {
		if s == t {
			return true
		}
	}
	return false
}

// archiveTypeFromExt maps an --output name to its archive type, or "".
func archiveTypeFromExt(output string) string {
	l := strings.ToLower(output)
	switch {
	case strings.HasSuffix(l, ".zip"):
		return "zip"
	case strings.HasSuffix(l, ".tar.gz"), strings.HasSuffix(l, ".tgz"):
		// .tgz and .tar.gz are one format; normalise so they compare equal.
		return "tar.gz"
	}
	return ""
}

func normalizeArchiveType(t string) string {
	if t == "tgz" {
		return "tar.gz"
	}
	return t
}

// resolveArchiveType picks the archive type for compress-files. The explicit
// type is --archive-format, or the deprecated spelling --format zip|tar.gz|tgz
// (any other --format value is the global output format and is not ours). An
// explicit type that contradicts the --output extension, or no type at all, is
// a usage error: nothing has been written when it is returned.
func resolveArchiveType(output, archiveFormat, globalFormat string) (string, error) {
	explicit := archiveFormat
	if explicit == "" && isArchiveType(globalFormat) {
		explicit = globalFormat
	}
	if explicit != "" && !isArchiveType(explicit) {
		return "", fmt.Errorf("unsupported --archive-format %q (want zip, tar.gz or tgz)", explicit)
	}
	explicit = normalizeArchiveType(explicit)
	fromExt := archiveTypeFromExt(output)

	switch {
	case explicit != "" && fromExt != "" && explicit != fromExt:
		return "", fmt.Errorf("--archive-format %s contradicts the extension of --output %s", explicit, output)
	case explicit != "":
		return explicit, nil
	case fromExt != "":
		return fromExt, nil
	}
	return "", fmt.Errorf("cannot tell the archive type from --output %q: use a .zip, .tar.gz or .tgz extension, or pass --archive-format", output)
}

func extractArchiveCmd() *cobra.Command {
	var archive, destination string
	var maxBytes int64
	var maxEntries int

	cmd := &cobra.Command{
		Use:   "extract-archive",
		Short: "Extract an archive",
		Long: fmt.Sprintf("Extracts a zip or tar.gz archive to a destination. Against decompression bombs, "+
			"extraction stops at %d bytes written in total (--max-bytes) or %d entries (--max-entries); "+
			"the entry that crosses a cap fails, its partial file is removed, the result carries truncated: true, "+
			"and the run exits 1. Pass -1 to lift a cap.",
			core.DefaultExtractMaxBytes, core.DefaultExtractMaxEntries),
		Run: func(cmd *cobra.Command, args []string) {
			// 0 is neither a cap nor "no limit", and core reads it as "use the
			// default", so an explicit 0 would silently mean 1 GiB. Below -1 is
			// undocumented and silently meant "no limit". Both are refused
			// before anything is written.
			for _, f := range []struct {
				name string
				bad  bool
			}{{"max-bytes", maxBytes == 0 || maxBytes < -1}, {"max-entries", maxEntries == 0 || maxEntries < -1}} {
				if f.bad && cmd.Flags().Changed(f.name) {
					emitDiagnostic(activeFmt, activeCompact,
						fmt.Errorf("--%s must be a positive cap, or -1 for no limit", f.name),
						goaxi.ExitUsage,
						[]string{"Check the flags and what they mean: llm-filesystem extract-archive --help"})
					return
				}
			}

			result, err := core.ExtractArchive(core.ExtractArchiveOptions{
				Archive:     archive,
				Destination: destination,
				MaxBytes:    maxBytes,
				MaxEntries:  maxEntries,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResultAXI(result, nil,
				func() []string {
					if result.Failed == 0 {
						return nil
					}
					step := fmt.Sprintf("Not extracted (%d): %s",
						result.Failed, strings.Join(result.Failures, "; "))
					if more := result.Failed - len(result.Failures); more > 0 {
						step += fmt.Sprintf("; and %d more", more)
					}
					steps := []string{step}
					if result.Truncated {
						steps = append(steps, "Stopped at a cap; entries after it were not read. "+
							"If the archive is trusted, raise the cap named above: --max-bytes N or --max-entries N (-1 for no limit)")
					}
					return steps
				},
				func() string {
					return fmt.Sprintf("Extracted %s to %s\n%d files extracted, %d overwritten, %d failed",
						result.Archive, result.Destination,
						result.FilesExtracted, result.Overwritten, result.Failed)
				})

			// The result is printed first so the caller learns which entries
			// were dropped and why; the exit code then says the run was partial
			// (owner decision, epic 04 Q2), as compress-files and sync-directories do.
			if result.Failed > 0 {
				exitFunc(int(goaxi.ExitError))
				return
			}
		},
	}

	cmd.Flags().StringVar(&archive, "archive", "", "Archive file path (required)")
	cmd.Flags().StringVar(&destination, "dest", "", "Destination directory (required)")
	cmd.Flags().Int64Var(&maxBytes, "max-bytes", core.DefaultExtractMaxBytes, "Stop once this many bytes are written in total (-1: no limit)")
	cmd.Flags().IntVar(&maxEntries, "max-entries", core.DefaultExtractMaxEntries, "Stop once the archive holds more than this many entries (-1: no limit)")
	cmd.MarkFlagRequired("archive")
	cmd.MarkFlagRequired("dest")

	return cmd
}

func syncDirectoriesCmd() *cobra.Command {
	var source, destination string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "sync-directories",
		Short: "Synchronize two directories",
		Long:  "Synchronizes the contents of a source directory to a destination",
		Run: func(cmd *cobra.Command, args []string) {
			result, err := core.SyncDirectories(core.SyncDirectoriesOptions{
				Source:      source,
				Destination: destination,
				AllowedDirs: GetAllowedDirs(),
				DryRun:      dryRun,
			})
			if err != nil {
				OutputError(err)
				return
			}

			OutputResultAXI(result, nil,
				func() []string {
					var steps []string
					if result.Failed > 0 {
						step := fmt.Sprintf("Not copied (%d): %s",
							result.Failed, strings.Join(result.Failures, "; "))
						if more := result.Failed - len(result.Failures); more > 0 {
							step += fmt.Sprintf("; and %d more", more)
						}
						steps = append(steps, step)
					}
					if result.DryRun {
						steps = append(steps, fmt.Sprintf(
							"Apply it: llm-filesystem sync-directories --source %s --dest %s",
							result.Source, result.Destination))
					}
					return steps
				},
				func() string {
					prefix := ""
					if result.DryRun {
						prefix = "[DRY RUN] "
					}
					return fmt.Sprintf("%sSynced %s to %s\n%d files copied, %d directories created, %d failed",
						prefix, result.Source, result.Destination,
						result.FilesCopied, result.DirsCreated, result.Failed)
				})

			// As in extract-archive and compress-files: the result first, so the
			// caller learns which paths were left out, then exit 1 for the
			// partial run (epic 04 Q2). This command used to exit 0 here, which
			// made the three disagree.
			if result.Failed > 0 {
				exitFunc(int(goaxi.ExitError))
				return
			}
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "Source directory (required)")
	cmd.Flags().StringVar(&destination, "dest", "", "Destination directory (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying")
	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("dest")

	return cmd
}

func listAllowedDirectoriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-allowed-directories",
		Short: "List allowed directories",
		Long:  "Returns the list of directories the tool is allowed to access",
		Run: func(cmd *cobra.Command, args []string) {
			result := core.ListAllowedDirectories(GetAllowedDirs())
			OutputResult(result, func() string {
				if len(result.AllowedDirectories) == 0 {
					return "All directories allowed (no restrictions)"
				}
				return fmt.Sprintf("Allowed directories:\n%s",
					strings.Join(result.AllowedDirectories, "\n"))
			})
		},
	}

	return cmd
}
