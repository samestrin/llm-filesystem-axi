package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The MCP server was removed (technical debt #4). It was a second front end
// that shelled out to this CLI, so every command change had to land twice and
// the two drifted. This guards against it coming back half-way: a build step
// that still builds a binary whose source is gone fails the release, and a
// leftover package keeps a second set of argument names alive for the docs
// oracle in docs_claims_test.go to accept.
//
// Only code, build and CI files are checked. The CHANGELOG has to be able to
// say the server was removed, and the user docs are cleaned up separately.
//
// The names are split so this file does not trip its own check.
func TestNoMCPServerInCodeOrBuild(t *testing.T) {
	banned := []string{"mcp" + "server", "llm-filesystem" + "-mcp"}

	root := "../../.."
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "build", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") || path == filepath.Join(root, "go.mod") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning source: %v", err)
	}
	for _, f := range []string{"Makefile", "install.sh"} {
		files = append(files, filepath.Join(root, f))
	}
	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("globbing workflows: %v", err)
	}
	files = append(files, workflows...)

	if len(workflows) == 0 || len(files) < 20 {
		t.Fatalf("found %d files (%d workflows); the paths are wrong and this test would pass vacuously",
			len(files), len(workflows))
	}

	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		for _, name := range banned {
			if strings.Contains(string(body), name) {
				t.Errorf("%s still names %q; the MCP server was removed", f, name)
			}
		}
	}
}
