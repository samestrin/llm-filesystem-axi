package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// editFixture writes one file containing "foo" and returns its directory and path.
func editFixture(t *testing.T) (dir, file string) {
	t.Helper()

	dir = t.TempDir()
	file = filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("foo foo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, file
}

func assertFileContent(t *testing.T, file, want string) {
	t.Helper()

	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", file, got, want)
	}
}

// TD #3: safe-edit and search-and-replace marked a preview only in their TEXT
// renderer. In TOON and JSON — the default and the machine format — a dry run of
// search-and-replace was byte-identical to a real one, and safe-edit differed
// only in a prose message. An agent could not tell whether its files had changed
// without parsing English. sync-directories already carries dry_run in every
// format; these two now do the same.
func TestEditDryRunIsVisibleInMachineFormats(t *testing.T) {
	commands := []struct {
		name string
		args func(dir, file string) []string
		// applied is the file content after a real run.
		applied string
	}{
		{
			name: "safe-edit",
			args: func(_, file string) []string {
				// The whole line, because "foo" alone matches twice and an
				// ambiguous safe-edit is now refused (it used to change the
				// first "foo" silently, which this case once encoded).
				return []string{"safe-edit", "--path", file, "--old", "foo foo", "--new", "bar", "--backup=false"}
			},
			applied: "bar\n",
		},
		{
			name: "search-and-replace",
			args: func(dir, _ string) []string {
				return []string{"search-and-replace", "--path", dir, "--pattern", "foo", "--replacement", "bar"}
			},
			applied: "bar bar\n",
		},
	}

	for _, c := range commands {
		t.Run(c.name+"/toon", func(t *testing.T) {
			dir, file := editFixture(t)

			stdout, _, _ := runCLI(t, append(c.args(dir, file), "--dry-run")...)

			if !hasTOONField(stdout, "dry_run") {
				t.Errorf("a TOON preview is indistinguishable from a real run: %q", stdout)
			}
			assertFileContent(t, file, "foo foo\n")
		})

		t.Run(c.name+"/json", func(t *testing.T) {
			dir, file := editFixture(t)

			stdout, _, _ := runCLI(t, append([]string{"--format", "json"},
				append(c.args(dir, file), "--dry-run")...)...)

			var got struct {
				DryRun bool `json:"dry_run"`
			}
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("a preview must be one JSON document: %v: %q", err, stdout)
			}
			if !got.DryRun {
				t.Errorf("dry_run missing from a JSON preview: %q", stdout)
			}
			assertFileContent(t, file, "foo foo\n")
		})

		// The other direction: a real run must not claim to be a preview, or
		// the field is useless as a discriminator.
		t.Run(c.name+"/a real run carries no dry_run", func(t *testing.T) {
			dir, file := editFixture(t)

			stdout, _, _ := runCLI(t, c.args(dir, file)...)
			if hasTOONField(stdout, "dry_run") {
				t.Errorf("a real TOON run reported dry_run: %q", stdout)
			}
			assertFileContent(t, file, c.applied)

			dir, file = editFixture(t)
			stdout, _, _ = runCLI(t, append([]string{"--format", "json"}, c.args(dir, file)...)...)
			var got map[string]any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("not one JSON document: %v: %q", err, stdout)
			}
			if _, ok := got["dry_run"]; ok {
				t.Errorf("a real JSON run reported dry_run: %q", stdout)
			}
			assertFileContent(t, file, c.applied)
		})
	}
}

// An old string matching more than once used to change the first occurrence
// and exit 0. Each string edit now refuses at exit 1 with the count, and its
// help names both ways out: --replace-all, or a longer old string. The file is
// checked, not just the exit code.
func TestAmbiguousEditIsRefusedWithRecovery(t *testing.T) {
	cases := []struct {
		name string
		args func(file string) []string
	}{
		{"edit-block", func(f string) []string {
			return []string{"edit-block", "--path", f, "--old", "foo", "--new", "bar"}
		}},
		{"safe-edit", func(f string) []string {
			return []string{"safe-edit", "--path", f, "--old", "foo", "--new", "bar"}
		}},
		{"edit-blocks", func(f string) []string {
			return []string{"edit-blocks", "--path", f, "--edits", `[{"old_string":"foo","new_string":"bar"}]`}
		}},
		{"edit-multiple-blocks", func(f string) []string {
			return []string{"edit-multiple-blocks", "--path", f, "--edits", `[{"old_text":"foo","new_text":"bar"}]`}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, file := editFixture(t)

			stdout, _, code := runCLI(t, c.args(file)...)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if !strings.Contains(stdout, "matches 2 times") {
				t.Errorf("the refusal does not name the match count: %q", stdout)
			}
			if !strings.Contains(stdout, "--replace-all") || !strings.Contains(stdout, "longer") {
				t.Errorf("help does not offer --replace-all and a longer old string: %q", stdout)
			}
			assertFileContent(t, file, "foo foo\n")
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("a refused edit left files behind: %v", entries)
			}

			stdout, _, code = runCLI(t, append(c.args(file), "--replace-all")...)
			if code != -1 {
				t.Errorf("--replace-all exit = %d: %q", code, stdout)
			}
			if !hasTOONField(stdout, "replacements") || !strings.Contains(stdout, "replacements: 2") {
				t.Errorf("--replace-all does not report replacements: 2: %q", stdout)
			}
			assertFileContent(t, file, "bar bar\n")
		})
	}
}

// outputEditError routes every error that is not an ambiguous match through the
// ordinary tool-failure path. A not-found edit must still exit 1 with the
// generic --help pointer and no --replace-all advice, which does not apply.
func TestEditNotFoundTakesTheOrdinaryErrorPath(t *testing.T) {
	cases := map[string][]string{
		"edit-block":           {"--old", "absent", "--new", "x"},
		"safe-edit":            {"--old", "absent", "--new", "x"},
		"edit-blocks":          {"--edits", `[{"old_string":"absent","new_string":"x"}]`},
		"edit-multiple-blocks": {"--edits", `[{"old_text":"absent","new_text":"x"}]`},
	}
	for name, rest := range cases {
		t.Run(name, func(t *testing.T) {
			_, file := editFixture(t)
			stdout, _, code := runCLI(t, append([]string{name, "--path", file}, rest...)...)
			if code != 1 {
				t.Errorf("exit = %d, want 1: %q", code, stdout)
			}
			if !strings.Contains(stdout, name+" --help") || strings.Contains(stdout, "--replace-all") {
				t.Errorf("want the generic --help pointer and no --replace-all advice: %q", stdout)
			}
			assertFileContent(t, file, "foo foo\n")
		})
	}
}
