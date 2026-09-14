package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// syncPair builds a small source tree and an empty destination.
func syncPair(t *testing.T) (src, dst string) {
	t.Helper()

	src = filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	return src, t.TempDir()
}

// AC11: a preview has to be distinguishable from a real run in the formats a
// machine reads, not only in the human text.
//
// The two existing --dry-run commands mark a preview by prefixing their TEXT
// renderer, which leaves a dry run byte-identical to an applied one in TOON and
// JSON — the default format and the one a script parses. An agent could not tell
// whether the files had actually moved. That bug is not copied here, and this is
// what stops it being copied later.
func TestSyncDryRunIsVisibleInMachineFormats(t *testing.T) {
	t.Run("toon", func(t *testing.T) {
		src, dst := syncPair(t)

		stdout, _, _ := runCLI(t, "sync-directories",
			"--source", src, "--dest", dst, "--dry-run")

		if !hasTOONField(stdout, "dry_run") {
			t.Errorf("a TOON preview is indistinguishable from a real run: %q", stdout)
		}
		if !hasTOONField(stdout, "planned") {
			t.Errorf("a preview must name what it would write: %q", stdout)
		}
		assertDestinationEmpty(t, dst)
	})

	t.Run("json", func(t *testing.T) {
		src, dst := syncPair(t)

		stdout, _, _ := runCLI(t, "--format", "json", "sync-directories",
			"--source", src, "--dest", dst, "--dry-run")

		var got struct {
			DryRun      bool     `json:"dry_run"`
			Planned     []string `json:"planned"`
			FilesCopied int      `json:"files_copied"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("a preview must be one JSON document: %v", err)
		}
		if !got.DryRun {
			t.Error("dry_run missing from a JSON preview")
		}
		if len(got.Planned) == 0 {
			t.Error("planned missing from a JSON preview")
		}
		if got.FilesCopied != 2 {
			t.Errorf("files_copied = %d, want 2", got.FilesCopied)
		}
		assertDestinationEmpty(t, dst)
	})

	// The other direction: a real run must not claim to be a preview, or the
	// flag is useless as a discriminator.
	t.Run("a real run carries no dry_run", func(t *testing.T) {
		src, dst := syncPair(t)

		stdout, _, _ := runCLI(t, "sync-directories", "--source", src, "--dest", dst)

		if hasTOONField(stdout, "dry_run") {
			t.Errorf("a real run reported dry_run: %q", stdout)
		}
		if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
			t.Errorf("the real run copied nothing: %v", err)
		}
	})
}

// The human format keeps its own marker. This is the idiom the two existing
// --dry-run commands use, and the only one they got right, so it must survive
// alongside the structured fields rather than be replaced by them.
func TestSyncDryRunMarksTheTextOutput(t *testing.T) {
	src, dst := syncPair(t)

	stdout, _, _ := runCLI(t, "--format", "text", "sync-directories",
		"--source", src, "--dest", dst, "--dry-run")

	if !strings.Contains(stdout, "[DRY RUN]") {
		t.Errorf("text output does not mark the preview: %q", stdout)
	}
	assertDestinationEmpty(t, dst)
}

// A sync the sandbox forbids must report a tool failure, not a cheerful zero.
// Reporting success for work that was refused is the defect this command
// already had once.
func TestSyncDirectoriesReportsASandboxRefusal(t *testing.T) {
	src, dst := syncPair(t)
	elsewhere := t.TempDir()

	stdout, _, code := runCLI(t, "--allowed-dirs", elsewhere,
		"sync-directories", "--source", src, "--dest", dst)

	if code == -1 || code == 0 {
		t.Errorf("a forbidden sync exited %d: %q", code, stdout)
	}
	if !strings.Contains(stdout, "error: true") {
		t.Errorf("a refusal must carry a structured error body: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err == nil {
		t.Error("a forbidden sync copied a file anyway")
	}
}

// hasTOONField reports whether the output carries a top-level field of this
// name, matching the start of a line rather than the substring anywhere.
//
// The loose form was a real CI failure. t.TempDir() embeds the TEST NAME in the
// path it creates, so a subtest called "a real run carries no dry_run" produced
// a destination path containing "dry_run" — and the output prints that path.
// The assertion matched its own temp directory and reported a field that was
// never there. It passed locally only because Go 1.26 names temp directories
// differently from the Go 1.24 used in CI.
func hasTOONField(out, field string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, field+":") || strings.HasPrefix(line, field+"[") {
			return true
		}
	}
	return false
}

// Reconstructs the CI failure deterministically.
//
// The original bug cannot reproduce on this machine: Go 1.26 names temp
// directories differently from the Go 1.24 used in CI, so the loose assertion
// passed locally and failed there. Feeding the helper the exact shape of output
// that broke it makes the fix provable on any platform, which is the property
// the original assertion lacked.
func TestHasTOONFieldIgnoresAFieldNameInsideAPath(t *testing.T) {
	out := "destination: /tmp/TestSyncDryRuna_real_run_carries_no_dry_run369885382/002\n" +
		"dirs_created: 2\n" +
		"files_copied: 2\n" +
		"success: true\n"

	if hasTOONField(out, "dry_run") {
		t.Error("matched a field name that only appears inside a path")
	}
	if !hasTOONField(out, "files_copied") {
		t.Error("missed a real field")
	}
	if !hasTOONField("planned[2]: a.txt,b.txt\n", "planned") {
		t.Error("missed an array field, which TOON writes as name[n]:")
	}
}

func assertDestinationEmpty(t *testing.T, dst string) {
	t.Helper()

	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a dry run wrote %d entries into the destination", len(entries))
	}
}
