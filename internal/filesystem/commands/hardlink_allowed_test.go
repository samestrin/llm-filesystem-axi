package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hardLinkJail builds an --allowed-dirs root A holding A/hl, a hard link to a
// file outside A. A hard link has no target to resolve, so the allowed-dirs
// check passed it, and write-file opened it O_TRUNC (or O_APPEND) in place,
// changing the outside file with exit 0 (EST-8338).
func hardLinkJail(t *testing.T) (allowed, secret, link string) {
	t.Helper()
	allowed, outside := t.TempDir(), t.TempDir()
	secret = filepath.Join(outside, "secret.txt")
	mustWrite(t, secret, "outside data")
	link = filepath.Join(allowed, "hl")
	if err := os.Link(secret, link); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	return allowed, secret, link
}

// The assertion is on the outside file's bytes: the bug exited 0, and only
// the outside file shows the write went through.
func TestAllowedDirsRefusesWriteThroughHardLink(t *testing.T) {
	cases := []struct {
		name string
		args func(a, link string) []string
		// write-file refuses the request (2); write-multiple-files reports
		// the entry failed and exits 1 like any failed entry (review round 2:
		// the docs claimed 2 for both).
		want int
	}{
		{"write-file", func(a, link string) []string {
			return []string{"--allowed-dirs", a, "write-file", "--path", link, "--content", "clobbered"}
		}, 2},
		{"write-file --append", func(a, link string) []string {
			return []string{"--allowed-dirs", a, "write-file", "--path", link, "--content", "appended", "--append"}
		}, 2},
		{"write-multiple-files", func(a, link string) []string {
			files := `[{"path":` + string(jsonString(link)) + `,"content":"clobbered"}]`
			return []string{"--allowed-dirs", a, "write-multiple-files", "--files", files}
		}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, secret, link := hardLinkJail(t)
			_, _, code := runCLI(t, c.args(a, link)...)
			if got := readString(t, secret); got != "outside data" {
				t.Errorf("outside file now holds %q", got)
			}
			if code != c.want {
				t.Errorf("exit %d, want %d", code, c.want)
			}
		})
	}
}

// The refusal is a refusal of the request: nothing was written, so exit 2.
func TestAllowedDirsHardLinkRefusalExitsTwo(t *testing.T) {
	a, _, link := hardLinkJail(t)
	if _, _, code := runCLI(t, "--allowed-dirs", a, "write-file", "--path", link, "--content", "x"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

// Known-good: an ordinary file under --allowed-dirs still writes and appends,
// and without --allowed-dirs a hard link is written through as Unix does.
func TestHardLinkGateLeavesOrdinaryWritesAlone(t *testing.T) {
	a := t.TempDir()
	p := filepath.Join(a, "f.txt")
	mustWrite(t, p, "one")
	if _, _, code := runCLI(t, "--allowed-dirs", a, "write-file", "--path", p, "--content", "+two", "--append"); code != -1 {
		t.Errorf("append to a single-link file: exit %d", code)
	}
	if got := readString(t, p); got != "one+two" {
		t.Errorf("file is %q", got)
	}

	_, secret, link := hardLinkJail(t)
	if _, _, code := runCLI(t, "write-file", "--path", link, "--content", "shared"); code != -1 {
		t.Errorf("write through a hard link with no --allowed-dirs: exit %d", code)
	}
	if got := readString(t, secret); got != "shared" {
		t.Errorf("linked file is %q", got)
	}
}

// An edit renamed a temp file over its target, which split a hard link: the
// other name kept the old bytes and the edit exited 0. It now writes in place
// and says so in a field, in TOON and JSON, and in the TEXT summary (EST-8805).
func TestEditThroughHardLinkReportsInPlace(t *testing.T) {
	cases := []struct {
		name string
		args func(p string) []string
	}{
		{"safe-edit", func(p string) []string {
			return []string{"safe-edit", "--path", p, "--old", "outside", "--new", "edited", "--backup=false"}
		}},
		{"edit-multiple-blocks", func(p string) []string {
			return []string{"edit-multiple-blocks", "--path", p, "--edits", `[{"old_text":"outside","new_text":"edited"}]`, "--backup=false"}
		}},
		// large-write-file replaced the linked name and kept quiet (EST-8806).
		{"large-write-file", func(p string) []string {
			return []string{"large-write-file", "--path", p, "--content", "edited data", "--backup=false"}
		}},
	}
	for _, c := range cases {
		for _, format := range []string{"toon", "json", "text"} {
			t.Run(c.name+"/"+format, func(t *testing.T) {
				_, secret, link := hardLinkJail(t)
				out, stderr, code := runCLI(t, append([]string{"--format", format}, c.args(link)...)...)
				if code != -1 {
					t.Fatalf("exit %d: %s", code, stderr)
				}
				for _, p := range []string{secret, link} {
					if got := readString(t, p); got != "edited data" {
						t.Errorf("%s holds %q, want the edit through both names", p, got)
					}
				}
				var says bool
				switch format {
				case "toon":
					says = hasTOONField(out, "in_place")
				case "json":
					says = strings.Contains(out, `"in_place": true`) || strings.Contains(out, `"in_place":true`)
				default:
					says = strings.Contains(out, "written in place, not atomically, because the file has 2 hard links")
				}
				if !says {
					t.Errorf("output does not say the write was in place:\n%s", out)
				}
			})
		}
	}
}

// Under --allowed-dirs the other name may be outside, so the edit is refused
// as write-file refuses it: exit 2, and the outside file keeps its bytes.
func TestAllowedDirsRefusesEditThroughHardLink(t *testing.T) {
	for _, args := range [][]string{
		{"safe-edit", "--old", "outside", "--new", "edited"},
		{"edit-multiple-blocks", "--edits", `[{"old_text":"outside","new_text":"edited"}]`},
		{"large-write-file", "--content", "edited data"},
	} {
		t.Run(args[0], func(t *testing.T) {
			a, secret, link := hardLinkJail(t)
			full := append([]string{"--allowed-dirs", a, args[0], "--path", link}, args[1:]...)
			out, stderr, code := runCLI(t, full...)
			for _, p := range []string{secret, link} {
				if got := readString(t, p); got != "outside data" {
					t.Errorf("%s now holds %q", p, got)
				}
			}
			if code != 2 || !strings.Contains(out+stderr, "hard links") {
				t.Errorf("exit %d, want 2 with the hard-link refusal: %s%s", code, out, stderr)
			}
		})
	}
}
