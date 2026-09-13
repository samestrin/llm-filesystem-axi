package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goaxi "github.com/samestrin/go-axi"
)

// bigFile writes a line-structured file comfortably over DefaultMaxSize (70000),
// so a truncating read has a line boundary to cut on.
func bigFile(t *testing.T) (path string, size int) {
	t.Helper()

	body := strings.Repeat("line of filler text\n", 6000) // 120,000 bytes
	path = filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, len(body)
}

// AC8: a file over the budget was REFUSED, and refused inconsistently. Under
// --format json the refusal exited 0 while printing an error body, so an agent
// checking the exit code was told the read had succeeded. TOON and text exited 1
// for the identical condition. One condition, two contracts, and one of them a
// lie.
//
// The refusal also bypassed the renderer: commands/read.go called fmt.Println
// against the real os.Stdout instead of the outWriter indirection, which is why
// the captured stdout below is empty today and why no test could ever see it.
//
// AXI principle 3 asks for truncation with a size hint, not a refusal. Asserting
// the exit code and the body TOGETHER is what makes this non-vacuous: today's
// JSON refusal already exits 0, so an exit-code-only test would pass against the
// bug it exists to catch.
func TestOversizedReadIsTruncatedNotRefused(t *testing.T) {
	path, total := bigFile(t)

	for _, f := range []Format{FormatTOON, FormatJSON} {
		t.Run(string(f), func(t *testing.T) {
			stdout, _, code := runCLI(t, "--format", string(f), "read-file", "--path", path)

			if stdout == "" {
				t.Fatal("nothing reached the captured sink; the renderer was bypassed")
			}
			if code != int(goaxi.ExitOK) && code != -1 {
				t.Errorf("exit = %d, want ExitOK (%d) or no explicit exit", code, goaxi.ExitOK)
			}
			if strings.Contains(stdout, "error: true") || strings.Contains(stdout, `"error":true`) {
				t.Errorf("a truncated read reported an error body: %q", truncForMsg(stdout))
			}
		})
	}

	t.Run("json carries the size hint", func(t *testing.T) {
		stdout, _, _ := runCLI(t, "--format", "json", "read-file", "--path", path)

		var got struct {
			Content    string `json:"content"`
			Size       int64  `json:"size"`
			Truncated  bool   `json:"truncated"`
			TotalSize  int64  `json:"total_size"`
			NextOffset int64  `json:"next_offset"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("a truncated read must still be one JSON document: %v", err)
		}

		if !got.Truncated {
			t.Error("truncated flag not set on a truncated read")
		}
		if got.TotalSize != int64(total) {
			t.Errorf("total_size = %d, want the real file size %d", got.TotalSize, total)
		}
		if got.Size >= int64(total) {
			t.Errorf("size = %d, want less than the whole file (%d)", got.Size, total)
		}
		if got.NextOffset != got.Size {
			t.Errorf("next_offset = %d, want it to equal the bytes returned (%d)", got.NextOffset, got.Size)
		}
		if got.Content == "" {
			t.Error("a truncated read returned no content at all")
		}
	})

	t.Run("toon carries guidance naming the escape hatch", func(t *testing.T) {
		stdout, _, _ := runCLI(t, "read-file", "--path", path)

		if !strings.Contains(stdout, "help[") {
			t.Fatalf("a truncated read gave no next step: %q", truncForMsg(stdout))
		}
		if !strings.Contains(stdout, "--start-offset") {
			t.Errorf("guidance must name how to resume: %q", truncForMsg(stdout))
		}
		if !strings.Contains(stdout, "--full") {
			t.Errorf("guidance must name how to get the whole file: %q", truncForMsg(stdout))
		}
	})
}

// AC8: the same file and the same limit must produce the same exit code however
// the output happens to be formatted. This is the specific defect: json exited
// 0, toon and text exited 1.
func TestTruncationExitCodeIsIdenticalAcrossFormats(t *testing.T) {
	path, _ := bigFile(t)

	codes := map[Format]int{}
	for _, f := range []Format{FormatTOON, FormatJSON, FormatText} {
		_, _, code := runCLI(t, "--format", string(f), "read-file", "--path", path)
		codes[f] = code
	}

	if codes[FormatTOON] != codes[FormatJSON] || codes[FormatTOON] != codes[FormatText] {
		t.Errorf("exit codes differ by format: toon=%d json=%d text=%d",
			codes[FormatTOON], codes[FormatJSON], codes[FormatText])
	}
}

// AC8: truncation needs an escape hatch, and --full is it. Extending --full from
// "all fields" to "all fields and all bytes" keeps one flag meaning "do not
// reduce what you return" rather than adding a second one to discover.
func TestFullFlagDefeatsTruncation(t *testing.T) {
	path, total := bigFile(t)

	stdout, _, code := runCLI(t, "--format", "json", "--full", "read-file", "--path", path)

	if code != int(goaxi.ExitOK) && code != -1 {
		t.Errorf("exit = %d, want success", code)
	}

	var got struct {
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--full read must be one JSON document: %v", err)
	}
	if got.Truncated {
		t.Error("--full still truncated the read")
	}
	if len(got.Content) != total {
		t.Errorf("content = %d bytes, want the whole file (%d)", len(got.Content), total)
	}
}

// An explicit --max-size is more specific than --full, so it wins. This mirrors
// the precedence --format already has over the legacy --json.
func TestExplicitMaxSizeBeatsFull(t *testing.T) {
	path, total := bigFile(t)

	stdout, _, _ := runCLI(t, "--format", "json", "--full",
		"read-file", "--path", path, "--max-size", "1000")

	var got struct {
		Size      int64 `json:"size"`
		Truncated bool  `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("read must be one JSON document: %v", err)
	}
	if !got.Truncated {
		t.Error("an explicit --max-size was overridden by --full")
	}
	if got.Size >= int64(total) {
		t.Errorf("size = %d, want it bounded by --max-size", got.Size)
	}
}

// A file inside the budget must be untouched: no truncation flags, whole
// content. This is the guard that truncation did not leak into the normal path.
func TestSmallReadIsNotMarkedTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	body := "small content\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, _ := runCLI(t, "--format", "json", "read-file", "--path", path)

	var got struct {
		Content    string `json:"content"`
		Truncated  bool   `json:"truncated"`
		TotalSize  int64  `json:"total_size"`
		NextOffset int64  `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("read must be one JSON document: %v", err)
	}
	if got.Truncated {
		t.Error("a file within the budget was marked truncated")
	}
	if got.Content != body {
		t.Errorf("content = %q, want %q", got.Content, body)
	}
	if got.TotalSize != 0 || got.NextOffset != 0 {
		t.Errorf("a complete read emitted truncation metadata: total_size=%d next_offset=%d",
			got.TotalSize, got.NextOffset)
	}
}

// truncForMsg keeps a failure message readable when the body is a large file.
func truncForMsg(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
