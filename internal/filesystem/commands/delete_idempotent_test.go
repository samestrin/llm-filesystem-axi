package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fresh-context review finding 2 (EST-6608): delete-file on a path that is
// already gone printed error: true and exited 1. AXI principle 6: the state the
// caller asked for already holds, so the call acknowledges it and exits 0 —
// but says deleted: false, existed: false, so nobody reads it as a deletion.
func TestDeleteFileOnAMissingPathIsANoOp(t *testing.T) {
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone.txt")

	stdout, _, code := runCLI(t, "delete-file", "--path", gone, "--confirm", "--format", "json")
	if code != -1 {
		t.Fatalf("exit %d, want 0\n%s", code, stdout)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if doc["deleted"] != false || doc["existed"] != false {
		t.Errorf("deleted = %v, existed = %v; want false, false\n%s", doc["deleted"], doc["existed"], stdout)
	}
	if doc["error"] != nil {
		t.Errorf("a no-op reported an error:\n%s", stdout)
	}
	if msg, _ := doc["message"].(string); !strings.Contains(msg, "does not exist") {
		t.Errorf("message = %q, want it to say the path does not exist", msg)
	}

	// TOON (the default) and text say the same, so no format reads as a
	// deletion.
	stdout, _, code = runCLI(t, "delete-file", "--path", gone, "--confirm")
	if code != -1 || !hasTOONField(stdout, "deleted") || !strings.Contains(stdout, "deleted: false") {
		t.Errorf("toon: exit %d\n%s", code, stdout)
	}
	stdout, _, code = runCLI(t, "delete-file", "--path", gone, "--confirm", "--format", "text")
	if code != -1 || strings.Contains(stdout, "Deleted ") || !strings.Contains(stdout, "does not exist") {
		t.Errorf("text: exit %d\n%s", code, stdout)
	}
}

// The other direction: a real delete says deleted: true, existed: true, and the
// file is gone.
func TestDeleteFileReportsARealDeletion(t *testing.T) {
	f := newTempFile(t)

	stdout, _, code := runCLI(t, "delete-file", "--path", f, "--confirm", "--format", "json")
	if code != -1 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	if _, err := os.Lstat(f); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
	var doc map[string]any
	_ = json.Unmarshal([]byte(stdout), &doc)
	if doc["deleted"] != true || doc["existed"] != true {
		t.Errorf("deleted = %v, existed = %v; want true, true\n%s", doc["deleted"], doc["existed"], stdout)
	}
}

// A missing parent is not "already deleted": the path is likely a typo, and
// acknowledging it would hide that. Nor is a path outside --allowed-dirs.
// Both stay errors.
func TestDeleteFileStillFailsForAMissingParentOrOutsideAllowedDirs(t *testing.T) {
	t.Run("missing parent", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "no-such-dir", "f.txt")
		stdout, _, code := runCLI(t, "delete-file", "--path", p, "--confirm", "--format", "json")
		if code != 1 || !strings.Contains(stdout, `"error"`) {
			t.Errorf("exit %d, want 1 with an error\n%s", code, stdout)
		}
	})

	t.Run("outside --allowed-dirs", func(t *testing.T) {
		allowed := t.TempDir()
		p := filepath.Join(t.TempDir(), "gone.txt")
		stdout, _, code := runCLI(t, "--allowed-dirs", allowed, "delete-file", "--path", p, "--confirm", "--format", "json")
		if code != 1 || !strings.Contains(stdout, `"error"`) {
			t.Errorf("exit %d, want 1 with an error\n%s", code, stdout)
		}
	})

	// A parent that is a file is not a directory the path is missing from.
	t.Run("parent is a file", func(t *testing.T) {
		f := newTempFile(t)
		stdout, _, code := runCLI(t, "delete-file", "--path", filepath.Join(f, "x"), "--confirm", "--format", "json")
		if code != 1 || !strings.Contains(stdout, `"error"`) {
			t.Errorf("exit %d, want 1 with an error\n%s", code, stdout)
		}
		if _, err := os.Stat(f); err != nil {
			t.Errorf("the parent file was touched: %v", err)
		}
	})

	// A dangling symlink exists: it is deleted, not reported as missing.
	t.Run("dangling symlink is deleted", func(t *testing.T) {
		dir := t.TempDir()
		link := filepath.Join(dir, "link")
		if err := os.Symlink(filepath.Join(dir, "nowhere"), link); err != nil {
			t.Skip(err)
		}
		stdout, _, code := runCLI(t, "delete-file", "--path", link, "--confirm", "--format", "json")
		if code != -1 || !strings.Contains(stdout, `"deleted": true`) {
			t.Errorf("exit %d\n%s", code, stdout)
		}
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("the link is still there: %v", err)
		}
	})
}

// The same rule inside batch-file-operations: a delete of a missing path
// counts as a success and says it deleted nothing; a missing parent still
// fails.
func TestBatchDeleteOfAMissingPathIsANoOp(t *testing.T) {
	dir := t.TempDir()
	ops := `[{"operation":"delete","source":"` + filepath.Join(dir, "gone.txt") + `"},` +
		`{"operation":"delete","source":"` + filepath.Join(dir, "nope", "x.txt") + `"}]`

	stdout, _, code := runCLI(t, "batch-file-operations", "--operations", ops, "--confirm", "--format", "json")
	var doc struct {
		Success    int              `json:"success"`
		Failed     int              `json:"failed"`
		Operations []map[string]any `json:"operations"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("exit %d, not JSON: %v\n%s", code, err, stdout)
	}
	if doc.Success != 1 || doc.Failed != 1 || len(doc.Operations) != 2 {
		t.Fatalf("success %d, failed %d; want 1, 1\n%s", doc.Success, doc.Failed, stdout)
	}
	first := doc.Operations[0]
	if first["success"] != true || first["deleted"] != false || first["existed"] != false {
		t.Errorf("missing-path entry = %v; want success, deleted false, existed false", first)
	}
	if doc.Operations[1]["success"] != false {
		t.Errorf("missing-parent entry = %v; want a failure", doc.Operations[1])
	}
}
