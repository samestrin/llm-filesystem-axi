package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cliBinary points BinaryPath at the built CLI for the duration of a test,
// skipping when it is absent.
func cliBinary(t *testing.T) {
	t.Helper()

	path, err := filepath.Abs("../../../build/llm-filesystem")
	if err != nil {
		t.Fatalf("resolving binary path: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("binary not built; run make build")
	}

	prev := BinaryPath
	BinaryPath = path
	t.Cleanup(func() { BinaryPath = prev })
}

// The server appends "--format toon" to request token-efficient output, but it
// appends it AFTER the per-command args. compress-files defines its own
// --format for the ARCHIVE type, so the later value wins and the archive format
// becomes "toon" — a format no archiver supports.
//
// The output format still needs to be TOON; that is already the CLI default, so
// nothing is lost by leaving the flag off when the command supplies its own.
func TestExecuteHandlerDoesNotClobberTheArchiveFormat(t *testing.T) {
	args, err := buildCommandArgs("compress_files", map[string]interface{}{
		"paths":  []interface{}{"/tmp/x.txt"},
		"output": "/tmp/x.tar.gz",
		"format": "tar.gz",
	})
	if err != nil {
		t.Fatalf("buildCommandArgs: %v", err)
	}

	var formats []string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--format" {
			formats = append(formats, args[i+1])
		}
	}

	if len(formats) != 1 {
		t.Fatalf("--format appears %d times (%v); a second one silently overrides the first", len(formats), formats)
	}
	if formats[0] != "tar.gz" {
		t.Errorf("--format = %q, want the archive format tar.gz", formats[0])
	}
}

// A command with no --format of its own must still get the TOON request.
func TestExecuteHandlerStillRequestsTOON(t *testing.T) {
	args, err := buildCommandArgs("read_file", map[string]interface{}{"path": "/tmp/x.txt"})
	if err != nil {
		t.Fatalf("buildCommandArgs: %v", err)
	}

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--format toon") {
		t.Errorf("args = %v, want a --format toon request", args)
	}
}

// End-to-end: the archive is actually produced, rather than the CLI rejecting
// "toon" as an archive format.
func TestExecuteHandlerCompressFilesProducesAnArchive(t *testing.T) {
	cliBinary(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	out := filepath.Join(dir, "out.tar.gz")

	body, err := ExecuteHandler(ToolPrefix+"compress_files", map[string]interface{}{
		"paths":  []interface{}{src},
		"output": out,
		"format": "tar.gz",
	})
	if err != nil {
		t.Fatalf("compress_files failed: %v\nbody: %s", err, body)
	}
	if strings.Contains(body, "unsupported format") {
		t.Errorf("the archive format was clobbered: %s", body)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Errorf("no archive produced: %v\nbody: %s", statErr, body)
	}
}

// A failing command returned (output, nil) — a SUCCESS carrying an error body.
// The server sets IsError from the returned error, so a real tool failure
// reached the model flagged OK, while the cases that exited silently (no output)
// were the only ones flagged as errors. The flag was inverted relative to
// reality.
//
// The body must still come back: it carries the structured error payload the
// model needs. So both are returned.
func TestExecuteHandlerReportsAFailingCommandAsAnError(t *testing.T) {
	cliBinary(t)

	body, err := ExecuteHandler(ToolPrefix+"read_file", map[string]interface{}{
		"path": "/definitely/not/here.txt",
	})

	if err == nil {
		t.Fatalf("a failing command must return an error; got success with body %q", body)
	}
	if strings.TrimSpace(body) == "" {
		t.Error("the structured error body must still reach the caller")
	}
	if !strings.Contains(body, "error") {
		t.Errorf("body = %q, want the structured error payload", body)
	}
}

// A successful command keeps reporting success, with its payload.
func TestExecuteHandlerReportsSuccessNormally(t *testing.T) {
	cliBinary(t)

	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}

	body, err := ExecuteHandler(ToolPrefix+"read_file", map[string]interface{}{"path": src})
	if err != nil {
		t.Fatalf("read_file failed: %v\nbody: %s", err, body)
	}
	if !strings.Contains(body, "hello") {
		t.Errorf("body = %q, want the file contents", body)
	}
}
