package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSizeExceededError and TestTotalSizeExceededError were removed together
// with the types they exercised.
//
// Reads truncate instead of refusing (AC8), so size stopped being an error class
// and SizeExceededError / TotalSizeExceededError lost their last caller. These
// tests were deleted WITH the production code, not to make a failing suite pass:
// a green test over code that no longer exists is not coverage, it is a claim of
// coverage. This repo already named that failure mode in commands/root.go —
// "every output test exercised a function production never called".
//
// Every behavioural assertion those size limits had is still here, rewritten
// below against the truncated success value and strengthened to pin the budget
// arithmetic the originals never checked.

func TestReadFileSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file that exceeds the default size limit
	largeContent := strings.Repeat("a", 100000) // 100KB
	largeFile := filepath.Join(tmpDir, "large.txt")
	if err := os.WriteFile(largeFile, []byte(largeContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a small file
	smallContent := "small content"
	smallFile := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(smallFile, []byte(smallContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Rewritten from "returns error". The old version only proved a refusal
	// happened; it never pinned the arithmetic. These assertions are strictly
	// stronger: the budget is respected, the hint is accurate, the resume point
	// is usable, and the content is genuinely the head of the file.
	t.Run("file over the default limit is truncated, not refused", func(t *testing.T) {
		result, err := ReadFile(ReadFileOptions{
			Path:             largeFile,
			AllowedDirs:      []string{tmpDir},
			SizeCheckMaxSize: 0, // Use default (70000)
		})

		if err != nil {
			t.Fatalf("a large file must read as a truncated success, got %v", err)
		}
		if !result.Truncated {
			t.Error("truncated flag not set on an over-budget read")
		}
		if result.TotalSize != 100000 {
			t.Errorf("total_size = %d, want the real file size 100000", result.TotalSize)
		}
		if int64(len(result.Content)) >= 100000 {
			t.Errorf("content = %d bytes, want less than the whole file", len(result.Content))
		}
		if est := int64(EstimateJSONStringSize(result.Content)); est > DefaultMaxSize {
			t.Errorf("truncated content still costs %d chars, over the %d budget", est, DefaultMaxSize)
		}
		if result.NextOffset != int64(len(result.Content)) {
			t.Errorf("next_offset = %d, want the bytes returned (%d)", result.NextOffset, len(result.Content))
		}
		if !strings.HasPrefix(largeContent, result.Content) {
			t.Error("content is not a prefix of the file, so next_offset would not resume correctly")
		}
	})

	t.Run("file within size limit succeeds", func(t *testing.T) {
		result, err := ReadFile(ReadFileOptions{
			Path:             smallFile,
			AllowedDirs:      []string{tmpDir},
			SizeCheckMaxSize: 0, // Use default
		})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.Content != smallContent {
			t.Errorf("Expected content=%q, got %q", smallContent, result.Content)
		}
	})

	t.Run("custom size limit truncates to that budget", func(t *testing.T) {
		result, err := ReadFile(ReadFileOptions{
			Path:             smallFile,
			AllowedDirs:      []string{tmpDir},
			SizeCheckMaxSize: 5,
		})

		if err != nil {
			t.Fatalf("a custom budget must truncate, not refuse: %v", err)
		}
		if !result.Truncated {
			t.Error("truncated flag not set under a custom budget")
		}
		if est := int64(EstimateJSONStringSize(result.Content)); est > 5 {
			t.Errorf("content costs %d chars, over the budget of 5", est)
		}
		if result.TotalSize != int64(len(smallContent)) {
			t.Errorf("total_size = %d, want %d", result.TotalSize, len(smallContent))
		}
		if !strings.HasPrefix(smallContent, result.Content) {
			t.Error("content is not a prefix of the file")
		}
	})

	t.Run("negative one size limit disables checking", func(t *testing.T) {
		result, err := ReadFile(ReadFileOptions{
			Path:             largeFile,
			AllowedDirs:      []string{tmpDir},
			SizeCheckMaxSize: -1, // No limit (-1 = disabled)
		})

		if err != nil {
			t.Fatalf("Unexpected error with size limit disabled: %v", err)
		}

		if len(result.Content) != 100000 {
			t.Errorf("Expected content length=100000, got %d", len(result.Content))
		}
	})

	t.Run("large explicit limit allows large file", func(t *testing.T) {
		result, err := ReadFile(ReadFileOptions{
			Path:             largeFile,
			AllowedDirs:      []string{tmpDir},
			SizeCheckMaxSize: 200000, // Larger than file
		})

		if err != nil {
			t.Fatalf("Unexpected error with large limit: %v", err)
		}

		if len(result.Content) != 100000 {
			t.Errorf("Expected content length=100000, got %d", len(result.Content))
		}
	})
}

func TestReadMultipleFilesSizeLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create files with known sizes
	file1Content := strings.Repeat("a", 40000) // 40KB
	file1 := filepath.Join(tmpDir, "file1.txt")
	if err := os.WriteFile(file1, []byte(file1Content), 0644); err != nil {
		t.Fatal(err)
	}

	file2Content := strings.Repeat("b", 50000) // 50KB
	file2 := filepath.Join(tmpDir, "file2.txt")
	if err := os.WriteFile(file2, []byte(file2Content), 0644); err != nil {
		t.Fatal(err)
	}

	smallFile := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(smallFile, []byte("small"), 0644); err != nil {
		t.Fatal(err)
	}

	// Rewritten from "returns error". Beyond replacing the refusal, this pins the
	// GREEDY allocation the old test could not see: the file asked for first
	// comes back whole, and the one that straddles the budget is the one cut.
	// Even division would fail this.
	t.Run("combined size over the limit truncates instead of refusing", func(t *testing.T) {
		result, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths:                 []string{file1, file2},
			AllowedDirs:           []string{tmpDir},
			SizeCheckMaxTotalSize: 0, // Use default (70000)
		})

		if err != nil {
			t.Fatalf("an over-budget set must read as a truncated success, got %v", err)
		}
		if !result.Truncated {
			t.Error("truncated flag not set on an over-budget set")
		}
		if result.TotalSize != 90000 {
			t.Errorf("total_size = %d, want the combined size 90000", result.TotalSize)
		}
		if len(result.Files) != 2 {
			t.Fatalf("files = %d, want 2", len(result.Files))
		}
		if len(result.Files[0].Content) != 40000 {
			t.Errorf("the first file requested came back cut (%d bytes); greedy must keep it whole",
				len(result.Files[0].Content))
		}
		if !result.Files[1].Truncated {
			t.Error("the file straddling the budget was not marked truncated")
		}
		if result.Files[1].TotalSize != 50000 {
			t.Errorf("second file total_size = %d, want 50000", result.Files[1].TotalSize)
		}

		var cost int64
		for _, f := range result.Files {
			cost += int64(EstimateJSONStringSize(f.Content))
		}
		if cost > DefaultMaxSize {
			t.Errorf("combined content costs %d chars, over the %d budget", cost, DefaultMaxSize)
		}
	})

	t.Run("combined size within limit succeeds", func(t *testing.T) {
		result, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths:                 []string{smallFile},
			AllowedDirs:           []string{tmpDir},
			SizeCheckMaxTotalSize: 0, // Use default
		})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result.Success != 1 {
			t.Errorf("Expected success=1, got %d", result.Success)
		}
	})

	t.Run("custom total size limit truncates to that budget", func(t *testing.T) {
		result, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths:                 []string{file1},
			AllowedDirs:           []string{tmpDir},
			SizeCheckMaxTotalSize: 30000, // smaller than the 40KB file
		})

		if err != nil {
			t.Fatalf("a custom budget must truncate, not refuse: %v", err)
		}
		if !result.Truncated {
			t.Error("truncated flag not set under a custom total budget")
		}
		if len(result.Files) != 1 {
			t.Fatalf("files = %d, want 1", len(result.Files))
		}
		if est := int64(EstimateJSONStringSize(result.Files[0].Content)); est > 30000 {
			t.Errorf("content costs %d chars, over the budget of 30000", est)
		}
		if result.Files[0].TotalSize != 40000 {
			t.Errorf("total_size = %d, want the real file size 40000", result.Files[0].TotalSize)
		}
	})

	t.Run("negative one total size limit disables checking", func(t *testing.T) {
		result, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths:                 []string{file1, file2},
			AllowedDirs:           []string{tmpDir},
			SizeCheckMaxTotalSize: -1, // No limit
		})

		if err != nil {
			t.Fatalf("Unexpected error with size limit disabled: %v", err)
		}

		if result.Success != 2 {
			t.Errorf("Expected success=2, got %d", result.Success)
		}
	})

	t.Run("handles missing files in size check gracefully", func(t *testing.T) {
		_, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths:                 []string{smallFile, filepath.Join(tmpDir, "nonexistent.txt")},
			AllowedDirs:           []string{tmpDir},
			SizeCheckMaxTotalSize: 0, // Use default
		})

		// Should not error at size check stage - missing files are handled during read
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})

	t.Run("handles directories in file list gracefully", func(t *testing.T) {
		subDir := filepath.Join(tmpDir, "subdir")
		os.Mkdir(subDir, 0755)

		_, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths:                 []string{smallFile, subDir},
			AllowedDirs:           []string{tmpDir},
			SizeCheckMaxTotalSize: 0, // Use default
		})

		// Should not error at size check stage - directories are handled during read
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})
}

// The resume command the help block advertises had NO test at all, and it was
// broken in the way that matters least visibly and most expensively: with no
// explicit --max-size it reached readFileByBytes with no byte limit, which read
// the entire file with os.ReadFile and then sliced. A 300MB file peaked at
// 609MB of resident memory; a 1GB file at over 2GB. The agent was told to run
// that command by the tool itself.
//
// Reading the whole file in chunks must reconstruct it exactly, or next_offset
// silently skips or repeats bytes.
func TestResumeByOffsetReconstructsTheFile(t *testing.T) {
	tmpDir := t.TempDir()
	// Line-structured and comfortably over the default budget, so more than one
	// resume is required.
	body := strings.Repeat("the quick brown fox jumps\n", 8000) // ~208KB
	path := filepath.Join(tmpDir, "resume.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	var rebuilt strings.Builder
	offset := 0
	for i := 0; ; i++ {
		if i > 20 {
			t.Fatal("resume did not terminate; next_offset is not advancing")
		}

		res, err := ReadFile(ReadFileOptions{
			Path:        path,
			StartOffset: offset,
			AllowedDirs: []string{tmpDir},
		})
		if err != nil {
			t.Fatalf("read at offset %d: %v", offset, err)
		}
		if len(res.Content) == 0 {
			t.Fatalf("read at offset %d returned nothing while %d bytes remained",
				offset, len(body)-offset)
		}

		rebuilt.WriteString(res.Content)

		if !res.Truncated {
			break
		}
		if res.TotalSize != int64(len(body)) {
			t.Errorf("total_size = %d, want %d", res.TotalSize, len(body))
		}
		if res.NextOffset != int64(offset+len(res.Content)) {
			t.Fatalf("next_offset = %d, want %d", res.NextOffset, offset+len(res.Content))
		}
		offset = int(res.NextOffset)
	}

	if rebuilt.String() != body {
		t.Errorf("resumed read reconstructed %d bytes, want %d; next_offset skipped or repeated content",
			rebuilt.Len(), len(body))
	}
}

// A resume that reaches the end of the file must NOT claim to be truncated, or
// an agent loops forever re-reading the tail.
func TestResumeAtTheTailIsNotTruncated(t *testing.T) {
	tmpDir := t.TempDir()
	body := "short enough to fit\n"
	path := filepath.Join(tmpDir, "tail.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := ReadFile(ReadFileOptions{
		Path:        path,
		StartOffset: 10,
		AllowedDirs: []string{tmpDir},
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Truncated {
		t.Error("a read that reached EOF reported itself truncated")
	}
	if res.Content != body[10:] {
		t.Errorf("content = %q, want %q", res.Content, body[10:])
	}
}

// fitToBudget is the subtlest part of the truncation path and the part no
// caller sees directly, so it is tested here rather than only through a read.
//
// The read-level tests above reach only its early return: their fixture is a run
// of 'a' whose JSON cost equals its byte length, so nothing is ever escaped and
// the shrink loop never runs. Every case below exists because a read fixture
// cannot reach it.
func TestFitToBudget(t *testing.T) {
	t.Run("content within budget is returned untouched", func(t *testing.T) {
		s := "short enough\n"
		got, cut := fitToBudget(s, 1000)

		if cut {
			t.Error("reported a cut for content that fits")
		}
		if got != s {
			t.Errorf("got %q, want the input unchanged", got)
		}
	})

	t.Run("a budget of -1 never cuts", func(t *testing.T) {
		s := strings.Repeat("x", 5000)
		got, cut := fitToBudget(s, -1)

		if cut || got != s {
			t.Error("-1 means no limit, so nothing may be dropped")
		}
	})

	t.Run("cuts on a line boundary when one exists", func(t *testing.T) {
		s := strings.Repeat("0123456789\n", 100)
		got, cut := fitToBudget(s, 500)

		if !cut {
			t.Fatal("1200 chars of cost must be cut to a 500 budget")
		}
		if !strings.HasSuffix(got, "\n") {
			t.Errorf("cut did not land on a line boundary, so next_offset would resume mid-line: %q", got)
		}
		if !strings.HasPrefix(s, got) {
			t.Error("result is not a prefix of the input")
		}
		if est := int64(EstimateJSONStringSize(got)); est > 500 {
			t.Errorf("kept content costs %d, over the budget of 500", est)
		}
	})

	t.Run("measures encoded cost, not raw bytes", func(t *testing.T) {
		// A quote is one byte raw and two characters encoded, so a byte-based
		// cut would keep twice as much as fits.
		s := strings.Repeat(`"`, 1000)
		got, cut := fitToBudget(s, 1000)

		if !cut {
			t.Fatal("2000 chars of cost must be cut to a 1000 budget")
		}
		if est := int64(EstimateJSONStringSize(got)); est > 1000 {
			t.Errorf("kept content costs %d, over the budget of 1000", est)
		}
		if len(got) >= 1000 {
			t.Errorf("kept %d bytes, which means the cut was by byte count rather than encoded cost", len(got))
		}
	})

	t.Run("never splits a multibyte rune", func(t *testing.T) {
		s := strings.Repeat("é", 500) // two bytes each, no line boundary
		got, cut := fitToBudget(s, 401)

		if !cut {
			t.Fatal("1000 chars of cost must be cut to a 401 budget")
		}
		if !utf8.ValidString(got) {
			t.Error("the cut produced invalid UTF-8")
		}
		if !strings.HasPrefix(s, got) {
			t.Error("result is not a prefix of the input")
		}
	})

	// The rune-boundary fallback strips while DecodeLastRuneInString reports
	// RuneError with size 1 — which EVERY byte of an invalid-UTF-8 prefix does.
	// So a binary file was stripped to nothing, and ReadFile then answered with
	// empty content, truncated: true, and a next_offset equal to the offset it
	// was given. An agent following that resume never terminates, and every
	// iteration exits 0.
	t.Run("invalid UTF-8 is never stripped to nothing", func(t *testing.T) {
		s := strings.Repeat("\xff", 100000)

		got, cut := fitToBudget(s, 70000)

		if !cut {
			t.Fatal("a 100000-byte string must be cut to a 70000 budget")
		}
		if got == "" {
			t.Fatal("stripped the entire prefix; a resume from here cannot advance")
		}
		if est := int64(EstimateJSONStringSize(got)); est > 70000 {
			t.Errorf("kept content costs %d, over the budget of 70000", est)
		}
		if !strings.HasPrefix(s, got) {
			t.Error("result is not a prefix of the input")
		}
	})

	// A tail of invalid bytes must cost at most a rune's worth, not the budget.
	t.Run("a partly invalid tail loses at most a few bytes", func(t *testing.T) {
		s := strings.Repeat("a", 69990) + strings.Repeat("\xff", 100)

		got, cut := fitToBudget(s, 70000)

		if !cut {
			t.Skip("fixture fits the budget; nothing to assert")
		}
		if len(got) < 60000 {
			t.Errorf("kept only %d bytes of a 70000 budget; the invalid tail ate the whole prefix", len(got))
		}
	})

	t.Run("a single line longer than the budget still yields a usable prefix", func(t *testing.T) {
		s := strings.Repeat("x", 5000) // no newline anywhere
		got, cut := fitToBudget(s, 100)

		if !cut {
			t.Fatal("expected a cut")
		}
		if got == "" {
			t.Error("returned nothing, though a prefix was available")
		}
		if est := int64(EstimateJSONStringSize(got)); est > 100 {
			t.Errorf("kept content costs %d, over the budget of 100", est)
		}
	})
}

func TestDefaultMaxSize(t *testing.T) {
	if DefaultMaxSize != 70000 {
		t.Errorf("Expected DefaultMaxSize=70000, got %d", DefaultMaxSize)
	}
}

func TestReadFileValidation(t *testing.T) {
	t.Run("empty path returns error", func(t *testing.T) {
		_, err := ReadFile(ReadFileOptions{
			Path: "",
		})
		if err == nil {
			t.Error("Expected error for empty path")
		}
	})
}

func TestReadMultipleFilesValidation(t *testing.T) {
	t.Run("empty paths returns error", func(t *testing.T) {
		_, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths: []string{},
		})
		if err == nil {
			t.Error("Expected error for empty paths")
		}
	})

	t.Run("nil paths returns error", func(t *testing.T) {
		_, err := ReadMultipleFiles(ReadMultipleFilesOptions{
			Paths: nil,
		})
		if err == nil {
			t.Error("Expected error for nil paths")
		}
	})
}

func TestReadFileByLines(t *testing.T) {
	tmpDir := t.TempDir()

	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	testFile := filepath.Join(tmpDir, "lines.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("read from specific line", func(t *testing.T) {
		result, err := ReadFile(ReadFileOptions{
			Path:             testFile,
			AllowedDirs:      []string{tmpDir},
			LineStart:        2,
			LineCount:        2,
			SizeCheckMaxSize: 0,
		})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !strings.Contains(result.Content, "line 2") {
			t.Error("Expected content to contain 'line 2'")
		}
		if !strings.Contains(result.Content, "line 3") {
			t.Error("Expected content to contain 'line 3'")
		}
		if strings.Contains(result.Content, "line 1") {
			t.Error("Content should not contain 'line 1'")
		}
	})
}

func TestExtractLinesBasic(t *testing.T) {
	tmpDir := t.TempDir()

	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	testFile := filepath.Join(tmpDir, "extract.txt")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("extract by line numbers", func(t *testing.T) {
		result, err := ExtractLines(ExtractLinesOptions{
			Path:        testFile,
			AllowedDirs: []string{tmpDir},
			LineNumbers: []int{1, 3, 5},
		})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !strings.Contains(result.Content, "line 1") {
			t.Error("Expected content to contain 'line 1'")
		}
		if !strings.Contains(result.Content, "line 3") {
			t.Error("Expected content to contain 'line 3'")
		}
		if !strings.Contains(result.Content, "line 5") {
			t.Error("Expected content to contain 'line 5'")
		}
	})

	t.Run("extract by range", func(t *testing.T) {
		result, err := ExtractLines(ExtractLinesOptions{
			Path:        testFile,
			AllowedDirs: []string{tmpDir},
			StartLine:   2,
			EndLine:     4,
		})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !strings.Contains(result.Content, "line 2") {
			t.Error("Expected content to contain 'line 2'")
		}
		if !strings.Contains(result.Content, "line 4") {
			t.Error("Expected content to contain 'line 4'")
		}
	})

	t.Run("extract by pattern", func(t *testing.T) {
		result, err := ExtractLines(ExtractLinesOptions{
			Path:        testFile,
			AllowedDirs: []string{tmpDir},
			Pattern:     "line 3",
		})

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !strings.Contains(result.Content, "line 3") {
			t.Error("Expected content to contain 'line 3'")
		}
	})

	t.Run("empty path returns error", func(t *testing.T) {
		_, err := ExtractLines(ExtractLinesOptions{
			Path: "",
		})
		if err == nil {
			t.Error("Expected error for empty path")
		}
	})
}
