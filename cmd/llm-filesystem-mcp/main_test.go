package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	cliPath    = "../../build/llm-filesystem"
	serverPath = "../../build/llm-filesystem-mcp"
)

// requireBinaries returns absolute paths to the built CLI and server. Locally
// an absent binary skips; when CI is set it fails, because a skipped
// end-to-end test in CI reports a green run that exercised nothing (TD #24,
// as requireBinary in cmd/llm-filesystem does).
func requireBinaries(t *testing.T) (cli, server string) {
	t.Helper()
	for _, p := range []string{cliPath, serverPath} {
		switch missingBinary(p) {
		case binaryFatal:
			t.Fatalf("%s not built; CI must run make build before go test", p)
		case binarySkip:
			t.Skipf("%s not built; run make build", p)
		}
	}
	cli, _ = filepath.Abs(cliPath)
	server, _ = filepath.Abs(serverPath)
	return cli, server
}

type binaryState int

const (
	binaryPresent binaryState = iota
	binarySkip
	binaryFatal
)

func missingBinary(path string) binaryState {
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		return binaryPresent
	}
	if os.Getenv("CI") != "" {
		return binaryFatal
	}
	return binarySkip
}

// If the CI branch is deleted, the end-to-end tests go back to skipping under
// CI and a green run proves nothing.
func TestMissingBinaryIsFatalUnderCI(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "no-such-binary")

	t.Setenv("CI", "1")
	if got := missingBinary(absent); got != binaryFatal {
		t.Errorf("CI set, binary absent: got %v, want binaryFatal", got)
	}
	t.Setenv("CI", "")
	if got := missingBinary(absent); got != binarySkip {
		t.Errorf("CI unset, binary absent: got %v, want binarySkip", got)
	}
	t.Setenv("CI", "1")
	if got := missingBinary(os.Args[0]); got != binaryPresent {
		t.Errorf("binary present: got %v, want binaryPresent", got)
	}
}

// connect starts the built server over stdio, pointed at the built CLI, and
// completes the MCP initialize handshake.
func connect(t *testing.T) (*mcp.ClientSession, *mcp.InitializeResult, string) {
	t.Helper()
	cli, server := requireBinaries(t)

	cmd := exec.Command(server)
	cmd.Env = append(os.Environ(), "LLM_FILESYSTEM_BINARY="+cli)
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, session.InitializeResult(), cli
}

func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	var parts []string
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return r, strings.Join(parts, "\n")
}

// Defends AC1: the server answers initialize and tools/list, and runs a write
// and a read through the real CLI. The write is checked on disk, not by its
// reply: a write that reports success and leaves nothing is the failure this
// repo exists to prevent.
func TestEndToEndOverStdio(t *testing.T) {
	session, init, _ := connect(t)
	if init.ServerInfo.Name != "llm-filesystem-mcp" {
		t.Errorf("server name = %q", init.ServerInfo.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"llm_filesystem_read_file", "llm_filesystem_write_file", "llm_filesystem_delete_file"} {
		if !names[want] {
			t.Errorf("tools/list lacks %s", want)
		}
	}
	for _, banned := range []string{"llm_filesystem_setup", "llm_filesystem_help", "llm_filesystem_completion"} {
		if names[banned] {
			t.Errorf("tools/list has excluded %s", banned)
		}
	}

	path := filepath.Join(t.TempDir(), "note.txt")
	const content = "line one, with a comma\n-line two\n"
	r, text := call(t, session, "llm_filesystem_write_file", map[string]any{"path": path, "content": content})
	if r.IsError {
		t.Fatalf("write failed: %s", text)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != content {
		t.Fatalf("on disk after write: %q, %v; want %q", got, err, content)
	}

	r, text = call(t, session, "llm_filesystem_read_file", map[string]any{"path": path})
	if r.IsError {
		t.Fatalf("read failed: %s", text)
	}
	if !strings.Contains(text, "line one, with a comma") || !strings.Contains(text, "-line two") {
		t.Errorf("read returned %q", text)
	}
}

// Defends the plan's failure rule: a failing command comes back as isError
// with the CLI's own error text, never as a success.
func TestFailingCommandIsAnErrorResult(t *testing.T) {
	session, _, _ := connect(t)
	missing := filepath.Join(t.TempDir(), "absent.txt")
	r, text := call(t, session, "llm_filesystem_read_file", map[string]any{"path": missing})
	if !r.IsError {
		t.Fatalf("reading a missing file reported success: %s", text)
	}
	for _, want := range []string{"error: true", "no such file or directory", "exit_code: 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("error result %q lacks %q", text, want)
		}
	}
}

// Defends the confirm rule end to end: a delete without confirm is refused by
// the CLI (exit 2) and the file is still there. Only the file proves it.
func TestDeleteWithoutConfirmTouchesNothing(t *testing.T) {
	session, _, _ := connect(t)
	path := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, text := call(t, session, "llm_filesystem_delete_file", map[string]any{"path": path})
	if !r.IsError || !strings.Contains(text, "exit_code: 2") {
		t.Errorf("delete without confirm: error=%v %q", r.IsError, text)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file gone after an unconfirmed delete: %v", err)
	}

	r, text = call(t, session, "llm_filesystem_delete_file", map[string]any{"path": path, "confirm": true})
	if r.IsError {
		t.Fatalf("confirmed delete failed: %s", text)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still there after a confirmed delete: %v", err)
	}
}

// Defends the argument guards at the MCP boundary (review of PR #62). Without
// them a bad call reaches the runner with no argv, runs the bare root, exits 0
// and returns the home screen as a success. Each bad call must come back as a
// usage error, and the write it names must not have happened.
func TestBadArgumentsAreRefusedAndRunNothing(t *testing.T) {
	session, _, _ := connect(t)
	dir := t.TempDir()

	cases := map[string]any{
		"unknown parameter":     map[string]any{"path": filepath.Join(dir, "a.txt"), "content": "x", "file_path": "y"},
		"wrongly typed":         map[string]any{"path": filepath.Join(dir, "b.txt"), "content": 42},
		"arguments not objects": []any{filepath.Join(dir, "c.txt"), "x"},
	}
	for name, args := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "llm_filesystem_write_file", Arguments: args})
		cancel()
		if err != nil {
			// The SDK refusing the call at the protocol level is also a
			// refusal; what matters is that nothing ran.
			t.Logf("%s: refused by the protocol: %v", name, err)
			continue
		}
		var text strings.Builder
		for _, c := range r.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text.WriteString(tc.Text + "\n")
			}
		}
		if !r.IsError {
			t.Errorf("%s: reported success: %s", name, text.String())
		}
		if !strings.Contains(text.String(), "exit_code: 2") {
			t.Errorf("%s: not a usage error: %s", name, text.String())
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a refused call wrote %d file(s): %v", len(entries), entries)
	}
}

var helpFlag = regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

// Defends AC2 against the shipped binary rather than the source tree: the
// server and the CLI are separate executables, and a server built from a
// newer tree than the CLI would advertise flags the CLI rejects. Every tool
// must name an existing command, and every parameter a flag in its --help.
func TestToolsMatchTheBuiltCLI(t *testing.T) {
	session, _, cli := connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	rootHelp, err := exec.Command(cli, "--help").Output()
	if err != nil {
		t.Fatalf("%s --help: %v", cli, err)
	}
	commands := map[string]bool{}
	inList := false
	for _, line := range strings.Split(string(rootHelp), "\n") {
		switch {
		case strings.HasPrefix(line, "Available Commands:"):
			inList = true
		case inList && strings.TrimSpace(line) == "":
			inList = false
		case inList:
			commands[strings.Fields(line)[0]] = true
		}
	}
	for _, excluded := range []string{"completion", "help", "setup"} {
		if !commands[excluded] {
			t.Errorf("the CLI no longer has %s; the exclusion list is stale", excluded)
		}
		delete(commands, excluded)
	}
	if len(list.Tools) != len(commands) {
		t.Errorf("%d tools for %d operational CLI commands", len(list.Tools), len(commands))
	}

	for _, tool := range list.Tools {
		command := strings.ReplaceAll(strings.TrimPrefix(tool.Name, "llm_filesystem_"), "_", "-")
		if !commands[command] {
			t.Errorf("tool %s has no CLI command %q", tool.Name, command)
			continue
		}
		help, err := exec.Command(cli, command, "--help").Output()
		if err != nil {
			t.Fatalf("%s %s --help: %v", cli, command, err)
		}
		flags := map[string]bool{}
		for _, m := range helpFlag.FindAllStringSubmatch(string(help), -1) {
			flags[m[1]] = true
		}
		schema, _ := tool.InputSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		for prop := range props {
			if flag := strings.ReplaceAll(prop, "_", "-"); !flags[flag] {
				t.Errorf("tool %s names %q; `%s --help` has no --%s", tool.Name, prop, command, flag)
			}
		}
	}
}

// The not-found error names the variable and the paths tried, and the server
// exits instead of starting with nothing to run.
func TestServerRefusesToStartWithoutTheCLI(t *testing.T) {
	_, server := requireBinaries(t)
	missing := filepath.Join(t.TempDir(), "llm-filesystem")
	cmd := exec.Command(server)
	cmd.Env = append(os.Environ(), "LLM_FILESYSTEM_BINARY="+missing)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("server started with no CLI: %s", out)
	}
	for _, want := range []string{"LLM_FILESYSTEM_BINARY", missing} {
		if !strings.Contains(string(out), want) {
			t.Errorf("startup error %q lacks %q", out, want)
		}
	}
}
