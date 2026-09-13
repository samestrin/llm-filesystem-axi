package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// withFields sets the resolved --fields selection that PersistentPreRunE
// normally establishes, and restores it afterwards. A separate helper rather
// than a fourth parameter on withFormat, which would touch every existing call.
func withFields(t *testing.T, fields ...string) {
	t.Helper()

	prev := activeFields
	activeFields = fields
	t.Cleanup(func() { activeFields = prev })
}

// AC9: --full was all-or-nothing, so an agent that wanted one extra column had
// to pay for every column. AXI principle 2 asks for the extra fields to be
// requestable by name.
//
// The call-site spec here keeps only "name". Asking for name AND size can only
// pass if --fields REPLACED that keep-list rather than intersecting with it,
// which is the whole point: the caller names the fields, the call site names the
// array, and neither has to know the other.
func TestFieldsSelectsASubsetWiderThanTheCallSiteSpec(t *testing.T) {
	withFormat(t, FormatTOON, false, false)
	withFields(t, "name", "size")

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), map[string][]string{"items": {"name"}}, nil,
			func() string { return "TEXT" })
	})

	if !strings.Contains(stdout, "name") {
		t.Errorf("--fields dropped a field it was asked for: %q", stdout)
	}
	if !strings.Contains(stdout, "size") {
		t.Errorf("--fields did not widen past the call-site spec {items:{name}}: %q", stdout)
	}
	// Top-level aggregates are not list items and must survive regardless.
	if !strings.Contains(stdout, "total") {
		t.Errorf("--fields ate a top-level aggregate: %q", stdout)
	}
}

// The other direction: naming fewer fields than the call site must narrow it.
func TestFieldsNarrowsBelowTheCallSiteSpec(t *testing.T) {
	withFormat(t, FormatTOON, false, false)
	withFields(t, "size")

	stdout, _, _ := captureOutput(t, func() {
		OutputResultAXI(sample(), map[string][]string{"items": {"name", "size"}}, nil,
			func() string { return "TEXT" })
	})

	if !strings.Contains(stdout, "size") {
		t.Fatalf("--fields dropped the field it was asked for: %q", stdout)
	}
	if strings.Contains(stdout, "a.txt") {
		t.Errorf("--fields size still emitted the name values: %q", stdout)
	}
}

// AC9: a wrong field name must fail loud and say what the right ones are. An
// agent that guesses wrong learns the schema in one turn instead of probing.
// Silently returning empty items would be the worst outcome — indistinguishable
// from a result that genuinely has no data.
func TestUnknownFieldFailsLoudAndNamesTheValidOnes(t *testing.T) {
	withFormat(t, FormatTOON, false, false)
	withFields(t, "nosuchfield")

	stdout, _, code := captureOutput(t, func() {
		OutputResultAXI(sample(), map[string][]string{"items": {"name"}}, nil,
			func() string { return "TEXT" })
	})

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if !strings.Contains(stdout, "nosuchfield") {
		t.Errorf("the diagnostic must name the bad field: %q", stdout)
	}
	if !strings.Contains(stdout, "name") || !strings.Contains(stdout, "size") {
		t.Errorf("the diagnostic must enumerate the available fields: %q", stdout)
	}
}

// A field name is validated against what the payload actually holds, so an empty
// result has nothing to validate against. Rejecting there would fail a correct
// --fields purely because the directory happened to be empty.
func TestFieldsOnAnEmptyResultDoesNotFailValidation(t *testing.T) {
	dir := t.TempDir()

	stdout, _, code := runCLI(t, "list-directory", "--path", dir, "--fields", "name")

	if code == int(goaxi.ExitUsage) {
		t.Errorf("a valid --fields was rejected on an empty result: %q", stdout)
	}
	if strings.Contains(stdout, "error: true") {
		t.Errorf("an empty result with --fields reported an error: %q", stdout)
	}
}

// AC9: a blank name is a malformed request, not an empty selection. Dropping it
// quietly would turn "--fields name,,size" into a two-field selection the caller
// never asked for, which is exactly the silent-default behaviour AXI rejects.
func TestBlankFieldNameFailsLoud(t *testing.T) {
	stdout, _, code := runCLI(t, "list-directory", "--path", ".", "--fields", "name,,size")

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if !strings.Contains(strings.ToLower(stdout), "empty field name") {
		t.Errorf("the diagnostic must explain the blank: %q", stdout)
	}
}

// A command returning a single record has no list to select fields from.
// Ignoring --fields there would let an agent believe it had narrowed output that
// in fact came back whole — a wrong belief is worse than a refusal.
func TestFieldsOnACommandWithNoListFailsLoud(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCLI(t, "get-file-info", "--path", f, "--fields", "name")

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if !strings.Contains(stdout, "--fields") {
		t.Errorf("the diagnostic must name the flag: %q", stdout)
	}
}

// The --fields rejection lived in OutputResultAXI, which runs at RENDER time —
// after the command body has already done its work. So an unsupported --fields
// on a mutating command performed the mutation and THEN reported a usage error.
//
// That is worse than a mislabelled success. The docs promise exit 2 means
// nothing was touched, and the whole reason exit 2 is distinct from exit 1 is to
// tell an agent "fix the invocation and retry" — which for move-file,
// sync-directories or a batch is destructive the second time.
//
// delete-file is the clearest case because its side effect is unmistakable.
func TestFieldsRejectionHappensBeforeTheCommandRuns(t *testing.T) {
	f := newTempFile(t)

	_, _, code := runCLI(t, "delete-file", "--path", f, "--confirm", "--fields", "path")

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d)", code, goaxi.ExitUsage)
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("the file was DELETED by a command that then reported a usage error: %v", err)
	}
}

// The text branch returns before the --fields validation, so the same invalid
// input was a hard usage error in TOON and JSON and silently dropped in text.
// One invocation must not have three different verdicts.
func TestFieldsIsValidatedInEveryFormat(t *testing.T) {
	for _, f := range []Format{FormatTOON, FormatJSON, FormatText} {
		t.Run(string(f), func(t *testing.T) {
			_, _, code := runCLI(t, "--format", string(f),
				"list-directory", "--path", ".", "--fields", "nosuchfield")

			if code != int(goaxi.ExitUsage) {
				t.Errorf("exit = %d, want ExitUsage (%d); an invalid field must not depend on the format",
					code, goaxi.ExitUsage)
			}
		})
	}
}

// AC9 says --fields works on a list-shaped command. These three emit a files[]
// array and refused it, with a message claiming they return no list.
func TestListShapedCommandsAcceptFields(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{"read-multiple-files", []string{"read-multiple-files", "--paths", a, "--fields", "path"}},
		{"find-large-files", []string{"find-large-files", "--path", dir, "--min-size", "1", "--fields", "path"}},
		{"write-multiple-files", []string{"write-multiple-files",
			"--files", `[{"path":"` + filepath.Join(dir, "w.txt") + `","content":"x"}]`,
			"--fields", "path"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stdout, _, code := runCLI(t, c.args...)

			if strings.Contains(stdout, "returns no list") {
				t.Errorf("a list-shaped command claims it returns no list: %q", stdout)
			}
			if code == int(goaxi.ExitUsage) {
				t.Errorf("--fields was refused by a list-shaped command: %q", stdout)
			}
		})
	}
}

// Once read-multiple-files honours --fields, projection can reach a read result
// — and `--fields content` would strip the truncation metadata, handing back a
// partial file that looks complete. AC9 forbids exactly that.
func TestProjectionCannotHideTruncation(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, []byte(strings.Repeat("filler line here\n", 6000)), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, _ := runCLI(t, "read-multiple-files", "--paths", big, "--fields", "content")

	if !strings.Contains(stdout, "truncated") {
		t.Errorf("--fields content hid the truncation flag; the caller cannot tell this file is partial: %q",
			stdout[:min(len(stdout), 300)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// The list-key table in RootCmd matches commands by NAME, so a rename or a typo
// would silently stop annotating one — and --fields would start refusing a
// command that returns a list, with no compile error to catch it.
func TestEveryAnnotatedListCommandExists(t *testing.T) {
	root := RootCmd()

	annotated := 0
	for _, c := range root.Commands() {
		if c.Annotations[listAnnotation] != "" {
			annotated++
		}
	}

	// Seven subcommands are listed in the table; the root carries its own.
	if annotated != 7 {
		var got []string
		for _, c := range root.Commands() {
			if c.Annotations[listAnnotation] != "" {
				got = append(got, c.Name())
			}
		}
		t.Errorf("%d commands carry a list annotation, want 7; a name in the table no longer resolves. Annotated: %v",
			annotated, got)
	}
	if root.Annotations[listAnnotation] == "" {
		t.Error("the root command lost its annotation; the landing view lists a directory")
	}
}

// AC9: --fields and an explicit --full are contradictory instructions. Honouring
// one silently would leave the agent believing it got the other.
func TestFieldsAndFullAreMutuallyExclusive(t *testing.T) {
	stdout, _, code := runCLI(t, "list-directory", "--path", ".", "--full", "--fields", "name")

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d) for contradictory flags", code, goaxi.ExitUsage)
	}
	if !strings.Contains(stdout, "full") || !strings.Contains(stdout, "fields") {
		t.Errorf("the diagnostic must name both flags: %q", stdout)
	}
}

// AC9 + AC12: the ambient LLM_FILESYSTEM_FULL is a default, and an explicit
// --fields beats a default rather than colliding with it.
//
// This also guards AC12 from the other side. Full + JSON takes a legacy
// byte-identical early return that skips projection entirely, so if ambient full
// survived here, --fields would be silently ignored instead of applied.
func TestFieldsDefeatsAmbientFullAndReachesProjection(t *testing.T) {
	t.Setenv(FullEnvVar, "1")

	// The directory must contain something. With no items there are no item
	// fields either, so the assertions below would hold against any
	// implementation, including one that ignored --fields entirely.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runCLI(t, "--format", "json",
		"list-directory", "--path", dir, "--fields", "name")

	if !strings.Contains(stdout, "a.txt") {
		t.Fatalf("the listing lost its only item, so nothing below is meaningful: %q", stdout)
	}

	if code == int(goaxi.ExitUsage) {
		t.Fatalf("--fields with ambient full was treated as a usage error: %q", stdout)
	}
	if strings.Contains(stdout, "size_readable") || strings.Contains(stdout, "\"mode\"") {
		t.Errorf("ambient full won and --fields was ignored: %q", stdout)
	}
}
