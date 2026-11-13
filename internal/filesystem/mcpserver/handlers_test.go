package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func toolFor(t *testing.T, command string) Tool {
	t.Helper()
	for _, tool := range mustTools(t) {
		if tool.Command == command {
			return tool
		}
	}
	t.Fatalf("no tool for %s", command)
	return Tool{}
}

func TestArgsBuildsFlagsFromParameters(t *testing.T) {
	tool := toolFor(t, "write-file")
	got, err := tool.Args(map[string]any{
		"path":    "/tmp/a b.txt",
		"content": "-starts with a dash",
		"append":  true,
	})
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	// Flags come out sorted, always as --name=value: a value that starts with
	// a dash is never read as a flag of its own.
	want := []string{"write-file", "--append=true", "--content=-starts with a dash", "--path=/tmp/a b.txt", "--format=toon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args = %q\nwant   %q", got, want)
	}
}

// Defends the plan's confirm rule: a destructive tool passes --confirm only
// when the caller sets it. A server that added it on its own would turn every
// MCP delete into a confirmed one.
func TestConfirmIsPassedOnlyWhenTheCallerSetsIt(t *testing.T) {
	tool := toolFor(t, "delete-file")

	got, err := tool.Args(map[string]any{"path": "/x"})
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	for _, a := range got {
		if strings.HasPrefix(a, "--confirm") {
			t.Errorf("caller did not set confirm, argv has %q", a)
		}
	}

	got, err = tool.Args(map[string]any{"path": "/x", "confirm": true})
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	if !contains(got, "--confirm=true") {
		t.Errorf("caller set confirm, argv = %q", got)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// A string-slice flag is parsed by pflag as CSV, so a path that contains a
// comma must be quoted or it arrives as two paths.
func TestSliceParametersSurviveCommas(t *testing.T) {
	tool := toolFor(t, "read-multiple-files")
	got, err := tool.Args(map[string]any{"paths": []any{"/a,b.txt", "/c.txt"}})
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	if !contains(got, `--paths="/a,b.txt",/c.txt`) {
		t.Errorf("argv = %q", got)
	}
}

func TestIntegerParameters(t *testing.T) {
	tool := toolFor(t, "edit-file")
	got, err := tool.Args(map[string]any{"path": "/p", "operation": "insert", "line": float64(3)})
	if err != nil {
		t.Fatalf("Args: %v", err)
	}
	if !contains(got, "--line=3") {
		t.Errorf("argv = %q", got)
	}
	if _, err := tool.Args(map[string]any{"path": "/p", "line": 2.5}); err == nil {
		t.Error("a fractional line was accepted")
	}
}

// A wrong key used to be the worst failure in this repo: a payload that does
// nothing and exits 0. An argument the tool does not have is refused, never
// dropped.
func TestUnknownOrMistypedArgumentsAreRefused(t *testing.T) {
	tool := toolFor(t, "read-file")
	cases := map[string]map[string]any{
		"unknown key":       {"path": "/p", "file_path": "/q"},
		"string as number":  {"path": 7},
		"bool as string":    {"path": "/p", "full": "yes"},
		"slice as string":   {"path": "/p", "fields": "a"},
		"slice of numbers":  {"path": "/p", "fields": []any{1}},
		"server-owned flag": {"path": "/p", "format": "json"},
	}
	for name, args := range cases {
		if _, err := tool.Args(args); err == nil {
			t.Errorf("%s: accepted %v", name, args)
		}
	}
	if _, err := tool.Args(map[string]any{"path": nil}); err != nil {
		t.Errorf("null is treated as absent, got %v", err)
	}
}

// fakeCLI writes an executable shell script standing in for llm-filesystem.
func fakeCLI(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}
	path := filepath.Join(t.TempDir(), "llm-filesystem")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func resultText(r *mcp.CallToolResult) string {
	var parts []string
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestRunSuccessReturnsTheCLIOutput(t *testing.T) {
	bin := fakeCLI(t, `echo "path: $2"`)
	r := Runner{Binary: bin, Timeout: 5 * time.Second}.Run(context.Background(), []string{"read-file", "--path=/x"})
	if r.IsError {
		t.Fatalf("exit 0 reported as an error: %s", resultText(r))
	}
	if got := resultText(r); got != "path: --path=/x\n" {
		t.Errorf("content = %q", got)
	}
}

// Defends the one thing not to copy from llm-data-axi: a non-zero exit must
// come back as isError with the CLI's own TOON error, never as a success.
func TestRunNonZeroExitIsAnError(t *testing.T) {
	for _, code := range []int{1, 2} {
		bin := fakeCLI(t, `echo 'error: true'; echo 'message: "boom"'; exit `+strconv.Itoa(code))
		r := Runner{Binary: bin, Timeout: 5 * time.Second}.Run(context.Background(), []string{"read-file"})
		if !r.IsError {
			t.Errorf("exit %d reported as success", code)
		}
		text := resultText(r)
		if !strings.Contains(text, `message: "boom"`) {
			t.Errorf("exit %d: the CLI's error is missing from %q", code, text)
		}
		if !strings.Contains(text, "exit_code: "+strconv.Itoa(code)) {
			t.Errorf("exit %d: the exit code is missing from %q", code, text)
		}
	}
}

func TestRunStderrIsKept(t *testing.T) {
	bin := fakeCLI(t, `echo 'warning: x' >&2; exit 1`)
	r := Runner{Binary: bin, Timeout: 5 * time.Second}.Run(context.Background(), []string{"read-file"})
	if !r.IsError || !strings.Contains(resultText(r), "warning: x") {
		t.Errorf("stderr lost: error=%v %q", r.IsError, resultText(r))
	}
}

func TestRunUnstartableBinaryIsAnError(t *testing.T) {
	r := Runner{Binary: filepath.Join(t.TempDir(), "gone"), Timeout: time.Second}.Run(context.Background(), []string{"read-file"})
	if !r.IsError || !strings.Contains(resultText(r), "gone") {
		t.Errorf("error=%v %q", r.IsError, resultText(r))
	}
}

func TestTimeoutFromEnv(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == TimeoutEnvVar {
				return v
			}
			return ""
		}
	}
	for in, want := range map[string]time.Duration{"": 120 * time.Second, "30s": 30 * time.Second, "45": 45 * time.Second, "2m": 2 * time.Minute} {
		got, err := TimeoutFromEnv(env(in))
		if err != nil || got != want {
			t.Errorf("%s=%q: got %v, %v; want %v", TimeoutEnvVar, in, got, err, want)
		}
	}
	// A bad value fails loud at startup rather than silently running every
	// call with a timeout the operator did not ask for.
	for _, bad := range []string{"soon", "0", "-5s"} {
		if _, err := TimeoutFromEnv(env(bad)); err == nil || !strings.Contains(err.Error(), TimeoutEnvVar) {
			t.Errorf("%s=%q: err = %v, want one naming the variable", TimeoutEnvVar, bad, err)
		}
	}
}
