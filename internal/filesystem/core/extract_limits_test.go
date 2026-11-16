package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extract-archive copied every entry with an unbounded io.Copy, so a small
// archive that inflates to terabytes (a decompression bomb) filled the disk,
// and one with millions of entries exhausted inodes, with nothing to stop it.
// Total bytes and entry count are now capped. An entry that would pass a cap
// fails, is counted, its partial file is removed, and the run stops there.

func TestExtractArchiveStopsAtTheByteCap(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			archive := f.build(t, []archiveEntry{
				{name: "small.txt", body: strings.Repeat("a", 10)},
				{name: "bomb.txt", body: strings.Repeat("b", 1000)},
				{name: "after.txt", body: "c"},
			})
			dest := t.TempDir()

			res, err := ExtractArchive(ExtractArchiveOptions{Archive: archive, Destination: dest, MaxBytes: 100})
			if err != nil {
				t.Fatalf("ExtractArchive: %v", err)
			}
			assertFailureNamed(t, res, "bomb.txt")
			if res.FilesExtracted != 1 {
				t.Errorf("files_extracted = %d, want 1", res.FilesExtracted)
			}
			if !res.Truncated {
				t.Error("a run stopped at a cap must say so: truncated is not set")
			}
			if _, err := os.Stat(filepath.Join(dest, "bomb.txt")); !os.IsNotExist(err) {
				t.Errorf("the over-cap entry's partial file was left behind: %v", err)
			}
			// Stopped, not skipped-past: a bomb's remaining entries are not read.
			if _, err := os.Stat(filepath.Join(dest, "after.txt")); !os.IsNotExist(err) {
				t.Errorf("an entry after the cap was still extracted: %v", err)
			}
			if got, _ := os.ReadFile(filepath.Join(dest, "small.txt")); len(got) != 10 {
				t.Errorf("small.txt = %q", got)
			}
		})
	}
}

func TestExtractArchiveStopsAtTheEntryCap(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			archive := f.build(t, []archiveEntry{
				{name: "one.txt", body: "1"},
				{name: "two.txt", body: "2"},
				{name: "three.txt", body: "3"},
			})
			dest := t.TempDir()

			res, err := ExtractArchive(ExtractArchiveOptions{Archive: archive, Destination: dest, MaxEntries: 2})
			if err != nil {
				t.Fatalf("ExtractArchive: %v", err)
			}
			assertFailureNamed(t, res, "three.txt")
			if res.FilesExtracted != 2 || !res.Truncated {
				t.Errorf("files_extracted = %d truncated = %v, want 2 and true", res.FilesExtracted, res.Truncated)
			}
			if _, err := os.Stat(filepath.Join(dest, "three.txt")); !os.IsNotExist(err) {
				t.Errorf("the entry past the cap was written: %v", err)
			}
		})
	}
}

// An archive inside both caps is untouched by them, including one that lands
// exactly on each: the caps are inclusive.
func TestExtractArchiveWithinTheCapsIsUnaffected(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			archive := f.build(t, []archiveEntry{
				{name: "a.txt", body: strings.Repeat("a", 50)},
				{name: "b.txt", body: strings.Repeat("b", 50)},
			})
			res, err := ExtractArchive(ExtractArchiveOptions{Archive: archive, Destination: t.TempDir(), MaxBytes: 100, MaxEntries: 2})
			if err != nil {
				t.Fatalf("ExtractArchive: %v", err)
			}
			if !res.Success || res.Truncated || res.FilesExtracted != 2 {
				t.Errorf("an archive exactly at both caps was refused: %+v", res)
			}
		})
	}
}

// The defaults apply when no cap is given; -1 lifts a cap.
func TestExtractArchiveDefaultsAndNoLimit(t *testing.T) {
	if DefaultExtractMaxBytes <= 0 || DefaultExtractMaxEntries <= 0 {
		t.Fatalf("defaults must be positive: %d bytes, %d entries", DefaultExtractMaxBytes, DefaultExtractMaxEntries)
	}
	archive := buildZip(t, []archiveEntry{{name: "a.txt", body: strings.Repeat("a", 500)}})
	res, err := ExtractArchive(ExtractArchiveOptions{Archive: archive, Destination: t.TempDir(), MaxBytes: -1, MaxEntries: -1})
	if err != nil || !res.Success {
		t.Fatalf("-1 should lift both caps: %v %+v", err, res)
	}
}
