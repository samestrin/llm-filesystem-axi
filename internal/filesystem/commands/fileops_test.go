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

// axi.md review round 1, item 2: the batch gate stat'ed op.Destination raw,
// while core runs it through NormalizePath. A copy onto '~/victim' or onto
// 'victim/' (an existing file) missed the existence check and overwrote it with
// exit 0 — the hole TestMoveAndCopyOverwriteGateNormalizesDestination closed
// for the single commands, left open one command across.
func TestBatchOverwriteGateNormalizesDestination(t *testing.T) {
	const precious = "IRREPLACEABLE"

	for _, op := range []string{"move", "copy"} {
		for name, spell := range map[string]func(victim string) string{
			"tilde":          func(string) string { return "~/victim.txt" },
			"trailing slash": func(victim string) string { return victim + "/" },
		} {
			t.Run(op+" "+name, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				src := filepath.Join(home, "src.txt")
				victim := filepath.Join(home, "victim.txt")
				if err := os.WriteFile(src, []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(victim, []byte(precious), 0o600); err != nil {
					t.Fatal(err)
				}

				ops := `[{"operation":"` + op + `","source":"` + src + `","destination":"` + spell(victim) + `"}]`
				_, _, code := runCLI(t, "batch-file-operations", "--operations", ops)

				if code != int(goaxi.ExitUsage) {
					t.Errorf("exit = %d, want ExitUsage (%d) for an unconfirmed overwrite", code, goaxi.ExitUsage)
				}
				got, err := os.ReadFile(victim)
				if err != nil || string(got) != precious {
					t.Errorf("destination = %q (err %v), want it untouched", got, err)
				}
			})
		}
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

// Epic 10 T5, AC4: move-file and copy-file replaced an existing destination
// silently — os.Rename and os.Create both overwrite without asking — while
// delete-file and the batch gate already refused the same destruction without
// --confirm. An agent one hallucinated path away from data loss got exit 0 and
// a cheerful "Moved"/"Copied". The gate must match the batch's: refusal is a
// usage error (exit 2, nothing attempted), the diagnostic names --confirm, and
// the destination's BYTES are what prove nothing was touched — an exit-code
// assertion alone would pass even if the overwrite happened first.
func TestMoveAndCopyOverwriteRequireConfirm(t *testing.T) {
	const precious = "IRREPLACEABLE"

	setup := func(t *testing.T) (src, victim string) {
		t.Helper()
		dir := t.TempDir()
		src = filepath.Join(dir, "src.txt")
		victim = filepath.Join(dir, "victim.txt")
		if err := os.WriteFile(src, []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(victim, []byte(precious), 0o600); err != nil {
			t.Fatal(err)
		}
		return src, victim
	}

	assertRefused := func(t *testing.T, cmd, src, victim, stdout, stderr string, code int) {
		t.Helper()
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
		// A move that refused must not have consumed the source either.
		gotSrc, err := os.ReadFile(src)
		if err != nil || string(gotSrc) != "replacement" {
			t.Errorf("source = %q (err %v), want it untouched", gotSrc, err)
		}
		if !strings.Contains(stdout, "--confirm") {
			t.Errorf("stdout = %q, want the diagnostic to name --confirm", stdout)
		}
		if !strings.Contains(stdout, "error: true") {
			t.Errorf("stdout = %q, want a structured TOON error (the default format)", stdout)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty; the error belongs on stdout", stderr)
		}
	}

	for _, cmd := range []string{"move-file", "copy-file"} {
		t.Run(cmd+" over an existing file refuses", func(t *testing.T) {
			src, victim := setup(t)
			stdout, stderr, code := runCLI(t, cmd, "--source", src, "--dest", victim)
			assertRefused(t, cmd, src, victim, stdout, stderr, code)
		})

		// cobra's required-flag check is satisfied by --confirm=false, because
		// the flag WAS provided. Only inspecting the value closes that hole
		// (the same one TestDeleteFileRefusesWithoutConfirm pins).
		t.Run(cmd+" --confirm=false refuses", func(t *testing.T) {
			src, victim := setup(t)
			stdout, stderr, code := runCLI(t, cmd, "--source", src, "--dest", victim, "--confirm=false")
			assertRefused(t, cmd, src, victim, stdout, stderr, code)
		})

		// The refusal must be structured in the format asked for, not only in
		// the TOON default: an agent running --format json parses the message.
		t.Run(cmd+" refusal is structured in json", func(t *testing.T) {
			src, victim := setup(t)
			stdout, _, code := runCLI(t, cmd, "--source", src, "--dest", victim, "--format", "json")
			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
			}
			assertStructuredError(t, FormatJSON, stdout, "--confirm")
		})

		// The gate must not become a wall: a confirmed overwrite still runs.
		t.Run(cmd+" --confirm replaces", func(t *testing.T) {
			src, victim := setup(t)
			_, _, code := runCLI(t, cmd, "--source", src, "--dest", victim, "--confirm")
			if code == int(goaxi.ExitUsage) {
				t.Fatal("a confirmed overwrite was rejected as a usage error")
			}
			got, err := os.ReadFile(victim)
			if err != nil || string(got) != "replacement" {
				t.Errorf("destination = %q (err %v), want the replacement", got, err)
			}
			if cmd == "move-file" {
				if _, err := os.Stat(src); !os.IsNotExist(err) {
					t.Error("a confirmed move left the source behind")
				}
			}
		})

		// The gate stays scoped to what destroys something: a move or copy to
		// a path that does not exist yet overwrites nothing and needs no
		// confirming, exactly as the batch gate treats it.
		t.Run(cmd+" to a fresh path needs no confirm", func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.txt")
			if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(dir, "fresh.txt")

			_, _, code := runCLI(t, cmd, "--source", src, "--dest", dst)
			if code == int(goaxi.ExitUsage) {
				t.Error("a " + cmd + " that overwrites nothing was gated")
			}
			if _, err := os.Stat(dst); err != nil {
				t.Errorf("the %s did not run: %v", cmd, err)
			}
		})
	}
}

// Review finding on PR #71 (HIGH): the gate stat-ed the raw --dest string,
// but core.MoveFile/CopyFile run it through core.NormalizePath, which expands
// a leading ~ and filepath.Clean's away trailing slashes. A destination
// spelled '~/victim.txt' or 'victim.txt/' therefore missed the existence
// check and was replaced silently with exit 0 — the exact destruction this
// gate exists to prevent. The gate must stat the same path core will write
// to, and print the confirmed command with that same normalized path so the
// suggested next step is runnable as printed.
func TestMoveAndCopyOverwriteGateNormalizesDestination(t *testing.T) {
	const precious = "IRREPLACEABLE"

	setup := func(t *testing.T) (src, victim string) {
		t.Helper()
		home := t.TempDir()
		t.Setenv("HOME", home)
		src = filepath.Join(home, "src.txt")
		victim = filepath.Join(home, "victim.txt")
		if err := os.WriteFile(src, []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(victim, []byte(precious), 0o600); err != nil {
			t.Fatal(err)
		}
		return src, victim
	}

	assertRefused := func(t *testing.T, src, victim, stdout, stderr string, code int) {
		t.Helper()
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
		gotSrc, err := os.ReadFile(src)
		if err != nil || string(gotSrc) != "replacement" {
			t.Errorf("source = %q (err %v), want it untouched", gotSrc, err)
		}
		if !strings.Contains(stdout, "--confirm") {
			t.Errorf("stdout = %q, want the diagnostic to name --confirm", stdout)
		}
		// The suggested command must carry the normalized destination, not
		// the raw spelling: '~/victim.txt' re-run with --confirm works, but
		// a suggestion that keeps a broken spelling is not a next step.
		if !strings.Contains(stdout, victim) {
			t.Errorf("stdout = %q, want the suggested command to use the normalized path %q", stdout, victim)
		}
		if !strings.Contains(stdout, "error: true") {
			t.Errorf("stdout = %q, want a structured TOON error (the default format)", stdout)
		}
		if stderr != "" {
			t.Errorf("stderr = %q, want empty; the error belongs on stdout", stderr)
		}
	}

	for _, cmd := range []string{"move-file", "copy-file"} {
		t.Run(cmd+" tilde destination refuses", func(t *testing.T) {
			src, victim := setup(t)
			stdout, stderr, code := runCLI(t, cmd, "--source", src, "--dest", "~/victim.txt")
			assertRefused(t, src, victim, stdout, stderr, code)
		})

		// os.Stat on 'victim.txt/' fails with ENOTDIR, but filepath.Clean
		// strips the slash and core overwrites the existing file.
		t.Run(cmd+" trailing-slash destination refuses", func(t *testing.T) {
			src, victim := setup(t)
			stdout, stderr, code := runCLI(t, cmd, "--source", src, "--dest", victim+"/")
			assertRefused(t, src, victim, stdout, stderr, code)
		})

		// The gate must not become a wall for normalized spellings either:
		// a confirmed overwrite still runs, with core doing the expansion.
		t.Run(cmd+" confirmed tilde destination replaces", func(t *testing.T) {
			src, victim := setup(t)
			_, _, code := runCLI(t, cmd, "--source", src, "--dest", "~/victim.txt", "--confirm")
			if code == int(goaxi.ExitUsage) {
				t.Fatal("a confirmed overwrite was rejected as a usage error")
			}
			got, err := os.ReadFile(victim)
			if err != nil || string(got) != "replacement" {
				t.Errorf("destination = %q (err %v), want the replacement", got, err)
			}
		})
	}

	// When normalization itself fails (here: ~ with no HOME), the gate must
	// defer to core's error path — not crash, not invent a refusal, and not
	// let anything run. core reports the failure and the victim stays intact.
	t.Run("normalization failure defers to core", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("HOME", "")
		src := filepath.Join(dir, "src.txt")
		victim := filepath.Join(dir, "victim.txt")
		if err := os.WriteFile(src, []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(victim, []byte(precious), 0o600); err != nil {
			t.Fatal(err)
		}

		_, _, code := runCLI(t, "copy-file", "--source", src, "--dest", "~/victim.txt")

		if code == 0 {
			t.Error("exit 0 although the destination could not be normalized")
		}
		if code == int(goaxi.ExitUsage) {
			t.Error("the gate refused on a path it could not normalize; core must report that error")
		}
		got, err := os.ReadFile(victim)
		if err != nil || string(got) != precious {
			t.Errorf("destination = %q (err %v), want it untouched", got, err)
		}
	})
}

// Epic 10 T5: the gate is scoped to move-file and copy-file. write-file keeps
// replacing an existing destination without --confirm — it is the tool an
// agent reaches for to rewrite a file it owns, and gating it would break every
// edit workflow that does not go through edit-block. This pins that half of
// the plan so a later "safety" sweep cannot quietly widen the gate.
func TestWriteFileStillReplacesWithoutConfirm(t *testing.T) {
	f := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(f, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, code := runCLI(t, "write-file", "--path", f, "--content", "new")

	if code == int(goaxi.ExitUsage) {
		t.Fatal("write-file was gated; the plan keeps it replacing without --confirm")
	}
	got, err := os.ReadFile(f)
	if err != nil || string(got) != "new" {
		t.Errorf("file = %q (err %v), want the replacement", got, err)
	}
}

// The gate covers overwrites too (TestBatchOverwriteRequiresConfirm), and the
// docs say so (TestKnownGapStatementsAreTrue), but --help said "Deletions
// require --confirm" and the flag said "Confirm any deletions". An agent that
// reads only --help plans a copy onto an existing file without --confirm and
// is refused. Both strings must name the overwrite case as well as deletes.
func TestBatchHelpNamesTheOverwriteGate(t *testing.T) {
	cmd := batchFileOperationsCmd()
	confirmFlag := cmd.Flags().Lookup("confirm")
	if confirmFlag == nil {
		t.Fatal("batch-file-operations has no --confirm flag")
	}

	for name, text := range map[string]string{
		"Long help":       cmd.Long,
		"--confirm usage": confirmFlag.Usage,
	} {
		if !strings.Contains(strings.ToLower(text), "delete") {
			t.Errorf("%s does not name deletes: %q", name, text)
		}
		if !strings.Contains(text, "move or copy onto an existing") {
			t.Errorf("%s does not name a move or copy onto an existing path: %q", name, text)
		}
	}
}
