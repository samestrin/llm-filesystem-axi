package commands

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
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

// writeArchive builds a zip or tar.gz in t.TempDir() from name -> body pairs,
// in order. A name ending "/" is a directory entry. The core package has the
// full fixture set; this is the minimum the command layer needs.
func writeArchive(t *testing.T, format string, entries [][2]string) string {
	t.Helper()

	var buf bytes.Buffer
	switch format {
	case "zip":
		zw := zip.NewWriter(&buf)
		for _, e := range entries {
			w, err := zw.Create(e[0])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(w, e[1]); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	case "tar.gz":
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		for _, e := range entries {
			hdr := &tar.Header{Name: e[0], Mode: 0o644, Size: int64(len(e[1])), Typeflag: tar.TypeReg}
			if strings.HasSuffix(e[0], "/") {
				hdr.Typeflag, hdr.Mode = tar.TypeDir, 0o755
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(tw, e[1]); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gw.Close(); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(t.TempDir(), "fixture."+format)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasTOONLine(out, line string) bool {
	for _, l := range strings.Split(out, "\n") {
		if l == line {
			return true
		}
	}
	return false
}

// extract-archive printed files_extracted: 1, success: true and exited 0 for an
// archive whose `../` entry it had refused. A partial run now says so in every
// format, and exits 1 (owner decision Q2) — after printing the result, so the
// caller still learns which entry was dropped and why.
func TestExtractArchiveReportsAPartialRun(t *testing.T) {
	for _, archiveFmt := range []string{"zip", "tar.gz"} {
		t.Run(archiveFmt, func(t *testing.T) {
			archive := writeArchive(t, archiveFmt, [][2]string{{"ok.txt", "ok"}, {"../escape.txt", "x"}})

			t.Run("toon", func(t *testing.T) {
				out, _, code := runCLI(t, "extract-archive", "--archive", archive, "--dest", t.TempDir())

				if code != 1 {
					t.Errorf("a partial run exited %d, want 1", code)
				}
				for _, want := range []string{"files_extracted: 1", "failed: 1", "success: false"} {
					if !hasTOONLine(out, want) {
						t.Errorf("TOON output lacks %q:\n%s", want, out)
					}
				}
				if !hasTOONField(out, "failures") {
					t.Errorf("TOON output lacks failures[]:\n%s", out)
				}
				// The help[] block, not only the payload, names the entry.
				i := strings.Index(out, "\nhelp[")
				if i < 0 || !strings.Contains(out[i:], "../escape.txt") {
					t.Errorf("the help[] block does not name the failed entry:\n%s", out)
				}
			})

			t.Run("json", func(t *testing.T) {
				out, _, code := runCLI(t, "--format", "json", "extract-archive", "--archive", archive, "--dest", t.TempDir())

				if code != 1 {
					t.Errorf("a partial run exited %d, want 1", code)
				}
				var doc struct {
					FilesExtracted int      `json:"files_extracted"`
					Failed         int      `json:"failed"`
					Failures       []string `json:"failures"`
					Success        *bool    `json:"success"`
					NextSteps      []string `json:"next_steps"`
				}
				if err := json.Unmarshal([]byte(out), &doc); err != nil {
					t.Fatalf("not JSON: %v\n%s", err, out)
				}
				if doc.FilesExtracted != 1 || doc.Failed != 1 || doc.Success == nil || *doc.Success {
					t.Errorf("want files_extracted 1, failed 1, success false; got %s", out)
				}
				if len(doc.Failures) != 1 || !strings.HasPrefix(doc.Failures[0], "../escape.txt: ") {
					t.Errorf("failures = %q, want the entry name and a reason", doc.Failures)
				}
				if !strings.Contains(strings.Join(doc.NextSteps, "\n"), "../escape.txt") {
					t.Errorf("next_steps = %q, want the failed entry named", doc.NextSteps)
				}
			})

			t.Run("text", func(t *testing.T) {
				out, _, code := runCLI(t, "--format", "text", "extract-archive", "--archive", archive, "--dest", t.TempDir())

				if code != 1 {
					t.Errorf("a partial run exited %d, want 1", code)
				}
				if !strings.Contains(out, "1 failed") || !strings.Contains(out, "../escape.txt") {
					t.Errorf("text output hides the failure:\n%s", out)
				}
			})
		})
	}
}

// The other side of the partial-run exit: a clean run — including the `./`
// entry `tar czf x.tgz -C dir .` writes — must still exit 0 with failed: 0, or
// exit 1 stops meaning anything. An overwrite is reported, not failed.
func TestExtractArchiveCleanRunExitsZero(t *testing.T) {
	for _, archiveFmt := range []string{"zip", "tar.gz"} {
		t.Run(archiveFmt, func(t *testing.T) {
			archive := writeArchive(t, archiveFmt, [][2]string{{"./", ""}, {"./a.txt", "a"}})
			dest := t.TempDir()
			if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}

			out, _, code := runCLI(t, "extract-archive", "--archive", archive, "--dest", dest)

			if code != -1 && code != 0 {
				t.Errorf("a clean run exited %d:\n%s", code, out)
			}
			for _, want := range []string{"failed: 0", "success: true", "overwritten: 1"} {
				if !hasTOONLine(out, want) {
					t.Errorf("TOON output lacks %q:\n%s", want, out)
				}
			}
			if hasTOONField(out, "failures") {
				t.Errorf("a clean run carries failures[]:\n%s", out)
			}
			if got, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(got) != "a" {
				t.Errorf("a.txt = %q, want the archive's content", got)
			}
		})
	}
}

// compress-files --paths ok.txt,missing.txt printed files_added: 1,
// success: true and exited 0. A partial run now says so in every format and
// exits 1 (owner decision Q2), after printing the result.
func TestCompressFilesReportsAPartialRun(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(ok, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")
	paths := ok + "," + missing

	t.Run("toon", func(t *testing.T) {
		out, _, code := runCLI(t, "compress-files", "--paths", paths, "--output", filepath.Join(t.TempDir(), "o.zip"))

		if code != 1 {
			t.Errorf("a partial run exited %d, want 1", code)
		}
		for _, want := range []string{"files_added: 1", "failed: 1", "success: false", "overwritten: false"} {
			if !hasTOONLine(out, want) {
				t.Errorf("TOON output lacks %q:\n%s", want, out)
			}
		}
		i := strings.Index(out, "\nhelp[")
		if i < 0 || !strings.Contains(out[i:], "missing.txt") {
			t.Errorf("the help[] block does not name the missing input:\n%s", out)
		}
	})

	// compress-files owns a local --format (T3 renames it), so JSON is reached
	// through --json here.
	t.Run("json", func(t *testing.T) {
		out, _, code := runCLI(t, "--json", "compress-files", "--paths", paths, "--output", filepath.Join(t.TempDir(), "o.zip"))

		if code != 1 {
			t.Errorf("a partial run exited %d, want 1", code)
		}
		var doc struct {
			FilesAdded int      `json:"files_added"`
			Failed     int      `json:"failed"`
			Failures   []string `json:"failures"`
			Success    *bool    `json:"success"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, out)
		}
		if doc.FilesAdded != 1 || doc.Failed != 1 || doc.Success == nil || *doc.Success {
			t.Errorf("want files_added 1, failed 1, success false; got %s", out)
		}
		if len(doc.Failures) != 1 || !strings.HasPrefix(doc.Failures[0], missing+": ") {
			t.Errorf("failures = %q, want the missing path and a reason", doc.Failures)
		}
	})
}

// An existing --output is replaced and reported, and a clean run exits 0.
func TestCompressFilesReportsAnOverwriteAndExitsZero(t *testing.T) {
	in := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(in, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "o.zip")
	if err := os.WriteFile(output, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code := runCLI(t, "compress-files", "--paths", in, "--output", output)

	if code != -1 && code != 0 {
		t.Errorf("a clean run exited %d:\n%s", code, out)
	}
	for _, want := range []string{"overwritten: true", "failed: 0", "success: true"} {
		if !hasTOONLine(out, want) {
			t.Errorf("TOON output lacks %q:\n%s", want, out)
		}
	}
	if got, _ := os.ReadFile(output); string(got) == "old" {
		t.Errorf("the output still holds the old content")
	}
}

// An archive that cannot be finished is an error and exits 1. /dev/full
// accepts the open and fails the flush, so the zip writer's Close is where the
// write error surfaces: before T2 that Close was a defer, and this printed
// success: true. /dev/full is not a regular file, so it must also survive the
// cleanup that removes an unfinished archive.
func TestCompressFilesExitsOneWhenTheArchiveCannotBeFinished(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full on this platform")
	}
	in := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(in, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, code := runCLI(t, "compress-files", "--paths", in, "--output", "/dev/full")

	if code != 1 {
		t.Errorf("an unfinished archive exited %d, want 1:\n%s", code, out)
	}
	if hasTOONLine(out, "success: true") {
		t.Errorf("an unfinished archive reported success:\n%s", out)
	}
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Errorf("cleanup removed a device it did not create: %v", err)
	}
}
