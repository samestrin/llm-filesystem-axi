package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// release.yml used to run only on a v* tag, so the first time anyone learned
// whether the five-platform build and SHA256SUMS.txt still worked was the
// moment a release was being cut, and a broken step meant a broken release.
// It now also runs by hand and on pull requests that change it, building
// everything and publishing nothing. These guard the two halves of that
// promise: a non-tag run must not be able to publish, and a tag must still
// publish.
//
// There is no YAML parser in go.mod, so the file is split by indentation:
// top-level keys at column 0, jobs at column 2. That is the layout the file
// uses, and readReleaseWorkflow fails loudly if it stops matching.

type releaseWorkflow struct {
	sections map[string]string // top-level key -> its block
	jobs     map[string]string // job name -> its block
}

func readReleaseWorkflow(t *testing.T) releaseWorkflow {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("reading release.yml: %v", err)
	}
	w := releaseWorkflow{sections: map[string]string{}, jobs: map[string]string{}}
	var section, job string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			section = strings.TrimSuffix(strings.Fields(line)[0], ":")
			job = ""
		}
		w.sections[section] += line + "\n"
		if section == "jobs" && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") {
			job = strings.TrimSuffix(strings.TrimSpace(line), ":")
		}
		if job != "" {
			w.jobs[job] += line + "\n"
		}
	}
	for _, s := range []string{"on", "permissions", "jobs"} {
		if w.sections[s] == "" {
			t.Fatalf("release.yml has no top-level %q section; the split is wrong and these tests would pass vacuously", s)
		}
	}
	if len(w.jobs) == 0 {
		t.Fatal("release.yml has no jobs; the split is wrong and these tests would pass vacuously")
	}
	return w
}

// A manual run is the proof the task asks for, so the trigger has to exist,
// and the tag trigger that cuts real releases has to survive alongside it.
func TestReleaseWorkflowTriggers(t *testing.T) {
	w := readReleaseWorkflow(t)
	for _, want := range []string{"workflow_dispatch", "tags: ['v*']"} {
		if !strings.Contains(w.sections["on"], want) {
			t.Errorf("release.yml triggers do not include %q:\n%s", want, w.sections["on"])
		}
	}
}

// Exactly one job may publish, it is the only one with write access, and it
// runs only on a pushed v* tag. Gating on the ref alone is not enough: a manual
// run started from a tag also has a refs/tags/v ref, and must publish nothing.
func TestReleaseWorkflowPublishesOnlyOnATagPush(t *testing.T) {
	w := readReleaseWorkflow(t)

	if strings.Contains(w.sections["permissions"], "write") {
		t.Errorf("release.yml grants write at the top level, so every job could publish:\n%s", w.sections["permissions"])
	}

	var publishers []string
	for name, block := range w.jobs {
		publishes := strings.Contains(block, "action-gh-release")
		if publishes {
			publishers = append(publishers, name)
		}
		if strings.Contains(block, "contents: write") && !publishes {
			t.Errorf("job %q has contents: write but does not publish; only the publish job needs it", name)
		}
	}
	if len(publishers) != 1 {
		t.Fatalf("want exactly one publishing job, got %v", publishers)
	}
	pub := w.jobs[publishers[0]]
	for _, want := range []string{
		"if: github.event_name == 'push' && startsWith(github.ref, 'refs/tags/v')",
		"contents: write",
		"needs: build",
	} {
		if !strings.Contains(pub, want) {
			t.Errorf("publish job is missing %q:\n%s", want, pub)
		}
	}
}

// The build job is what a manual run proves, so it must do everything a
// release does short of publishing: verify, build all five platforms, write
// SHA256SUMS.txt, and check the result rather than trust it.
func TestReleaseWorkflowBuildJobBuildsEverything(t *testing.T) {
	w := readReleaseWorkflow(t)
	build := w.jobs["build"]
	if build == "" {
		t.Fatal("release.yml has no build job")
	}
	if strings.Contains(build, "if:") {
		t.Errorf("the build job is conditional; it must run on every trigger:\n%s", build)
	}
	for _, want := range []string{
		"go test -race ./...",
		"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64",
		"sha256sum * > SHA256SUMS.txt",
		"sha256sum -c SHA256SUMS.txt",
		"actions/upload-artifact",
	} {
		if !strings.Contains(build, want) {
			t.Errorf("build job is missing %q", want)
		}
	}
}
