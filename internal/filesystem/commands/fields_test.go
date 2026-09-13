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
