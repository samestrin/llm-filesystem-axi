package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// AC13: bare `llm-filesystem` printed a cobra usage screen — the exact
// anti-pattern AXI principle 6 names. The root command had no Run, so cobra
// returned flag.ErrHelp and rendered the default help template.
//
// Content first means the landing page is DATA: what this binary is, where it
// is, and what is actually in the working directory right now. The usage screen
// stays one step away, behind --help.
//
// Asserting the ABSENCE of the usage markers is the load-bearing half. A home
// view that printed live data AND the usage screen would satisfy every positive
// assertion below while still costing an agent exactly the tokens the principle
// exists to save.
func TestBareInvocationPrintsLiveContentNotUsage(t *testing.T) {
	stdout, stderr, code := runCLI(t, []string{}...)

	if code != int(goaxi.ExitOK) && code != -1 {
		t.Errorf("exit = %d, want ExitOK (%d) or no explicit exit", code, goaxi.ExitOK)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty for a landing page", stderr)
	}

	for _, marker := range []string{"Usage:", "Available Commands:", "Flags:"} {
		if strings.Contains(stdout, marker) {
			t.Errorf("the usage screen is still the landing page (found %q): %q", marker, stdout)
		}
	}

	// The three identity fields the spec asks for, plus live data and a next
	// step. bin: is the running executable, so an agent knows what it is talking
	// to rather than guessing from $PATH.
	for _, want := range []string{"bin:", "about:", "cwd:", "items[", "help["} {
		if !strings.Contains(stdout, want) {
			t.Errorf("home view missing %q: %q", want, stdout)
		}
	}

	if !strings.Contains(stdout, "~") {
		t.Errorf("the home directory must be rendered with a ~ prefix: %q", stdout)
	}
}

// --help and --version are not the landing page, and adding a Run to the root
// must not change either. Cobra handles both before it reaches the Runnable
// check, but that is exactly the kind of thing worth pinning rather than
// trusting.
func TestHelpStillShowsTheUsageScreen(t *testing.T) {
	stdout, _, code := runCLI(t, "--help")

	if code != int(goaxi.ExitOK) && code != -1 {
		t.Errorf("--help exit = %d, want success", code)
	}
	if !strings.Contains(stdout, "Available Commands:") {
		t.Errorf("--help must still list the commands: %q", stdout)
	}
}

// An unknown subcommand must still fail loud after the root becomes runnable.
// Cobra resolves the command before checking Runnable, so this keeps working —
// but a regression here would turn every typo into a silent landing page, which
// is the worst possible failure mode for an agent.
func TestUnknownCommandIsNotSwallowedByTheHomeView(t *testing.T) {
	stdout, _, code := runCLI(t, "bogus-command")

	if code != int(goaxi.ExitUsage) {
		t.Errorf("exit = %d, want ExitUsage (%d); a typo must not render the home view",
			code, goaxi.ExitUsage)
	}
	if strings.Contains(stdout, "about:") {
		t.Errorf("an unknown command rendered the home view: %q", stdout)
	}
}

// runCLI called with no variadic arguments passes a NIL slice, and cobra falls
// back to os.Args[1:] when its args are nil — which under `go test` is the test
// binary's own flags, producing a bewildering unknown-flag failure. execute
// normalizes nil so no future test has to know that.
func TestBareInvocationWithNilArgsIsSafe(t *testing.T) {
	stdout, _, code := runCLI(t)

	if code == int(goaxi.ExitUsage) {
		t.Fatalf("nil args were parsed as the test binary's flags: %q", stdout)
	}
	if !strings.Contains(stdout, "about:") {
		t.Errorf("nil args did not render the home view: %q", stdout)
	}
}

// The human format has its own landing page. runHome's text renderer is the one
// part of it that a TOON or JSON test never reaches, so without this the whole
// branch ships unexercised.
func TestBareInvocationTextFormatIsHumanReadable(t *testing.T) {
	stdout, _, code := runCLI(t, "--format", "text")

	if code != int(goaxi.ExitOK) && code != -1 {
		t.Errorf("exit = %d, want ExitOK (%d) or no explicit exit", code, goaxi.ExitOK)
	}
	if strings.Contains(stdout, "Usage:") {
		t.Errorf("the text landing page is still the usage screen: %q", stdout)
	}
	for _, want := range []string{"bin: ", "cwd: ", "Next steps:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("text home view missing %q: %q", want, stdout)
		}
	}
}

// tildePath decides how every path in the landing view is displayed, so its
// edges are pinned directly rather than through whatever $HOME happens to be on
// the machine running the suite.
//
// The last case is the one that matters: a sibling directory sharing the home
// prefix must NOT be abbreviated. Matching on the prefix alone would turn
// /Users/sam-backup into ~-backup, which is not a path anything can resolve.
func TestTildePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory on this machine")
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"the home directory itself", home, "~"},
		{
			"a path under home",
			filepath.Join(home, "x", "y"),
			"~" + string(os.PathSeparator) + filepath.Join("x", "y"),
		},
		{"a path outside home", filepath.Join(string(os.PathSeparator), "tmp"), filepath.Join(string(os.PathSeparator), "tmp")},
		{"a sibling sharing the home prefix", home + "-backup", home + "-backup"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tildePath(c.in); got != c.want {
				t.Errorf("tildePath(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// AC13's degradation clause, exercised for real.
//
// The test below it named this clause but reached it through --allowed-dirs,
// which is the SANDBOX refusing a listing — a path that already degraded
// correctly. The clause that mattered was os.Getwd() failing, and that exits 1:
// runHome treats it as fatal. A landing page that dies because the working
// directory is unknowable tells an agent less than the usage screen it replaced,
// including what binary it is holding.
//
// The first version of this test deleted the working directory and hoped
// os.Getwd would fail. On macOS it does not — the process keeps resolving the
// deleted path — so the test SKIPPED, and the clause went unexercised while
// looking covered. A skipped test is not coverage.
//
// getwd is indirected for exactly the reason outWriter and exitFunc are: the
// path that actually ships is otherwise untestable.
func TestBareInvocationSurvivesAnUnknowableCwd(t *testing.T) {
	prev := getwd
	getwd = func() (string, error) { return "", errors.New("getwd: permission denied") }
	t.Cleanup(func() { getwd = prev })

	stdout, _, code := runCLI(t, []string{}...)

	if code != int(goaxi.ExitOK) && code != -1 {
		t.Errorf("a home view with an unknowable cwd must still exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "bin:") || !strings.Contains(stdout, "about:") {
		t.Errorf("identity must survive an unknowable cwd: %q", stdout)
	}
	if !strings.Contains(stdout, "help[") {
		t.Errorf("a degraded home view must still say what to do next: %q", stdout)
	}
}

// AC13: the landing page degrades rather than fails. A home view that exits
// non-zero because the working directory happens to be unreadable would be a
// worse landing than the usage screen it replaces — the agent learns nothing at
// all, including what binary it is holding.
func TestBareInvocationSurvivesAnUnreadableCwd(t *testing.T) {
	// Restrict the sandbox to somewhere the working directory is not, so the
	// listing is refused while identity stays knowable.
	stdout, _, code := runCLI(t, "--allowed-dirs", t.TempDir())

	if code != int(goaxi.ExitOK) && code != -1 {
		t.Errorf("a home view whose listing failed must still exit 0, got %d", code)
	}
	if !strings.Contains(stdout, "bin:") || !strings.Contains(stdout, "about:") {
		t.Errorf("identity must survive a failed listing: %q", stdout)
	}
	if !strings.Contains(stdout, "help[") {
		t.Errorf("a degraded home view must still say what to do next: %q", stdout)
	}
}
