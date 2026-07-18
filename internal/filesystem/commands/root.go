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
	fullFlag    bool
	allowedDirs []string

	// Resolved output mode, set in PersistentPreRunE from the flags above.
	activeFmt     = FormatTOON
	activeCompact bool
	activeFull    bool
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
			activeFull = resolveFull(cmd.Flags().Changed("full"), fullFlag, os.Getenv(FullEnvVar))
			return nil
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&formatFlag, "format", "toon",
		"Output format: toon (default, token-efficient), json, or text")
	rootCmd.PersistentFlags().BoolVar(&fullFlag, "full", false,
		"Emit all fields instead of the minimal default (env: "+FullEnvVar+")")
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

// OutputResult renders the result in the active output format (toon/json/text)
// with no minimal projection or next-step hints. Non-list commands use this.
func OutputResult(result interface{}, textFn func() string) {
	OutputResultAXI(result, nil, nil, textFn)
}

// OutputResultAXI renders result applying the AXI experience: a minimal field
// projection (spec maps array field -> kept item keys) when not in --full mode,
// and next-step hints (AXI #9). The full + JSON combination is kept
// byte-identical to the pre-AXI --json output for legacy consumers.
func OutputResultAXI(result interface{}, spec map[string][]string, steps []string, textFn func() string) {
	// Human text: render the text body, then append hints as trailing lines.
	if activeFmt == FormatText {
		out := textFn()
		if len(steps) > 0 {
			out += "\n\nNext steps:"
			for _, s := range steps {
				out += "\n  - " + s
			}
		}
		fmt.Println(out)
		return
	}

	// Full + JSON: legacy byte-identical path — no projection, no hints.
	if activeFmt == FormatJSON && activeFull {
		var b []byte
		if activeCompact {
			b, _ = json.Marshal(result)
		} else {
			b, _ = json.MarshalIndent(result, "", "  ")
		}
		fmt.Println(string(b))
		return
	}

	// Everything else renders through the generic representation so projection
	// and hint injection can apply consistently across TOON and JSON.
	payload, err := toGeneric(result)
	if err != nil {
		b, _ := json.Marshal(result)
		fmt.Println(string(b))
		return
	}
	if !activeFull && spec != nil {
		payload = projectGeneric(payload, spec)
	}
	payload = injectNextSteps(payload, steps)

	out, rerr := renderGeneric(activeFmt, activeCompact, payload)
	if rerr != nil {
		b, _ := json.Marshal(payload)
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
