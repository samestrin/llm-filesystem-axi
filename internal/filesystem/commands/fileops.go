package commands

import (
	"fmt"
	"os"

	goaxi "github.com/samestrin/go-axi"
	"github.com/samestrin/llm-filesystem-axi/internal/filesystem/core"
	"github.com/spf13/cobra"
)

func addFileOpsCommands(rootCmd *cobra.Command) {
	rootCmd.AddCommand(copyFileCmd())
	rootCmd.AddCommand(moveFileCmd())
	rootCmd.AddCommand(deleteFileCmd())
	rootCmd.AddCommand(batchFileOperationsCmd())
}

// refuseUnconfirmedOverwrite gates move-file and copy-file the same way the
// batch gate does: os.Rename and os.Create both replace an existing
// destination silently, so the operation is destructive exactly when the
// destination already exists. A fresh destination overwrites nothing and
// needs no confirming.
//
// The refusal is a usage error, not a tool failure: nothing was attempted,
// so nothing failed. Exit 1 would tell the agent the tool tried and broke,
// and the rational response to that is retrying the identical command.
// Exit 2 says "fix the invocation" — here, add --confirm.
//
// Only inspecting the flag's VALUE closes the --confirm=false hole: a
// provided flag satisfies cobra's required check even when it says false.
func refuseUnconfirmedOverwrite(command, source, destination string, confirm bool) bool {
	if confirm {
		return false
	}
	dst, exists := destinationExists(destination)
	if !exists {
		return false
	}
	emitDiagnostic(activeFmt, activeCompact,
		fmt.Errorf("%s would overwrite an existing destination, which requires --confirm", command),
		goaxi.ExitUsage,
		[]string{"Confirm the overwrite: llm-filesystem " + command + " --source " + source + " --dest " + dst + " --confirm"})
	return true
}

// destinationExists reports whether a move or copy to destination would replace
// something, and the normalized path it checked.
//
// It stats the path core will actually write to: core.MoveFile/CopyFile run
// the destination through core.NormalizePath (~ expansion, filepath.Clean), so
// a raw stat misses '~/victim.txt' and 'victim.txt/' and lets the overwrite
// through silently. If normalization fails, it reports false so core reports
// that error rather than a refusal being invented here.
//
// Lstat, not Stat: Stat follows a symlink, so a dangling one read as "nothing
// there" and was replaced by a regular file with no --confirm. delete-file
// uses Lstat for the same reason.
func destinationExists(destination string) (string, bool) {
	dst, err := core.NormalizePath(destination)
	if err != nil {
		return "", false
	}
	if _, statErr := os.Lstat(dst); statErr != nil {
		return dst, false
	}
	return dst, true
}

func copyFileCmd() *cobra.Command {
	var source, destination string
	var confirm bool

	cmd := &cobra.Command{
		Use:   "copy-file",
		Short: "Copy a file or directory",
		Long:  "Copies a file or directory to a new location. Replacing an existing destination requires --confirm.",
		Run: func(cmd *cobra.Command, args []string) {
			if refuseUnconfirmedOverwrite("copy-file", source, destination, confirm) {
				return
			}
			result, err := core.CopyFile(core.CopyFileOptions{
				Source:      source,
				Destination: destination,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				return fmt.Sprintf("Copied %s to %s", result.Source, result.Destination)
			})
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "Source path (required)")
	cmd.Flags().StringVar(&destination, "dest", "", "Destination path (required)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm replacing an existing destination")
	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("dest")

	return cmd
}

func moveFileCmd() *cobra.Command {
	var source, destination string
	var confirm bool

	cmd := &cobra.Command{
		Use:   "move-file",
		Short: "Move or rename a file or directory",
		Long:  "Moves or renames a file or directory. Replacing an existing destination requires --confirm.",
		Run: func(cmd *cobra.Command, args []string) {
			if refuseUnconfirmedOverwrite("move-file", source, destination, confirm) {
				return
			}
			result, err := core.MoveFile(core.MoveFileOptions{
				Source:      source,
				Destination: destination,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				return fmt.Sprintf("Moved %s to %s", result.Source, result.Destination)
			})
		},
	}

	cmd.Flags().StringVar(&source, "source", "", "Source path (required)")
	cmd.Flags().StringVar(&destination, "dest", "", "Destination path (required)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm replacing an existing destination")
	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("dest")

	return cmd
}

func deleteFileCmd() *cobra.Command {
	var path string
	var recursive, confirm bool

	cmd := &cobra.Command{
		Use:   "delete-file",
		Short: "Delete a file or directory",
		Long:  "Deletes a file or directory, optionally recursively. Requires --confirm.\n\nA path that is already gone is a no-op: it exits 0 with deleted: false and\nexisted: false. A missing parent directory, or a path outside --allowed-dirs,\nis still an error.",
		Run: func(cmd *cobra.Command, args []string) {
			// MarkFlagRequired below is satisfied by --confirm=false, because
			// the flag WAS provided. Only inspecting the value closes that hole.
			//
			// This is a usage error, not a tool failure: nothing was attempted,
			// so nothing failed. Exit 1 would tell the agent the tool tried and
			// broke, and the rational response to that is retrying the identical
			// command. Exit 2 says "fix the invocation".
			if !confirm {
				emitDiagnostic(activeFmt, activeCompact,
					fmt.Errorf("delete-file requires --confirm"),
					goaxi.ExitUsage,
					[]string{"Confirm the deletion: llm-filesystem delete-file --path " + path + " --confirm"})
				return
			}

			result, err := core.DeleteFile(core.DeleteFileOptions{
				Path:        path,
				Recursive:   recursive,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResult(result, func() string {
				if result.Deleted != nil && !*result.Deleted {
					return fmt.Sprintf("Nothing to delete: %s does not exist", result.Path)
				}
				return fmt.Sprintf("Deleted %s", result.Path)
			})
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Path to delete (required)")
	cmd.Flags().BoolVar(&recursive, "recursive", false, "Delete directories recursively")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm the deletion (required)")
	cmd.MarkFlagRequired("path")
	cmd.MarkFlagRequired("confirm")

	return cmd
}

const operationsExample = `[{"operation":"copy","source":"a","destination":"b"}]`

// decodeBatchOperations decodes --operations and checks every entry before any
// runs. Only unknown keys used to be refused up front: an entry with a key
// missing ran, so a valid entry followed by a copy with no destination made
// the first change and then failed, and an empty destination normalized to
// the working directory and drew the wrong "overwrites" refusal.
func decodeBatchOperations(raw string) ([]core.BatchOperation, error) {
	ops, err := decodeEntries[core.BatchOperation](raw, "operation")
	if err != nil {
		return nil, err
	}
	for i, op := range ops {
		switch op.Operation {
		case "":
			return nil, fmt.Errorf("operation %d: operation is required", i+1)
		case "copy", "move", "delete":
		default:
			return nil, fmt.Errorf("operation %d: unknown operation %q (use copy, move or delete)", i+1, op.Operation)
		}
		if op.Source == "" {
			return nil, fmt.Errorf("operation %d: source is required", i+1)
		}
		if op.Operation != "delete" && op.Destination == "" {
			return nil, fmt.Errorf("operation %d: destination is required for %s", i+1, op.Operation)
		}
	}
	return ops, nil
}

func batchFileOperationsCmd() *cobra.Command {
	var operationsJSON string
	var confirm bool

	cmd := &cobra.Command{
		Use:   "batch-file-operations",
		Short: "Perform batch file operations",
		Long:  "Performs multiple file operations in a batch. Any entry that destroys something requires --confirm: a delete, or a move or copy onto an existing path.",
		Run: func(cmd *cobra.Command, args []string) {
			operations, err := decodeBatchOperations(operationsJSON)
			if err != nil {
				refusePayload("invalid operations JSON", err, operationsExample)
				return
			}

			// Gating delete-file while leaving this open would be theatre. The
			// batch routes "delete" to the same core.DeleteFile, so an agent
			// that meets the gate steps around it in one hop.
			//
			// Scoped to what is destructive: a batch that only copies is not
			// made harder to use by it.
			if !confirm {
				for i, op := range operations {
					var why string
					switch op.Operation {
					case "delete":
						why = "is a delete"
					case "move", "copy":
						// os.Rename and os.Create both replace an existing
						// destination silently. Gating delete while leaving
						// these open just moved the hole one operation across.
						// Normalized as the single commands do, or '~/x'
						// and 'x/' walk past the gate.
						if _, exists := destinationExists(op.Destination); exists {
							why = "overwrites an existing file"
						}
					}
					if why == "" {
						continue
					}

					emitDiagnostic(activeFmt, activeCompact,
						fmt.Errorf("operation %d %s, which requires --confirm", i+1, why),
						goaxi.ExitUsage,
						[]string{"Confirm the batch: re-run the same command with --confirm (over MCP, pass confirm: true)"})
					return
				}
			}

			result, err := core.BatchFileOperations(core.BatchFileOperationsOptions{
				Operations:  operations,
				AllowedDirs: GetAllowedDirs(),
			})
			if err != nil {
				OutputError(err)
				return
			}
			OutputResultAXI(result, nil, func() []string {
				var nums []int
				var details []string
				for i, op := range result.Operations {
					if op.Success {
						continue
					}
					desc := operations[i].Operation + " " + operations[i].Source
					if operations[i].Destination != "" {
						desc += " -> " + operations[i].Destination
					}
					nums = append(nums, i+1)
					details = append(details, fmt.Sprintf("operation %d (%s): %s", i+1, desc, op.Message))
				}
				return failedEntrySteps("operations", "operation", "operations", nums, details)
			}, func() string {
				return fmt.Sprintf("Batch complete: %d success, %d failed",
					result.Success, result.Failed)
			})

			// As in write-multiple-files: the result names the failed
			// entries, and the exit code says the batch did not succeed.
			if result.Failed > 0 {
				exitFunc(int(goaxi.ExitError))
				return
			}
		},
	}

	cmd.Flags().StringVar(&operationsJSON, "operations", "",
		"JSON array of operations: "+operationsExample)
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm destructive entries in the batch: a delete, or a move or copy onto an existing path")
	cmd.MarkFlagRequired("operations")

	return cmd
}
