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
	fieldsFlag  []string
	allowedDirs []string

	// Resolved output mode, set in PersistentPreRunE from the flags above.
	activeFmt     = FormatTOON
	activeCompact bool
	activeFull    bool
	activeFields  []string
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
		// AXI principle 6, content first: a bare invocation answers with live
		// data rather than a usage screen. Without a Run the root is not
		// Runnable, so cobra returns flag.ErrHelp and prints the help template —
		// the exact anti-pattern the principle names.
		//
		// This does not swallow an unknown subcommand. Cobra resolves the
		// command in Find, which fails before the Runnable check is reached, so
		// a typo still exits 2. --help and --version are handled earlier still.
		Run: runHome,
		// Resolve the output format once, before any subcommand runs. An
		// invalid --format fails loud here (non-zero exit) rather than
		// silently defaulting.
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f, compact, err := resolveFormat(formatFlag, cmd.Flags().Changed("format"), jsonOutput, minOutput)
			if err != nil {
				return err
			}
			activeFmt, activeCompact = f, compact

			fields, ferr := normalizeFields(fieldsFlag)
			if ferr != nil {
				return ferr
			}
			activeFields = fields

			// An explicit --fields beats the ambient LLM_FILESYSTEM_FULL
			// default; a contradictory explicit --full is rejected earlier by
			// the mutual-exclusion group. Forcing full off here is also what
			// keeps --fields away from the legacy JSON+full early return, which
			// skips projection entirely and would silently ignore it (AC12).
			activeFull = resolveFull(cmd.Flags().Changed("full"), fullFlag, os.Getenv(FullEnvVar)) &&
				len(activeFields) == 0
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
	rootCmd.PersistentFlags().StringSliceVar(&fieldsFlag, "fields", nil,
		"Comma-separated item fields to emit instead of the minimal set (mutually exclusive with --full)")
	rootCmd.PersistentFlags().StringSliceVar(&allowedDirs, "allowed-dirs", nil,
		"Directories the tool is allowed to access (comma-separated)")

	// Contradictory instructions, so cobra rejects them before the command runs.
	// Its error surfaces through execute, which means exit 2 and a structured
	// body come for free.
	rootCmd.MarkFlagsMutuallyExclusive("full", "fields")

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
// stepsFn is a closure rather than a slice so a command can decide its guidance
// from what it actually found. A fixed slice is built before the result exists,
// which is how a zero-match search came to offer "Open a match" — a step naming
// a target that was not there. nil is a valid value and means no guidance, which
// is why the 23 call sites that never had any compile unchanged.
func OutputResultAXI(result interface{}, spec map[string][]string, stepsFn func() []string, textFn func() string) {
	// Resolved once, not per format branch: JSON and TOON must never disagree
	// about what the next step is, and a command whose guidance is expensive to
	// build must not pay for it twice.
	var steps []string
	if stepsFn != nil {
		steps = stepsFn()
	}

	// Human text: render the text body, then append hints as trailing lines.
	if activeFmt == FormatText {
		fmt.Fprint(outWriter, textFn()+"\n"+textSteps(steps))
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
	if len(activeFields) > 0 {
		if spec == nil {
			emitDiagnostic(activeFmt, activeCompact,
				fmt.Errorf("--fields is not supported by this command (it returns no list)"),
				goaxi.ExitUsage, nil)
			return
		}

		// Validated against what the payload actually holds, which is exactly
		// the set --full would expose, ,omitempty included. An empty union means
		// an empty result, and rejecting there would fail a correct --fields
		// purely because the directory happened to have nothing in it.
		avail := availableFields(payload, spec)
		if missing := unknownFields(activeFields, avail); len(avail) > 0 && len(missing) > 0 {
			emitDiagnostic(activeFmt, activeCompact,
				fmt.Errorf("unknown --fields: %s", strings.Join(missing, ", ")),
				goaxi.ExitUsage,
				[]string{"Available fields: " + strings.Join(sortedFieldNames(avail), ", ")})
			return
		}

		spec = overrideSpec(spec, activeFields)
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
		exitFunc(int(goaxi.ExitError))
	}
}

// emitDiagnostic renders err in f and exits with code.
//
// Plain text goes to stderr, where a human reads it. Structured formats go to
// stdout, so an agent receives a parseable body on the stream it actually reads
// — AXI reserves stderr for logs. The two are exclusive on purpose: a diagnostic
// written to both streams is one event that a consumer merging them counts
// twice, which is what the MCP server's CombinedOutput would do.
//
// The exit code is a parameter rather than a constant because the same rendering
// serves two different situations. A malformed invocation and a failed operation
// need the same structured body and emphatically different codes.
// steps is recovery guidance and follows the same three-way split the success
// path uses: JSON carries it inside the document (renderError places it), TOON
// gets a trailing help[] block, text gets the human bullets. An error document
// therefore has the same shape as every other document this tool emits.
//
// Body and guidance are assembled and written once, for the reason the payload
// and its help block are: a consumer must never receive half of one document.
func emitDiagnostic(f Format, compact bool, err error, code goaxi.ExitCode, steps []string) {
	var doc bytes.Buffer
	doc.WriteString(renderError(f, compact, err, steps))
	doc.WriteByte('\n')

	switch f {
	case FormatTOON:
		// WriteHelp writes nothing for an empty list, so an error with no
		// actionable step emits no stray block.
		if herr := goaxi.WriteHelp(&doc, steps); herr != nil {
			// Guidance is an enhancement. Losing it must not also cost the
			// caller the diagnostic, which is the only part it can act on.
			doc.Reset()
			doc.WriteString(renderError(f, compact, err, nil))
			doc.WriteByte('\n')
		}
	case FormatText:
		doc.WriteString(textSteps(steps))
	}

	sink := outWriter
	if f == FormatText {
		sink = errWriter
	}
	// Best effort: this is already the failure path, so a write error here has
	// nowhere left to report itself except the exit code.
	_, _ = sink.Write(doc.Bytes())
	exitFunc(int(code))
}

// OutputError renders a TOOL failure in the active output format and exits
// ExitError: the operation was attempted and it did not work.
//
// It supplies no guidance. What to try after a failed operation depends on what
// failed, so a generic line here would be a guess — and AC10 forbids a help line
// the agent cannot act on. Call sites that know the recovery pass their own.
func OutputError(err error) {
	emitDiagnostic(activeFmt, activeCompact, err, goaxi.ExitError, nil)
}

// Execute runs the CLI against the real process arguments.
func Execute() {
	execute(os.Args[1:])
}

// execute runs the root command with the given args and selects the exit status.
//
// Any error arriving here is a USAGE error, and that is structural rather than a
// guess: every subcommand uses cobra's Run rather than RunE and reports its own
// failures through OutputError, which exits before returning. So the only errors
// that can reach this point are cobra's own parse and resolution failures —
// unknown subcommand, unknown flag, missing required flag, invalid --format.
//
// The error used to be discarded. SilenceErrors is set on the root command so
// cobra does not print it either, which meant all four classes exited 1 with
// nothing on either stream — a bare non-zero status and nothing to act on. The
// parseFormat message was built and thrown away. AXI principle 6 asks a tool to
// fail loud on unknown input.
//
// ExitUsage is deliberately distinct from ExitError. A typo and a broken tool
// are different situations, and an agent cannot decide whether a retry is
// worthwhile if they share a code.
//
// The diagnostic was plain text on stderr regardless of --format, so an agent
// running --format json got nothing parseable from a typo and stdout — the
// stream it reads — stayed empty. It now renders through the same structured
// path a tool failure takes, which is why the two differ only in their code.
//
// The format comes from argv rather than from activeFmt, because for an unknown
// flag or subcommand cobra fails during parsing and PersistentPreRunE, which is
// what assigns activeFmt, never runs at all.
func execute(args []string) {
	// Cobra falls back to os.Args[1:] when SetArgs is given nil. Production
	// never reaches that — a bare invocation yields an empty but non-nil slice —
	// but a caller passing nil would silently parse THIS process's arguments,
	// which under `go test` are the test binary's own flags. Normalizing here
	// beats relying on every future caller knowing.
	if args == nil {
		args = []string{}
	}

	cmd := RootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(outWriter)
	cmd.SetErr(errWriter)

	// ExecuteC returns the command cobra actually resolved: the root for an
	// unknown subcommand, and the subcommand itself for a bad flag or a missing
	// required one. Naming that beats scanning argv, which cannot tell a
	// subcommand from a flag's value.
	failed, err := cmd.ExecuteC()
	if err != nil {
		f, compact := formatFromArgs(args)

		path := "llm-filesystem"
		if failed != nil {
			path = failed.CommandPath()
		}

		emitDiagnostic(f, compact, err, goaxi.ExitUsage,
			[]string{"Valid flags and subcommands: " + path + " --help"})
	}
}
