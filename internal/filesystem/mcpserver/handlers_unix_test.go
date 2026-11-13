//go:build !windows

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Defends epic 09 Q4: a hung child is killed at the timeout, and the error
// says a write may be partial, because the CLI may have been mid-write when
// it died and the caller must check before retrying.
func TestRunTimeoutKillsTheChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	bin := fakeCLI(t, `echo $$ > `+pidFile+`; exec sleep 30`)

	start := time.Now()
	r := Runner{Binary: bin, Timeout: 300 * time.Millisecond}.Run(context.Background(), []string{"write-file"})
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Run returned after %v; the timeout did not fire", elapsed)
	}
	if !r.IsError {
		t.Fatal("a timed-out call reported success")
	}
	text := resultText(r)
	for _, want := range []string{"write-file", "300ms", "killed", "partial"} {
		if !strings.Contains(text, want) {
			t.Errorf("timeout error %q does not mention %q", text, want)
		}
	}

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the fake CLI never started: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err := syscall.Kill(pid, 0); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("child %d is still running after the timeout", pid)
	}
}
