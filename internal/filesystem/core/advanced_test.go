package core

import (
	"os"
	"path/filepath"
	"testing"
)

// AC11: sync-directories copies unconditionally, so the only way to find out
// what it would clobber was to let it.
//
// Both halves matter. The first proves the destination is untouched; the second
// proves the preview MATCHED the run that followed. A stub returning zero counts
// and writing nothing would satisfy the first half and fail the second, which is
// what stops "dry run" from being implemented as "do nothing and report
// nothing".
func TestSyncDirectoriesDryRunChangesNothing(t *testing.T) {
	src := mkTree(t)
	dst := t.TempDir()

	preview, err := SyncDirectories(SyncDirectoriesOptions{
		Source:      src,
		Destination: dst,
		DryRun:      true,
		AllowedDirs: []string{src, dst},
	})
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	if !preview.DryRun {
		t.Error("dry_run not set on a preview; in TOON and JSON that is the only thing distinguishing it from a real run")
	}
	if preview.FilesCopied == 0 {
		t.Fatal("the preview reported nothing to do for a tree with files in it")
	}

	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a dry run wrote %d entries into the destination", len(entries))
	}

	applied, err := SyncDirectories(SyncDirectoriesOptions{
		Source:      src,
		Destination: dst,
		AllowedDirs: []string{src, dst},
	})
	if err != nil {
		t.Fatalf("real sync failed: %v", err)
	}

	if applied.DryRun {
		t.Error("dry_run set on a real run")
	}
	if preview.FilesCopied != applied.FilesCopied {
		t.Errorf("preview said %d files, the real run copied %d", preview.FilesCopied, applied.FilesCopied)
	}
	if preview.DirsCreated != applied.DirsCreated {
		t.Errorf("preview said %d dirs, the real run created %d", preview.DirsCreated, applied.DirsCreated)
	}
	// planned is a bounded sample, not a full manifest — a sync of 50,000 files
	// must not answer with 50,000 strings. So it must be non-empty and never
	// claim more than files_copied, which stays the authoritative count.
	if len(preview.Planned) == 0 {
		t.Error("the preview named no paths at all, so it says nothing about what would be written")
	}
	if len(preview.Planned) > preview.FilesCopied {
		t.Errorf("planned lists %d paths but only %d files would be copied",
			len(preview.Planned), preview.FilesCopied)
	}

	// The real run must actually have written, or the comparison above would be
	// two matching descriptions of nothing.
	entries, err = os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("the real run wrote nothing")
	}
}

// sync-directories reported success while copying nothing.
//
// SyncDirectories called copyFile(dstPath, path), but the signature is
// copyFile(src, dst) — the three call sites in fileops.go all pass (src, dst).
// It opened a destination that did not exist, failed, and the walk discarded the
// error with a bare `return nil`. So files_copied stayed 0 and success stayed
// true. Directories WERE created, which left the destination looking plausibly
// populated. The defect predates this branch (fba7f40) and survived because no
// test covered SyncDirectories at all.
//
// This asserts file CONTENT on disk rather than the counters. A counter can be
// wrong in the same direction as the bug that produced it; bytes cannot.
func TestSyncDirectoriesActuallyCopiesFileContent(t *testing.T) {
	src := mkTree(t)
	dst := t.TempDir()

	res, err := SyncDirectories(SyncDirectoriesOptions{
		Source:      src,
		Destination: dst,
		AllowedDirs: []string{src, dst},
	})
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// mkTree writes b.txt ("bb"), a.go ("package x") and sub/deep.go ("x").
	for rel, want := range map[string]string{
		"b.txt":       "bb",
		"a.go":        "package x",
		"sub/deep.go": "x",
	} {
		got, readErr := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
		if readErr != nil {
			t.Errorf("%s did not reach the destination: %v", rel, readErr)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}

	if res.FilesCopied != 3 {
		t.Errorf("files_copied = %d, want 3", res.FilesCopied)
	}
	if !res.Success {
		t.Error("success = false after a sync that worked")
	}
}

// The refusal paths. A sync that cannot run must say so rather than report a
// cheerful zero — which is the shape of the bug fixed above, and the reason
// these are worth pinning rather than leaving to the happy path.
func TestSyncDirectoriesRefusesBadInput(t *testing.T) {
	dir := mkTree(t)

	cases := []struct {
		name string
		opts SyncDirectoriesOptions
	}{
		{"no source", SyncDirectoriesOptions{Destination: dir}},
		{"no destination", SyncDirectoriesOptions{Source: dir}},
		{
			"destination outside the sandbox",
			SyncDirectoriesOptions{
				Source:      dir,
				Destination: filepath.Join(t.TempDir(), "elsewhere"),
				AllowedDirs: []string{dir},
			},
		},
		{
			"source outside the sandbox",
			SyncDirectoriesOptions{
				Source:      t.TempDir(),
				Destination: dir,
				AllowedDirs: []string{dir},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := SyncDirectories(c.opts)

			if err == nil {
				t.Fatalf("expected a refusal, got result %+v", res)
			}
			if res != nil {
				t.Errorf("a refusal must not also return a result: %+v", res)
			}
		})
	}
}

// A real run must not carry preview metadata, or a consumer cannot use the
// presence of planned[] to tell the two apart.
func TestSyncDirectoriesRealRunCarriesNoPlan(t *testing.T) {
	src := mkTree(t)
	dst := t.TempDir()

	res, err := SyncDirectories(SyncDirectoriesOptions{
		Source:      src,
		Destination: dst,
		AllowedDirs: []string{src, dst},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Planned) != 0 {
		t.Errorf("a real run returned a plan of %d entries", len(res.Planned))
	}
}
