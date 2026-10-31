package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
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
				return []string{"safe-edit", "--path", file, "--old", "foo", "--new", "bar", "--backup=false"}
			},
			applied: "bar foo\n",
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
