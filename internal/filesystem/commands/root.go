package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags
var Version = "1.0.0"

var (
	// Global flags
	formatFlag  string
	jsonOutput  bool
	minOutput   bool
	allowedDirs []string

	// Resolved output mode, set in PersistentPreRunE from the flags above.
	activeFmt     = FormatTOON
	activeCompact bool
)

// RootCmd returns the root command for llm-filesystem
func RootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "llm-filesystem",
		Short:   "High-performance filesystem operations CLI",
		Version: Version,
		Long: `llm-filesystem provides fast file operations for Claude Code and CLI usage.

It supports 27 commands for reading, writing, editing, and managing files.
Output defaults to token-efficient TOON; use --format json for machine parsing.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		// Resolve the output format once, before any subcommand runs. An
		// invalid --format fails loud here (non-zero exit) rather than
		// silently defaulting.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f, compact, err := resolveFormat(formatFlag, cmd.Flags().Changed("format"), jsonOutput, minOutput)
			if err != nil {
				return err
			}
			activeFmt, activeCompact = f, compact
			return nil
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&formatFlag, "format", "toon",
		"Output format: toon (default, token-efficient), json, or text")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output as JSON (deprecated: use --format json)")
	rootCmd.PersistentFlags().BoolVar(&minOutput, "min", false, "Minimal/compact output (deprecated)")
	rootCmd.PersistentFlags().StringSliceVar(&allowedDirs, "allowed-dirs", nil,
		"Directories the tool is allowed to access (comma-separated)")

	// Add all subcommands
	addReadCommands(rootCmd)
	addWriteCommands(rootCmd)
	addEditCommands(rootCmd)
	addDirectoryCommands(rootCmd)
	addSearchCommands(rootCmd)
	addFileOpsCommands(rootCmd)
	addAdvancedCommands(rootCmd)

	return rootCmd
}

// GetAllowedDirs returns the expanded allowed directories
func GetAllowedDirs() []string {
	expanded := make([]string, 0, len(allowedDirs))
	for _, dir := range allowedDirs {
		if strings.HasPrefix(dir, "~") {
			home, err := os.UserHomeDir()
			if err == nil {
				dir = strings.Replace(dir, "~", home, 1)
			}
		}
		expanded = append(expanded, dir)
	}
	return expanded
}

// OutputResult renders the result in the active output format (toon/json/text).
func OutputResult(result interface{}, textFn func() string) {
	out, err := renderResult(activeFmt, activeCompact, result, textFn)
	if err != nil {
		// Defensive fallback (e.g. a TOON encode failure): emit compact JSON
		// rather than nothing, without recursing through OutputError.
		b, _ := json.Marshal(result)
		out = string(b)
	}
	fmt.Println(out)
}

// OutputError renders an error in the active output format and exits non-zero.
// Plain text goes to stderr; structured formats (json, toon) go to stdout so
// the caller receives a parseable body.
func OutputError(err error) {
	rendered := renderError(activeFmt, activeCompact, err)
	if activeFmt == FormatText {
		fmt.Fprintln(os.Stderr, rendered)
	} else {
		fmt.Println(rendered)
	}
	os.Exit(1)
}

// Execute runs the root command
func Execute() {
	if err := RootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
