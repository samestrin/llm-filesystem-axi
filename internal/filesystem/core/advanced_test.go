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
