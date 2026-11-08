package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TD #39: create-directory on a directory that already existed reported
// "Created directory" and created: true, claiming work that never happened.
// The core fix is tested in core/write_test.go, but the user-facing message is
// a separate branch in the command, and deleting it left every test green.
// This drives the real command twice and checks what a caller actually sees.
func TestCreateDirectoryReportsExistingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "made")

	stdout, _, code := runCLI(t, "create-directory", "--path", dir)
	if code > 0 {
		t.Fatalf("first run exited %d\n%s", code, stdout)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("first run did not create the directory: %v", err)
	}
	if !hasTOONLine(stdout, "created: true") || !hasTOONLine(stdout, "exists: false") {
		t.Errorf("first run should report created: true / exists: false\n%s", stdout)
	}

	t.Run("text says it already existed", func(t *testing.T) {
		stdout, _, code := runCLI(t, "create-directory", "--path", dir, "--format", "text")
		if code > 0 {
			t.Fatalf("repeat run exited %d\n%s", code, stdout)
		}
		if !strings.Contains(stdout, "Directory already exists: ") {
			t.Errorf("want \"Directory already exists\"\n%s", stdout)
		}
		if strings.Contains(stdout, "Created directory") {
			t.Errorf("repeat run claims it created the directory\n%s", stdout)
		}
	})

	t.Run("toon reports created false, exists true", func(t *testing.T) {
		stdout, _, code := runCLI(t, "create-directory", "--path", dir)
		if code > 0 {
			t.Fatalf("repeat run exited %d\n%s", code, stdout)
		}
		if !hasTOONField(stdout, "created") || !hasTOONField(stdout, "exists") {
			t.Fatalf("missing created/exists fields\n%s", stdout)
		}
		if !hasTOONLine(stdout, "created: false") {
			t.Errorf("want created: false\n%s", stdout)
		}
		if !hasTOONLine(stdout, "exists: true") {
			t.Errorf("want exists: true\n%s", stdout)
		}
	})
}
