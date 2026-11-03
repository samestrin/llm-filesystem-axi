package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// A copy that fails part-way must not leave the destination worse than it found
// it, and must not be reported as a success.
//
// copyFile calls os.Create — which TRUNCATES — before io.Copy. filepath.Walk
// uses Lstat, so a symlink pointing at a directory is classed as a file: the
// open succeeds, the destination is truncated to zero, io.Copy then fails EISDIR,
// and the walk discards the error with a bare `return nil`. Existing destination
// data is destroyed, files_copied does not count it, and success stays true.
func TestSyncDirectoriesDoesNotDestroyOnACopyFailure(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	realDir := filepath.Join(src, "realdir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(src, "trap")); err != nil {
		t.Skip("symlinks unavailable on this filesystem")
	}

	precious := filepath.Join(dst, "trap")
	if err := os.WriteFile(precious, []byte("IRREPLACEABLE DESTINATION DATA"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := SyncDirectories(SyncDirectoriesOptions{
		Source:      src,
		Destination: dst,
		AllowedDirs: []string{src, dst},
	})

	got, rerr := os.ReadFile(precious)
	if rerr == nil && len(got) == 0 {
		t.Error("an existing destination file was truncated by a copy that then failed")
	}
	if rerr == nil && string(got) != "IRREPLACEABLE DESTINATION DATA" {
		t.Errorf("destination content changed to %q despite the copy failing", got)
	}

	// Whatever the outcome, a failure must be visible to the caller.
	if err == nil && res != nil && res.Success && res.Failed == 0 {
		t.Error("a sync containing a failed copy reported success with zero failures")
	}
}

// A sync that cannot create its destination must not report success. MkdirAll
// fails, the walk continues (a directory visitor must return SkipDir to skip —
// nil descends), every copy then fails into a directory that does not exist, and
// none of it is reported.
func TestSyncDirectoriesReportsAnUncreatableDestination(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission bits would not be enforced")
	}

	src := mkTree(t)
	parent := t.TempDir()
	dst := filepath.Join(parent, "sub")

	if err := os.Chmod(parent, 0o555); err != nil {
		t.Skip("cannot make the parent read-only")
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	res, err := SyncDirectories(SyncDirectoriesOptions{
		Source:      src,
		Destination: dst,
		AllowedDirs: []string{src, parent},
	})

	if err == nil && res != nil && res.Success {
		t.Errorf("a sync that created nothing reported success: %+v", res)
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

// archiveEntry describes one entry of a fixture archive built in t.TempDir().
// The fixtures are built entry by entry rather than with a real archiver
// because the cases that matter here — a `../` name, a bad checksum, a stream
// cut short — are exactly the ones a well-behaved archiver refuses to write.
type archiveEntry struct {
	name   string
	body   string
	dir    bool
	tarTyp byte   // tar only; 0 means a regular file, or a directory when dir is set
	method uint16 // zip only; an unregistered method makes Open fail
	badCRC bool   // zip only; a wrong checksum makes the copy fail
}

func buildZip(t *testing.T, entries []archiveEntry) string {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Registered on the writer only, so the reader does not know it: the
	// entry is written fine and fails at Open, which is the case under test.
	zw.RegisterCompressor(99, func(w io.Writer) (io.WriteCloser, error) {
		return nopWriteCloser{w}, nil
	})
	for _, e := range entries {
		switch {
		case e.dir:
			if _, err := zw.Create(e.name); err != nil {
				t.Fatal(err)
			}
		case e.badCRC:
			w, err := zw.CreateRaw(&zip.FileHeader{
				Name:               e.name,
				Method:             zip.Store,
				CRC32:              0xdeadbeef,
				CompressedSize64:   uint64(len(e.body)),
				UncompressedSize64: uint64(len(e.body)),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(w, e.body); err != nil {
				t.Fatal(err)
			}
		default:
			w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: e.method})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(w, e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "fixture.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// buildTarGz writes the entries as a gzipped tar. truncateAt > 0 cuts the
// uncompressed tar stream to that many bytes before compressing it, which is
// how a copy failure is produced: the header promises more data than follows.
func buildTarGz(t *testing.T, entries []archiveEntry, truncateAt int) string {
	t.Helper()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(len(e.body))}
		switch {
		case e.dir:
			hdr.Typeflag, hdr.Mode, hdr.Size = tar.TypeDir, 0o755, 0
		case e.tarTyp == tar.TypeXGlobalHeader:
			// What `git archive` writes: the commit id as a pax comment. The
			// writer accepts nothing but PAXRecords on a global header.
			hdr = &tar.Header{Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "0123456789abcdef"}}
		case e.tarTyp == tar.TypeSymlink:
			hdr.Typeflag, hdr.Linkname, hdr.Size = tar.TypeSymlink, "/etc/passwd", 0
		case e.tarTyp != 0:
			hdr.Typeflag, hdr.Size = e.tarTyp, 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Size > 0 {
			if _, err := io.WriteString(tw, e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	raw := tarBuf.Bytes()
	if truncateAt > 0 {
		raw = raw[:truncateAt]
	}

	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "fixture.tar.gz")
	if err := os.WriteFile(path, gzBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// archiveFormats runs a case against both extractors, so neither guard can be
// fixed while the other is forgotten.
var archiveFormats = []struct {
	name  string
	build func(t *testing.T, entries []archiveEntry) string
}{
	{"zip", buildZip},
	{"tar.gz", func(t *testing.T, e []archiveEntry) string { return buildTarGz(t, e, 0) }},
}

func extract(t *testing.T, archive, dest string) *ExtractResult {
	t.Helper()

	res, err := ExtractArchive(ExtractArchiveOptions{Archive: archive, Destination: dest})
	if err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}
	return res
}

// assertFailureNamed checks that the run is a reported partial failure and that
// failures[] names the entry. Success is checked against Failed rather than
// assumed, because a refused entry that left success: true is the bug.
func assertFailureNamed(t *testing.T, res *ExtractResult, entry string) {
	t.Helper()

	if res.Success {
		t.Errorf("a run that did not write %q reported success: %+v", entry, res)
	}
	if res.Failed == 0 {
		t.Errorf("failed = 0 for a run that did not write %q", entry)
	}
	for _, f := range res.Failures {
		if strings.HasPrefix(f, entry+": ") {
			return
		}
	}
	t.Errorf("failures %q do not name %q with a reason", res.Failures, entry)
}

// extract-archive refused a `../` entry, which is correct, and then reported
// files_extracted: 1, success: true with no word about it: both guards used a
// bare `continue`. The caller had no way to learn an entry was dropped.
func TestExtractArchiveReportsAnEscapingEntry(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "out")
			archive := f.build(t, []archiveEntry{
				{name: "ok.txt", body: "ok"},
				{name: "../escape.txt", body: "escaped"},
			})

			res := extract(t, archive, dest)

			if res.FilesExtracted != 1 || res.Failed != 1 {
				t.Errorf("files_extracted = %d, failed = %d, want 1 and 1", res.FilesExtracted, res.Failed)
			}
			assertFailureNamed(t, res, "../escape.txt")
			if _, err := os.Stat(filepath.Join(root, "escape.txt")); err == nil {
				t.Error("the escaping entry was written outside the destination")
			}
			if got, _ := os.ReadFile(filepath.Join(dest, "ok.txt")); string(got) != "ok" {
				t.Errorf("ok.txt = %q, want the good entry extracted", got)
			}
		})
	}
}

// `tar czf x.tgz -C dir .` writes a `./` directory entry. It resolves to the
// destination itself, which is not an escape, and must not turn an ordinary
// archive into a reported failure. The guard compared against dest+"/", which
// the destination itself does not match.
func TestExtractArchiveTreatsTheDestinationEntryAsANoOp(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "out")
			archive := f.build(t, []archiveEntry{
				{name: "./", dir: true},
				{name: "./a.txt", body: "a"},
			})

			res := extract(t, archive, dest)

			if !res.Success || res.Failed != 0 || res.FilesExtracted != 1 {
				t.Errorf("want success, failed 0, 1 extracted; got %+v", res)
			}
		})
	}

	// The same archive from the real tool, where one is installed, so the
	// fixture above cannot drift from what tar actually writes.
	t.Run("tar -C dir .", func(t *testing.T) {
		tarBin, err := exec.LookPath("tar")
		if err != nil {
			t.Skip("tar not installed")
		}
		src := t.TempDir()
		if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
			t.Fatal(err)
		}
		archive := filepath.Join(t.TempDir(), "x.tgz")
		if out, err := exec.Command(tarBin, "czf", archive, "-C", src, ".").CombinedOutput(); err != nil {
			t.Fatalf("tar: %v: %s", err, out)
		}

		res := extract(t, archive, filepath.Join(t.TempDir(), "out"))

		if !res.Success || res.Failed != 0 || res.FilesExtracted != 1 {
			t.Errorf("want success, failed 0, 1 extracted; got %+v", res)
		}
	})
}

// os.Create truncated an existing file without a word. The epic reports
// overwrites rather than gating them (owner decision Q1), so the count has to
// be there — and a new file must not inflate it.
func TestExtractArchiveCountsOverwrites(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			dest := t.TempDir()
			if err := os.WriteFile(filepath.Join(dest, "ok.txt"), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			archive := f.build(t, []archiveEntry{
				{name: "ok.txt", body: "new"},
				{name: "fresh.txt", body: "fresh"},
			})

			res := extract(t, archive, dest)

			if res.Overwritten != 1 {
				t.Errorf("overwritten = %d, want 1 (ok.txt existed, fresh.txt did not)", res.Overwritten)
			}
			if !res.Success || res.FilesExtracted != 2 {
				t.Errorf("an overwrite is not a failure; got %+v", res)
			}
			if got, _ := os.ReadFile(filepath.Join(dest, "ok.txt")); string(got) != "new" {
				t.Errorf("ok.txt = %q, want the archive's content", got)
			}
		})
	}
}

// Every tar entry that was not a directory or a regular file — a symlink, a
// hard link, a device — fell out of the type switch and vanished.
func TestExtractArchiveReportsAnUnsupportedTarEntry(t *testing.T) {
	dest := t.TempDir()
	archive := buildTarGz(t, []archiveEntry{
		{name: "ok.txt", body: "ok"},
		{name: "link", tarTyp: tar.TypeSymlink},
		{name: "fifo", tarTyp: tar.TypeFifo},
	}, 0)

	res := extract(t, archive, dest)

	if res.FilesExtracted != 1 || res.Failed != 2 {
		t.Errorf("files_extracted = %d, failed = %d, want 1 and 2", res.FilesExtracted, res.Failed)
	}
	assertFailureNamed(t, res, "link")
	assertFailureNamed(t, res, "fifo")
	if _, err := os.Lstat(filepath.Join(dest, "link")); err == nil {
		t.Error("an unsupported entry was written anyway")
	}
}

// A directory that cannot be made — here because a FILE is in its place — was
// ignored for a directory entry and silently skipped for a file entry.
func TestExtractArchiveReportsAMkdirFailure(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			dest := t.TempDir()
			if err := os.WriteFile(filepath.Join(dest, "blocker"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			archive := f.build(t, []archiveEntry{
				{name: "blocker/sub/", dir: true},
				{name: "blocker/x.txt", body: "x"},
			})

			res := extract(t, archive, dest)

			if res.Failed != 2 {
				t.Errorf("failed = %d, want 2", res.Failed)
			}
			assertFailureNamed(t, res, "blocker/sub/")
			assertFailureNamed(t, res, "blocker/x.txt")
		})
	}
}

// os.Create on a path that is a directory fails, and the entry was skipped
// without a word.
func TestExtractArchiveReportsACreateFailure(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			dest := t.TempDir()
			if err := os.Mkdir(filepath.Join(dest, "d.txt"), 0o755); err != nil {
				t.Fatal(err)
			}
			archive := f.build(t, []archiveEntry{{name: "d.txt", body: "x"}})

			res := extract(t, archive, dest)

			if res.FilesExtracted != 0 {
				t.Errorf("files_extracted = %d for an entry that could not be created", res.FilesExtracted)
			}
			assertFailureNamed(t, res, "d.txt")
			if res.Overwritten != 0 {
				t.Errorf("overwritten = %d; nothing was replaced", res.Overwritten)
			}
		})
	}
}

// A zip entry that cannot be opened — here an unknown compression method — was
// skipped without a word.
func TestExtractArchiveReportsAZipOpenFailure(t *testing.T) {
	dest := t.TempDir()
	archive := buildZip(t, []archiveEntry{
		{name: "ok.txt", body: "ok"},
		{name: "odd.bin", body: "x", method: 99},
	})

	res := extract(t, archive, dest)

	if res.FilesExtracted != 1 {
		t.Errorf("files_extracted = %d, want 1", res.FilesExtracted)
	}
	assertFailureNamed(t, res, "odd.bin")
}

// io.Copy's error was discarded, so a corrupt entry was COUNTED as extracted:
// a bad zip checksum, or a tar stream that ends before the entry does.
func TestExtractArchiveReportsACopyFailure(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		archive := buildZip(t, []archiveEntry{{name: "bad.txt", body: "corrupt", badCRC: true}})

		res := extract(t, archive, t.TempDir())

		if res.FilesExtracted != 0 {
			t.Errorf("files_extracted = %d for an entry that failed its checksum", res.FilesExtracted)
		}
		assertFailureNamed(t, res, "bad.txt")
	})

	t.Run("tar.gz", func(t *testing.T) {
		// 512 header bytes plus 10 of the 2000 the header promises.
		archive := buildTarGz(t, []archiveEntry{{name: "cut.txt", body: strings.Repeat("x", 2000)}}, 522)

		res := extract(t, archive, t.TempDir())

		if res.FilesExtracted != 0 {
			t.Errorf("files_extracted = %d for an entry cut short", res.FilesExtracted)
		}
		assertFailureNamed(t, res, "cut.txt")
	})
}

type failingClose struct{ io.Writer }

func (failingClose) Close() error { return errors.New("injected close failure") }

// outFile.Close() was discarded, and on a full or remote filesystem Close is
// where a write error surfaces. Nothing on a test filesystem fails Close, so
// the file opener is swapped for one whose Close does.
func TestExtractArchiveReportsACloseFailure(t *testing.T) {
	prev := createExtractedFile
	createExtractedFile = func(path string) (io.WriteCloser, error) {
		f, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		_ = f.Close()
		return failingClose{io.Discard}, nil
	}
	t.Cleanup(func() { createExtractedFile = prev })

	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			res := extract(t, f.build(t, []archiveEntry{{name: "a.txt", body: "a"}}), t.TempDir())

			if res.FilesExtracted != 0 {
				t.Errorf("files_extracted = %d for an entry whose close failed", res.FilesExtracted)
			}
			assertFailureNamed(t, res, "a.txt")
			if !strings.Contains(strings.Join(res.Failures, "\n"), "injected close failure") {
				t.Errorf("failures %q do not carry the reason", res.Failures)
			}
		})
	}
}

// failures[] echoes archive-supplied names, so a hostile archive with 50,000
// bad entries must not answer with 50,000 strings. failed stays the true count;
// a bound on it would make a partial run look smaller than it was.
func TestExtractArchiveBoundsFailures(t *testing.T) {
	var entries []archiveEntry
	for i := 0; i < maxPlannedPaths+50; i++ {
		entries = append(entries, archiveEntry{name: fmt.Sprintf("../e%d.txt", i), body: "x"})
	}

	res := extract(t, buildZip(t, entries), t.TempDir())

	if res.Failed != maxPlannedPaths+50 {
		t.Errorf("failed = %d, want the true count %d", res.Failed, maxPlannedPaths+50)
	}
	if len(res.Failures) != maxPlannedPaths {
		t.Errorf("len(failures) = %d, want it bounded at %d", len(res.Failures), maxPlannedPaths)
	}
}

// The `./` no-op is for a DIRECTORY entry. A file entry that resolves to the
// destination cannot be written over a directory, and that must be a reported
// failure rather than a second no-op.
func TestExtractArchiveReportsAFileEntryNamingTheDestination(t *testing.T) {
	for _, f := range archiveFormats {
		t.Run(f.name, func(t *testing.T) {
			res := extract(t, f.build(t, []archiveEntry{{name: ".", body: "x"}}), t.TempDir())

			if res.FilesExtracted != 0 {
				t.Errorf("files_extracted = %d", res.FilesExtracted)
			}
			assertFailureNamed(t, res, ".")
		})
	}
}

// Input the command cannot act on is an error, never a result: a result with
// files_extracted: 0 and success: true would say an empty archive was extracted.
func TestExtractArchiveRefusesBadInput(t *testing.T) {
	dir := t.TempDir()
	notGzip := filepath.Join(dir, "plain.tgz")
	if err := os.WriteFile(notGzip, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := buildZip(t, []archiveEntry{{name: "a.txt", body: "a"}})

	cases := []struct {
		name string
		opts ExtractArchiveOptions
	}{
		{"no archive", ExtractArchiveOptions{Destination: dir}},
		{"no destination", ExtractArchiveOptions{Archive: good}},
		{"unknown extension", ExtractArchiveOptions{Archive: filepath.Join(dir, "x.rar"), Destination: dir}},
		{"missing zip", ExtractArchiveOptions{Archive: filepath.Join(dir, "missing.zip"), Destination: dir}},
		{"missing tar.gz", ExtractArchiveOptions{Archive: filepath.Join(dir, "missing.tar.gz"), Destination: dir}},
		{"not gzip", ExtractArchiveOptions{Archive: notGzip, Destination: dir}},
		{"archive outside the sandbox", ExtractArchiveOptions{Archive: good, Destination: dir, AllowedDirs: []string{dir}}},
		{"destination outside the sandbox", ExtractArchiveOptions{Archive: good, Destination: dir, AllowedDirs: []string{filepath.Dir(good)}}},
		{"destination is a file", ExtractArchiveOptions{Archive: good, Destination: notGzip}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := ExtractArchive(c.opts)
			if err == nil {
				t.Errorf("want an error, got %+v", res)
			}
		})
	}
}

// `git archive` writes a pax_global_header entry into every tarball. It is
// metadata for the archive, not an entry, and reporting it as an unsupported
// type turned every `git archive` tarball into failed: 1, exit 1 with nothing
// wrong.
func TestExtractArchiveSkipsAPaxGlobalHeader(t *testing.T) {
	archive := buildTarGz(t, []archiveEntry{
		{name: "pax_global_header", tarTyp: tar.TypeXGlobalHeader},
		{name: "a.txt", body: "a"},
	}, 0)

	res := extract(t, archive, t.TempDir())

	if !res.Success || res.Failed != 0 || res.FilesExtracted != 1 {
		t.Errorf("want success, failed 0, 1 extracted; got %+v", res)
	}
}

// compressFormats are the archive types compress-files writes. Each compress
// test runs against both, because the zip and tar.gz paths were separate copies
// of the same silent loop.
var compressFormats = []string{"zip", "tar.gz"}

func compress(t *testing.T, format, output string, paths ...string) *CompressResult {
	t.Helper()
	res, err := CompressFiles(CompressFilesOptions{Paths: paths, Output: output, Format: format})
	if err != nil {
		t.Fatalf("CompressFiles: %v", err)
	}
	return res
}

// compress-files --paths ok.txt,missing.txt printed files_added: 1 and
// success: true. The missing input was skipped at os.Stat and nothing said so.
func TestCompressFilesReportsAMissingInput(t *testing.T) {
	for _, format := range compressFormats {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			ok := filepath.Join(dir, "ok.txt")
			if err := os.WriteFile(ok, []byte("ok"), 0o644); err != nil {
				t.Fatal(err)
			}
			missing := filepath.Join(dir, "missing.txt")

			res := compress(t, format, filepath.Join(t.TempDir(), "out."+format), ok, missing)

			if res.FilesAdded != 1 || res.Failed != 1 || res.Success {
				t.Errorf("want files_added 1, failed 1, success false; got %+v", res)
			}
			if len(res.Failures) != 1 || !strings.HasPrefix(res.Failures[0], missing+": ") {
				t.Errorf("failures = %q, want the missing path and a reason", res.Failures)
			}
		})
	}
}

// Inside a directory input, an add error (here a dangling symlink, which Walk
// hands over as a file that cannot be opened) and a Walk error (a subdirectory
// that cannot be read) were each answered `return nil` and dropped.
func TestCompressFilesReportsWalkAndAddErrors(t *testing.T) {
	for _, format := range compressFormats {
		t.Run(format, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "src")
			locked := filepath.Join(src, "locked")
			if err := os.MkdirAll(locked, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(locked, "hidden.txt"), []byte("h"), 0o644); err != nil {
				t.Fatal(err)
			}
			dangling := filepath.Join(src, "dangling")
			if err := os.Symlink(filepath.Join(src, "nowhere"), dangling); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(locked, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
			if _, err := os.ReadDir(locked); err == nil {
				t.Skip("running as a user that can read a mode-000 directory")
			}

			res := compress(t, format, filepath.Join(t.TempDir(), "out."+format), src)

			if res.FilesAdded != 1 || res.Failed != 2 || res.Success {
				t.Errorf("want files_added 1, failed 2, success false; got %+v", res)
			}
			joined := strings.Join(res.Failures, "\n")
			for _, want := range []string{dangling + ": ", locked + ": "} {
				if !strings.Contains(joined, want) {
					t.Errorf("failures %q do not name %s", res.Failures, want)
				}
			}
		})
	}
}

// os.Create truncated an existing --output without a word. It is still
// replaced (owner decision Q5: report, not refuse), but the result says so.
func TestCompressFilesReportsAnOverwrittenOutput(t *testing.T) {
	for _, format := range compressFormats {
		t.Run(format, func(t *testing.T) {
			in := filepath.Join(t.TempDir(), "a.txt")
			if err := os.WriteFile(in, []byte("a"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out."+format)

			if res := compress(t, format, out, in); res.Overwritten {
				t.Errorf("overwritten = true for an output that did not exist")
			}
			res := compress(t, format, out, in)
			if !res.Overwritten || !res.Success || res.Failed != 0 {
				t.Errorf("want overwritten true, success true, failed 0; got %+v", res)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("injected write failure") }
func (failingWriter) Close() error              { return nil }

// The zip, gzip and tar writers were closed only by defer, so the error that
// finishes the archive (the zip central directory, the gzip trailer, the final
// flush of the file) was never seen, and a truncated archive read as a
// success. The output is created for real, then swapped for a sink that fails,
// so the test can also prove the unfinished file is removed.
func TestCompressFilesFailsWhenTheArchiveCannotBeFinished(t *testing.T) {
	sinks := map[string]func() io.WriteCloser{
		"close fails": func() io.WriteCloser { return failingClose{io.Discard} },
		"write fails": func() io.WriteCloser { return failingWriter{} },
	}
	for sinkName, sink := range sinks {
		for _, format := range compressFormats {
			t.Run(sinkName+"/"+format, func(t *testing.T) {
				prev := createArchiveFile
				createArchiveFile = func(path string) (io.WriteCloser, error) {
					f, err := os.Create(path)
					if err != nil {
						return nil, err
					}
					_ = f.Close()
					return sink(), nil
				}
				t.Cleanup(func() { createArchiveFile = prev })

				in := filepath.Join(t.TempDir(), "a.txt")
				if err := os.WriteFile(in, []byte("a"), 0o644); err != nil {
					t.Fatal(err)
				}
				out := filepath.Join(t.TempDir(), "out."+format)

				res, err := CompressFiles(CompressFilesOptions{Paths: []string{in}, Output: out, Format: format})

				if err == nil {
					t.Fatalf("an archive that could not be finished returned no error: %+v", res)
				}
				if !strings.Contains(err.Error(), "injected") {
					t.Errorf("error %q does not carry the cause", err)
				}
				if _, statErr := os.Lstat(out); !os.IsNotExist(statErr) {
					t.Errorf("the unfinished archive was left at %s (stat: %v)", out, statErr)
				}
			})
		}
	}
}

// failures[] is bounded like extract's, and failed stays the true count.
func TestCompressFilesBoundsFailures(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for i := 0; i < maxPlannedPaths+5; i++ {
		paths = append(paths, filepath.Join(dir, fmt.Sprintf("missing%d.txt", i)))
	}

	res := compress(t, "zip", filepath.Join(t.TempDir(), "out.zip"), paths...)

	if res.Failed != maxPlannedPaths+5 || len(res.Failures) != maxPlannedPaths || res.Success {
		t.Errorf("want failed %d, %d failures, success false; got failed %d, %d failures, success %v",
			maxPlannedPaths+5, maxPlannedPaths, res.Failed, len(res.Failures), res.Success)
	}
}

// The other side: a clean run of a file and a directory still says success,
// and the archive it wrote holds both, so failed: 0 is not bought by adding
// nothing.
func TestCompressFilesCleanRunRoundTrips(t *testing.T) {
	for _, format := range compressFormats {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "d", "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{"f.txt": "f", "d/sub/g.txt": "g"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			out := filepath.Join(t.TempDir(), "out."+format)

			res := compress(t, format, out, filepath.Join(root, "f.txt"), filepath.Join(root, "d"))

			if res.FilesAdded != 2 || res.Failed != 0 || !res.Success || res.Failures != nil {
				t.Errorf("want files_added 2, failed 0, success true, no failures; got %+v", res)
			}
			if res.Size == 0 {
				t.Errorf("size = 0 for a written archive")
			}
			dest := t.TempDir()
			if x := extract(t, out, dest); x.FilesExtracted != 2 || x.Failed != 0 {
				t.Fatalf("extracting the archive: %+v", x)
			}
			for name, body := range map[string]string{"f.txt": "f", "d/sub/g.txt": "g"} {
				if got, _ := os.ReadFile(filepath.Join(dest, name)); string(got) != body {
					t.Errorf("%s = %q, want %q", name, got, body)
				}
			}
		})
	}
}

// Each refusal is an error with no result, and none of them leaves a file at
// --output: an unsupported format used to be checked only after the paths, and
// a refusal that still created the archive would be success by side effect.
func TestCompressFilesRefusesBadInput(t *testing.T) {
	in := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(in, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	out := filepath.Join(outDir, "out.zip")
	other := t.TempDir()

	cases := map[string]CompressFilesOptions{
		"no paths":           {Output: out},
		"no output":          {Paths: []string{in}},
		"unsupported format": {Paths: []string{in}, Output: out, Format: "rar"},
		"output not allowed": {Paths: []string{in}, Output: out, AllowedDirs: []string{other}},
		"input not allowed":  {Paths: []string{in}, Output: filepath.Join(other, "o.zip"), AllowedDirs: []string{other}},
		"output uncreatable": {Paths: []string{in}, Output: filepath.Join(outDir, "no", "such", "dir", "o.zip")},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := CompressFiles(opts)
			if err == nil {
				t.Fatalf("no error; result %+v", res)
			}
			if res != nil {
				t.Errorf("a refusal returned a result: %+v", res)
			}
			for _, p := range []string{out, filepath.Join(other, "o.zip")} {
				if _, statErr := os.Lstat(p); !os.IsNotExist(statErr) {
					t.Errorf("a refusal left %s behind", p)
				}
			}
		})
	}
}
