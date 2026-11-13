package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TimeoutEnvVar sets the per-call timeout: a Go duration ("90s", "2m") or a
// whole number of seconds.
const TimeoutEnvVar = "LLM_FILESYSTEM_MCP_TIMEOUT"

// DefaultTimeout is how long one call may run when TimeoutEnvVar is unset.
const DefaultTimeout = 120 * time.Second

// waitDelay bounds how long Run waits for the child's output pipes after
// killing it, in case something it started still holds them open.
const waitDelay = 2 * time.Second

// TimeoutFromEnv reads the per-call timeout. A value that is not a positive
// duration is an error, so the server refuses to start rather than running
// every call with a timeout the operator did not ask for.
func TimeoutFromEnv(getenv func(string) string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(TimeoutEnvVar))
	if raw == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		secs, serr := strconv.Atoi(raw)
		if serr != nil {
			return 0, fmt.Errorf("%s=%q is not a duration (e.g. 90s, 2m) or a number of seconds", TimeoutEnvVar, raw)
		}
		d = time.Duration(secs) * time.Second
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s=%q must be greater than zero", TimeoutEnvVar, raw)
	}
	return d, nil
}

// Runner runs the llm-filesystem binary for one tool call.
type Runner struct {
	Binary  string
	Timeout time.Duration
}

// Run executes the CLI with argv and maps the outcome to an MCP result.
//
// Exit 0 is a success carrying the CLI's TOON. Any other exit is an error
// result (isError) carrying the CLI's own TOON error and the exit code: 1 is
// a failure, 2 a usage error where nothing was attempted. llm-data-axi's
// server returns a failing command's output as a success; that is the one
// part of its pattern not copied here.
//
// The child gets no stdin: it must never read the MCP stream the server is
// speaking on.
func (r Runner) Run(ctx context.Context, argv []string) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.Binary, argv...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = waitDelay
	err := cmd.Run()

	command := r.Binary
	if len(argv) > 0 {
		command = "llm-filesystem " + argv[0]
	}

	// Only a run that did not complete was killed: one that exited 0 just
	// before the deadline did all its work.
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
		why := fmt.Sprintf("did not finish within %s", r.Timeout)
		if !errors.Is(ctxErr, context.DeadlineExceeded) {
			why = "was cancelled by the client"
		}
		return errorResult(output(&stdout, &stderr), fmt.Sprintf(
			"%s %s and was killed. A write may be partial: check the target before retrying. (Set %s to allow longer calls.)",
			command, why, TimeoutEnvVar))
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return &mcp.CallToolResult{Content: textContent(output(&stdout, &stderr))}
	case errors.As(err, &exitErr):
		return errorResult(output(&stdout, &stderr), fmt.Sprintf("exit_code: %d", exitErr.ExitCode()))
	default:
		return errorResult(nil, fmt.Sprintf("could not run %s: %v", r.Binary, err))
	}
}

// output is what the CLI printed: stdout, then stderr when there was any.
func output(stdout, stderr *bytes.Buffer) []string {
	var parts []string
	if stdout.Len() > 0 {
		parts = append(parts, stdout.String())
	}
	if stderr.Len() > 0 {
		parts = append(parts, stderr.String())
	}
	return parts
}

func errorResult(parts []string, note string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: textContent(append(parts, note)), IsError: true}
}

func textContent(parts []string) []mcp.Content {
	content := make([]mcp.Content, len(parts))
	for i, p := range parts {
		content[i] = &mcp.TextContent{Text: p}
	}
	if len(content) == 0 {
		// The protocol requires a content array; an empty one is still one.
		content = []mcp.Content{}
	}
	return content
}
