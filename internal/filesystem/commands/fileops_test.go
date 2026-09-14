package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// newTempFile creates a file that a delete test can try to destroy.
func newTempFile(t *testing.T) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(p, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// AC11: delete-file took a path and deleted it, with no way to express intent
// separately from the target. For a tool driven by an agent that is one
// hallucinated path away from data loss.
//
// Asserting the file STILL EXISTS is the load-bearing half of each case. An
// exit-code assertion alone would pass even if the delete happened and then
// something errored afterwards — which is exactly the failure the gate exists to
// prevent.
//
// A missing --confirm is a usage error, not a tool failure: nothing was
// attempted, so nothing failed. Exit 1 would tell the agent the tool tried and
// broke, and the rational response to that is retrying the identical command.
// Exit 2 says "fix the invocation", which is the action actually required.
func TestDeleteFileRefusesWithoutConfirm(t *testing.T) {
	t.Run("missing --confirm refuses and leaves the file", func(t *testing.T) {
		f := newTempFile(t)

		_, _, code := runCLI(t, "delete-file", "--path", f)

		if code != int(goaxi.ExitUsage) {
			t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
		}
		if _, err := os.Stat(f); err != nil {
			t.Errorf("the file was deleted without --confirm: %v", err)
		}
	})

	// cobra's required-flag check is satisfied by --confirm=false, because the
	// flag WAS provided. Only inspecting the value closes that hole.
	t.Run("--confirm=false refuses and leaves the file", func(t *testing.T) {
		f := newTempFile(t)

		_, _, code := runCLI(t, "delete-file", "--path", f, "--confirm=false")

		if code != int(goaxi.ExitUsage) {
			t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
		}
		if _, err := os.Stat(f); err != nil {
			t.Errorf("--confirm=false still deleted the file: %v", err)
		}
	})

	// The gate must not become a wall: a confirmed delete still deletes.
	t.Run("--confirm deletes", func(t *testing.T) {
		f := newTempFile(t)

		_, _, code := runCLI(t, "delete-file", "--path", f, "--confirm")

		if code == int(goaxi.ExitUsage) {
			t.Fatal("a confirmed delete was rejected as a usage error")
		}
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Error("--confirm did not delete the file")
		}
	})
}

// AC11: gating delete-file alone would be theatre. batch-file-operations accepts
// {"operation":"delete"} and routes it to the same core.DeleteFile, so an agent
// that meets the gate steps around it in one hop and the guarantee is worth
// nothing.
//
// Note the operation carries the target in "source", not "path" — the batch
// dispatcher maps op.Source into DeleteFileOptions.Path.
func TestBatchDeleteRequiresConfirm(t *testing.T) {
	f := newTempFile(t)
	ops := `[{"operation":"delete","source":"` + f + `"}]`

	_, _, code := runCLI(t, "batch-file-operations", "--operations", ops)

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("a batch delete ran without --confirm: %v", err)
	}
}

// Malformed input must fail loud rather than decode to an empty batch, which
// would report a cheerful "0 success, 0 failed" for work that was never even
// described — a success reported for nothing done.
func TestBatchRejectsMalformedOperations(t *testing.T) {
	stdout, _, code := runCLI(t, "batch-file-operations", "--operations", "not json at all")

	if code == int(goaxi.ExitOK) || code == -1 {
		t.Errorf("malformed operations JSON was accepted, exit = %d: %q", code, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "invalid operations json") {
		t.Errorf("the diagnostic must name the problem: %q", stdout)
	}
}

// The gate matched only "delete", so a move or a copy onto an EXISTING file
// destroyed it with no confirmation at all: os.Rename and os.Create both replace
// the destination silently, and the batch then reports success.
//
// The gate's own rationale is that gating delete-file while leaving the batch
// open "would just move the hole". The hole moved one operation across.
func TestBatchOverwriteRequiresConfirm(t *testing.T) {
	const precious = "IRREPLACEABLE"

	for _, op := range []string{"move", "copy"} {
		t.Run(op+" over an existing file", func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.txt")
			victim := filepath.Join(dir, "victim.txt")
			if err := os.WriteFile(src, []byte("replacement"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(victim, []byte(precious), 0o600); err != nil {
				t.Fatal(err)
			}

			ops := `[{"operation":"` + op + `","source":"` + src + `","destination":"` + victim + `"}]`
			_, _, code := runCLI(t, "batch-file-operations", "--operations", ops)

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d) for an unconfirmed overwrite", code, goaxi.ExitUsage)
			}
			got, err := os.ReadFile(victim)
			if err != nil {
				t.Fatalf("the destination was removed entirely: %v", err)
			}
			if string(got) != precious {
				t.Errorf("destination = %q, want it untouched (%q)", got, precious)
			}
		})
	}
}

// The gate must stay scoped to what actually destroys something: a move or copy
// to a path that does not exist yet overwrites nothing and needs no confirming.
func TestBatchNonOverwritingMoveNeedsNoConfirm(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "fresh.txt")

	ops := `[{"operation":"move","source":"` + src + `","destination":"` + dst + `"}]`
	_, _, code := runCLI(t, "batch-file-operations", "--operations", ops)

	if code == int(goaxi.ExitUsage) {
		t.Error("a move that overwrites nothing was gated")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("the move did not run: %v", err)
	}
}

// The gate is scoped to what is destructive. A batch that only copies must not
// be made harder to use by it.
func TestBatchWithoutDeleteNeedsNoConfirm(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "b.txt")
	ops := `[{"operation":"copy","source":"` + src + `","destination":"` + dst + `"}]`

	_, _, code := runCLI(t, "batch-file-operations", "--operations", ops)

	if code == int(goaxi.ExitUsage) {
		t.Error("a batch containing no delete was gated")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("the copy did not run: %v", err)
	}
}
