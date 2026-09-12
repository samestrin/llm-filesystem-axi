package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	goaxi "github.com/samestrin/go-axi"
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

// Output sinks and the exit hook, indirected so the paths that actually ship can
// be tested. OutputResultAXI and OutputError used to write straight to
// os.Stdout/os.Stderr and call os.Exit, which left the live render path with no
// test at all while every output test exercised a function production never
// called.
var (
	outWriter io.Writer = os.Stdout
	errWriter io.Writer = os.Stderr
	exitFunc            = os.Exit
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
		fmt.Fprintln(outWriter, out)
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
		fmt.Fprintln(outWriter, string(b))
		return
	}

	// Everything else renders through the generic representation so projection
	// and hint injection can apply consistently across TOON and JSON.
	payload, err := toGeneric(result)
	if err != nil {
		// This used to print raw json.Marshal(result) and return zero, so a
		// caller that asked for TOON silently received JSON with no marker.
		OutputError(fmt.Errorf("cannot represent result: %w", err))
		return
	}
	if !activeFull && spec != nil {
		payload = projectGeneric(payload, spec)
	}

	// Contextual disclosure (AXI principle 9) takes a different shape per
	// format, because the formats have different rules about what one document
	// is. TOON gets a trailing help[] block: the inline array is the only form
	// in circulation that survives its own codec, so body and block decode as a
	// single value. JSON cannot take an appended TOON line without ceasing to
	// be one JSON document, so it keeps the payload field it has always had.
	if activeFmt == FormatJSON {
		payload = injectNextSteps(payload, steps)
	}

	out, rerr := renderGeneric(activeFmt, activeCompact, payload)
	if rerr != nil {
		// Same reasoning as above: refuse rather than emit a payload in a
		// format the caller did not ask for and cannot detect.
		OutputError(rerr)
		return
	}

	// The payload and the help block are ONE document, so they are assembled
	// first and written once. Writing them separately left a window where the
	// payload landed and the block did not; the caller then received an error
	// body appended to a half-written document, which is two conflicting
	// documents on one stream.
	var doc bytes.Buffer
	doc.WriteString(out)
	doc.WriteByte('\n')

	if activeFmt == FormatTOON {
		// WriteHelp writes nothing for an empty list, so a command with no
		// meaningful next step emits no stray block. It sanitizes each line,
		// which matters because step text interpolates caller-supplied paths.
		if err := goaxi.WriteHelp(&doc, steps); err != nil {
			OutputError(err)
			return
		}
	}

	if _, err := outWriter.Write(doc.Bytes()); err != nil {
		// The sink is gone, so a structured body cannot reach it either. Report
		// on stderr and fail; do not retry the payload through OutputError.
		fmt.Fprintln(errWriter, "Error: "+err.Error())
		exitFunc(1)
	}
}

// OutputError renders an error in the active output format and exits non-zero.
// Plain text goes to stderr; structured formats (json, toon) go to stdout so
// the caller receives a parseable body.
func OutputError(err error) {
	rendered := renderError(activeFmt, activeCompact, err)
	if activeFmt == FormatText {
		fmt.Fprintln(errWriter, rendered)
	} else {
		fmt.Fprintln(outWriter, rendered)
	}
	exitFunc(1)
}

// Execute runs the root command
func Execute() {
	if err := RootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
