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

		if !strings.Contains(stdout, "dry_run: true") {
			t.Errorf("a TOON preview is indistinguishable from a real run: %q", stdout)
		}
		if !strings.Contains(stdout, "planned") {
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

		if strings.Contains(stdout, "dry_run") {
			t.Errorf("a real run reported dry_run: %q", stdout)
		}
		if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
			t.Errorf("the real run copied nothing: %v", err)
		}
	})
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
