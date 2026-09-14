package core

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

// DefaultMaxSize is the default maximum JSON output size in characters (70K)
// This accounts for Claude's ~75K character limit on tool responses
const DefaultMaxSize = 70000

// EstimateJSONStringSize estimates the size of a string after JSON encoding
// JSON escapes: \n, \t, \r, ", \, and control characters
func EstimateJSONStringSize(s string) int {
	size := len(s)
	for _, c := range s {
		switch c {
		case '"', '\\':
			size++ // These become \" or \\
		case '\n', '\t', '\r':
			size++ // These become \n, \t, \r
		default:
			// Control characters (< 0x20) become \uXXXX (6 chars)
			if c < 0x20 {
				size += 5 // Original char counts as 1, \uXXXX is 6, so add 5
			}
		}
	}
	return size
}

// SizeExceededError, TotalSizeExceededError and FileSizeEntry lived here.
//
// Reads truncate rather than refuse (AXI principle 3), so size stopped being an
// error class and the types lost their last caller. They were deleted with the
// behaviour they described rather than left behind as vestigial API.

// ReadFileOptions contains input parameters for ReadFile
type ReadFileOptions struct {
	Path             string
	StartOffset      int
	MaxSize          int
	LineStart        int
	LineCount        int
	AllowedDirs      []string
	SizeCheckMaxSize int64 // Maximum allowed JSON output size (0 = use default, -1 = no limit, >0 = custom)
}

// ReadFileResult represents the result of a file read operation.
//
// Size keeps its original meaning — bytes actually returned — so nothing shifts
// under an existing consumer. TotalSize is the file's real size, and the gap
// between the two is the size hint AXI principle 3 asks for. NextOffset is the
// exact --start-offset that resumes where this read stopped, so the agent does
// no arithmetic. All three are omitempty: their absence means a complete read.
type ReadFileResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Size       int64  `json:"size"`
	Lines      int    `json:"lines,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	TotalSize  int64  `json:"total_size,omitempty"`
	NextOffset int64  `json:"next_offset,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ReadFile reads a file with optional line range or byte offset
func ReadFile(opts ReadFileOptions) (*ReadFileResult, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Resolve symlinks and normalize path
	path := opts.Path
	resolved, err := ResolveSymlink(path)
	if err == nil {
		path = resolved
	}

	// Validate path against allowed directories
	if err := ValidatePath(path, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Get file info
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}

	// Determine max size limit (0 = use default, -1 = no limit, >0 = custom)
	maxSize := opts.SizeCheckMaxSize
	if maxSize == 0 {
		maxSize = DefaultMaxSize
	}
	// maxSize is now: DefaultMaxSize, -1 (no limit), or custom value

	totalSize := info.Size()

	// An over-budget file used to be REFUSED here. AXI principle 3 asks for a
	// truncated read with a size hint instead: a refusal tells an agent nothing
	// about the file, while a prefix plus total_size tells it everything it
	// needs to decide what to do next.
	//
	// Bounding the read itself is load-bearing, not tidiness. Reading a 5GB file
	// whole and then slicing it would exhaust the agent's own process, which is
	// the one thing the old fail-fast pre-check got right. Only the whole-file
	// path needs it — a caller-supplied line range or byte window is bounded
	// already.
	lineRangeRead := opts.LineStart > 0 || opts.LineCount > 0
	byteWindowRead := !lineRangeRead && (opts.StartOffset > 0 || opts.MaxSize > 0)

	// Bytes to read on the byte-oriented paths. An explicit --max-size wins;
	// otherwise the size budget bounds the read. This is what stops the resume
	// command from loading the whole file: --start-offset previously arrived
	// here with no byte limit at all.
	//
	// JSON escaping only ever grows a string, so maxSize BYTES is a safe upper
	// bound on what fits in maxSize CHARS; fitToBudget trims the remainder when
	// escaping pushes it back over.
	byteBudget := opts.MaxSize
	if byteBudget <= 0 && maxSize > 0 {
		byteBudget = int(maxSize)
	}

	var content string
	var lines int

	switch {
	case lineRangeRead:
		content, lines, err = readFileByLines(path, opts.LineStart, opts.LineCount)
	case byteWindowRead:
		content, err = readFileByBytes(path, opts.StartOffset, byteBudget)
		lines = strings.Count(content, "\n")
	case maxSize > 0 && totalSize > maxSize:
		content, err = readFileByBytes(path, 0, int(maxSize))
		lines = strings.Count(content, "\n")
	default:
		content, lines, err = readEntireFile(path)
	}

	if err != nil {
		return nil, err
	}

	// Fit to the estimated-JSON budget, which catches escape-heavy content that
	// fits in bytes but not in encoded characters.
	if maxSize > 0 {
		if kept, cut := fitToBudget(content, maxSize); cut {
			content = kept
			lines = strings.Count(content, "\n")
		}
	}

	// Truncated means bytes remain past what was returned. Derived from the file
	// rather than from which branch ran, so a resume that stops short is flagged
	// exactly like a first read that does. A line-range read is excluded: its
	// window is the caller's own, and its end is not a byte offset.
	truncated := !lineRangeRead &&
		int64(opts.StartOffset)+int64(len(content)) < totalSize

	res := &ReadFileResult{
		Path:    path,
		Content: content,
		Size:    int64(len(content)),
		Lines:   lines,
	}

	if truncated {
		res.Truncated = true
		res.TotalSize = totalSize
		// A byte offset into the file, so it resumes exactly where this read
		// stopped — including when this read was itself a resume.
		res.NextOffset = int64(opts.StartOffset) + int64(len(content))
	}

	return res, nil
}

// fitToBudget returns the largest prefix of s whose estimated JSON size is
// within maxSize, and whether anything was dropped.
//
// The budget is measured in EstimateJSONStringSize units but spent on output
// that may be TOON, which escapes less than JSON. The estimate is therefore
// conservative — it can return slightly less than would have fit, never more.
// That is the safe direction: overshooting the limit is what breaks a consumer.
//
// The cut prefers a line boundary, so NextOffset lands at the start of a line
// and the advertised --start-offset resume reads cleanly. Falling back to a rune
// boundary keeps a minified or single-line file from being cut mid-character.
func fitToBudget(s string, maxSize int64) (string, bool) {
	if maxSize <= 0 || int64(EstimateJSONStringSize(s)) <= maxSize {
		return s, false
	}

	// Shrink by the ratio of budget to estimated cost. Escaping never costs less
	// than one byte per byte, so this converges in a pass or two rather than
	// walking backwards a byte at a time.
	kept := s
	for len(kept) > 0 && int64(EstimateJSONStringSize(kept)) > maxSize {
		est := int64(EstimateJSONStringSize(kept))
		n := int(int64(len(kept)) * maxSize / est)
		if n >= len(kept) {
			n = len(kept) - 1
		}
		if n < 0 {
			n = 0
		}
		kept = kept[:n]
	}

	if i := strings.LastIndexByte(kept, '\n'); i >= 0 {
		return kept[:i+1], true
	}

	// No line boundary available: back off to a whole rune, but by at most
	// UTFMax-1 bytes. A rune is never longer than that, so needing more means
	// the content simply is not UTF-8.
	//
	// Unbounded, this consumed the ENTIRE prefix of a binary file: every byte of
	// invalid UTF-8 decodes as RuneError with size 1, so the loop stripped until
	// nothing was left. ReadFile then answered with empty content, truncated
	// true, and a next_offset equal to the offset it was given — a resume that
	// cannot advance, which an agent following the help block repeats forever
	// while every iteration exits 0.
	byteCut := kept
	for i := 0; i < utf8.UTFMax-1 && len(kept) > 0; i++ {
		r, size := utf8.DecodeLastRuneInString(kept)
		if r != utf8.RuneError || size > 1 {
			return kept, true
		}
		kept = kept[:len(kept)-1]
	}

	if kept == "" {
		// Not UTF-8 at all. The byte cut is the honest answer: a prefix that
		// advances beats a correct-looking nothing.
		return byteCut, true
	}
	return kept, true
}

func readEntireFile(path string) (string, int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read file: %w", err)
	}
	lines := strings.Count(string(content), "\n")
	return string(content), lines, nil
}

func readFileByLines(path string, startLine, lineCount int) (string, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var result strings.Builder
	scanner := bufio.NewScanner(file)
	currentLine := 0
	linesRead := 0

	// Handle 1-indexed line numbers
	if startLine < 1 {
		startLine = 1
	}

	for scanner.Scan() {
		currentLine++
		if currentLine >= startLine {
			if lineCount > 0 && linesRead >= lineCount {
				break
			}
			result.WriteString(scanner.Text())
			result.WriteString("\n")
			linesRead++
		}
	}

	if err := scanner.Err(); err != nil {
		return "", 0, fmt.Errorf("error reading file: %w", err)
	}

	return result.String(), linesRead, nil
}

// readFileByBytes returns up to maxSize bytes starting at startOffset, or
// everything from startOffset when maxSize is not positive.
//
// It seeks and streams. The previous version did two things that made the
// advertised resume command dangerous: with no maxSize it called os.ReadFile on
// the WHOLE file and then sliced, so `--start-offset N` on a 300MB file peaked
// at 609MB of RSS — on a 1GB file, over 2GB. And with a maxSize it took a single
// file.Read, which is permitted to return fewer bytes than asked for, so a short
// read silently truncated more than the budget required.
func readFileByBytes(path string, startOffset, maxSize int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	if startOffset > 0 {
		if _, err := file.Seek(int64(startOffset), io.SeekStart); err != nil {
			return "", fmt.Errorf("failed to seek: %w", err)
		}
	}

	var r io.Reader = file
	if maxSize > 0 {
		r = io.LimitReader(file, int64(maxSize))
	}

	buffer, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read: %w", err)
	}
	return string(buffer), nil
}

// ReadMultipleFilesOptions contains input parameters for ReadMultipleFiles
type ReadMultipleFilesOptions struct {
	Paths                 []string
	AllowedDirs           []string
	SizeCheckMaxTotalSize int64 // Maximum allowed total JSON output size (0 = use default, -1 = no limit, >0 = custom)
}

// ReadMultipleFilesResult represents results from reading multiple files.
//
// Truncated means at least one file was cut or not read at all; TotalSize is the
// combined size on disk. Both are omitempty, so their absence means every
// requested file came back whole.
// Skipped counts files that were named but never opened because the budget was
// already spent. They are neither a success nor a failure, and without a counter
// of their own success+failed summed to less than the number of files — so a
// caller checking failed == 0 concluded everything had been read.
type ReadMultipleFilesResult struct {
	Files     []ReadFileResult `json:"files"`
	Success   int              `json:"success"`
	Failed    int              `json:"failed"`
	Skipped   int              `json:"skipped,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
	TotalSize int64            `json:"total_size,omitempty"`
}

// ReadMultipleFiles reads multiple files concurrently
func ReadMultipleFiles(opts ReadMultipleFilesOptions) (*ReadMultipleFilesResult, error) {
	if len(opts.Paths) == 0 {
		return nil, fmt.Errorf("paths is required")
	}

	// Determine max total size (0 = use default, -1 = no limit, >0 = custom)
	maxTotalSize := opts.SizeCheckMaxTotalSize
	if maxTotalSize == 0 {
		maxTotalSize = DefaultMaxSize
	}
	// maxTotalSize is now: DefaultMaxSize, -1 (no limit), or custom value

	// Resolve once, so the budget pass and the read pass agree on paths.
	paths := make([]string, len(opts.Paths))
	for i, p := range opts.Paths {
		if resolved, rerr := ResolveSymlink(p); rerr == nil && resolved != "" {
			paths[i] = resolved
			continue
		}
		paths[i] = p
	}

	// GREEDY budget allocation, replacing a refusal.
	//
	// Walking in order and giving each file everything it needs keeps the files
	// the caller asked for FIRST whole, which is far more useful than N evenly
	// shrunken fragments. A file past the budget is never opened at all, so this
	// is also faster than the old read-everything-then-refuse.
	//
	// budget: -1 means no limit, 0 means do not read, >0 is a character budget.
	// It is never passed as 0 to ReadFile, where 0 would mean "use the default".
	budgets := make([]int64, len(paths))
	sizes := make([]int64, len(paths))
	remaining := maxTotalSize
	var totalOnDisk int64

	for i, p := range paths {
		info, serr := os.Stat(p)
		if serr != nil || info.IsDir() {
			// Not a readable file. Let the read pass report why, rather than
			// guessing at a diagnosis from here.
			budgets[i] = -1
			continue
		}
		sizes[i] = info.Size()
		totalOnDisk += info.Size()

		if maxTotalSize < 0 {
			budgets[i] = -1
			continue
		}
		if remaining <= 0 {
			budgets[i] = 0
			continue
		}

		budgets[i] = remaining
		if info.Size() < remaining {
			remaining -= info.Size()
		} else {
			remaining = 0
		}
	}

	results := make([]ReadFileResult, len(paths))
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	failed := 0
	skipped := 0

	for i, p := range paths {
		if budgets[i] == 0 {
			skipped++
			// Named and sized, but deliberately not read. Neither a success nor
			// a failure: it was never attempted, and truncated says so. The size
			// comes from the budget pass rather than a second stat of a file
			// this branch has already decided not to open.
			results[i] = ReadFileResult{Path: p, Truncated: true, TotalSize: sizes[i]}
			continue
		}

		wg.Add(1)
		go func(idx int, filePath string, budget int64) {
			defer wg.Done()

			// ReadFile already validates, truncates and reports total_size and
			// next_offset per file, so every guarantee AC8 makes for one file
			// holds for each file here without a second implementation.
			r, rerr := ReadFile(ReadFileOptions{
				Path:             filePath,
				AllowedDirs:      opts.AllowedDirs,
				SizeCheckMaxSize: budget,
			})

			mu.Lock()
			defer mu.Unlock()

			if rerr != nil {
				results[idx] = ReadFileResult{Path: filePath, Error: rerr.Error()}
				failed++
				return
			}
			results[idx] = *r
			success++
		}(i, p, budgets[i])
	}

	wg.Wait()

	// Enforce the budget in ENCODED characters, which is the unit it is
	// expressed in.
	//
	// The upfront allocation charges raw bytes, because a stat is all it has to
	// go on. Escape-heavy content costs several times its size once encoded — a
	// control byte is one raw byte and six encoded characters — so eight small
	// files overran the cap more than threefold while the last two got nothing.
	// This pass spends the real cost in request order, so the files asked for
	// first still win.
	if maxTotalSize > 0 {
		var spent int64
		for i := range results {
			if results[i].Error != "" {
				continue
			}

			cost := int64(EstimateJSONStringSize(results[i].Content))
			if spent+cost <= maxTotalSize {
				spent += cost
				continue
			}

			originalSize := results[i].Size
			left := maxTotalSize - spent
			kept := ""
			if left > 0 {
				kept, _ = fitToBudget(results[i].Content, left)
			}

			if len(kept) < len(results[i].Content) {
				if results[i].TotalSize == 0 {
					results[i].TotalSize = originalSize
				}
				results[i].Content = kept
				results[i].Size = int64(len(kept))
				results[i].Lines = strings.Count(kept, "\n")
				results[i].NextOffset = int64(len(kept))
				results[i].Truncated = true
			}
			spent = maxTotalSize
		}
	}

	// Computed after the goroutines join, so it needs no lock of its own.
	truncated := false
	for _, r := range results {
		if r.Truncated {
			truncated = true
			break
		}
	}

	out := &ReadMultipleFilesResult{
		Files:   results,
		Success: success,
		Failed:  failed,
		Skipped: skipped,
	}
	if truncated {
		out.Truncated = true
		out.TotalSize = totalOnDisk
	}
	return out, nil
}

// ExtractLinesOptions contains input parameters for ExtractLines
type ExtractLinesOptions struct {
	Path         string
	LineNumbers  []int
	StartLine    int
	EndLine      int
	Pattern      string
	ContextLines int
	AllowedDirs  []string
}

// ExtractLinesResult represents the result of extracting lines
type ExtractLinesResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Lines   int    `json:"lines"`
}

// ExtractLines extracts specific lines from a file
func ExtractLines(opts ExtractLinesOptions) (*ExtractLinesResult, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Validate path
	path := opts.Path
	resolved, _ := ResolveSymlink(path)
	if resolved != "" {
		path = resolved
	}
	if err := ValidatePath(path, opts.AllowedDirs); err != nil {
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var result strings.Builder
	scanner := bufio.NewScanner(file)
	currentLine := 0
	var allLines []string

	// Read all lines first if we need context or pattern matching
	if opts.Pattern != "" || opts.ContextLines > 0 {
		for scanner.Scan() {
			allLines = append(allLines, scanner.Text())
		}
	}

	linesExtracted := 0

	// Extract by line numbers
	if len(opts.LineNumbers) > 0 {
		lineSet := make(map[int]bool)
		for _, ln := range opts.LineNumbers {
			lineSet[ln] = true
		}

		if len(allLines) == 0 {
			file.Seek(0, 0)
			scanner = bufio.NewScanner(file)
			for scanner.Scan() {
				currentLine++
				if lineSet[currentLine] {
					result.WriteString(fmt.Sprintf("%d: %s\n", currentLine, scanner.Text()))
					linesExtracted++
				}
			}
		} else {
			for ln := range lineSet {
				if ln > 0 && ln <= len(allLines) {
					result.WriteString(fmt.Sprintf("%d: %s\n", ln, allLines[ln-1]))
					linesExtracted++
				}
			}
		}
	} else if opts.StartLine > 0 && opts.EndLine > 0 {
		// Extract by range
		if len(allLines) == 0 {
			file.Seek(0, 0)
			scanner = bufio.NewScanner(file)
			for scanner.Scan() {
				currentLine++
				if currentLine >= opts.StartLine && currentLine <= opts.EndLine {
					result.WriteString(fmt.Sprintf("%d: %s\n", currentLine, scanner.Text()))
					linesExtracted++
				}
				if currentLine > opts.EndLine {
					break
				}
			}
		} else {
			for i := opts.StartLine; i <= opts.EndLine && i <= len(allLines); i++ {
				if i > 0 {
					result.WriteString(fmt.Sprintf("%d: %s\n", i, allLines[i-1]))
					linesExtracted++
				}
			}
		}
	} else if opts.Pattern != "" {
		// Extract by pattern
		for i, line := range allLines {
			if strings.Contains(line, opts.Pattern) {
				lineNum := i + 1
				start := max(0, i-opts.ContextLines)
				end := min(len(allLines), i+opts.ContextLines+1)
				for j := start; j < end; j++ {
					result.WriteString(fmt.Sprintf("%d: %s\n", j+1, allLines[j]))
					linesExtracted++
				}
				if end < len(allLines) {
					result.WriteString(fmt.Sprintf("-- match at line %d --\n", lineNum))
				}
			}
		}
	}

	return &ExtractLinesResult{
		Path:    path,
		Content: result.String(),
		Lines:   linesExtracted,
	}, nil
}
