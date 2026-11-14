package core

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileSearchResult represents a found file
type FileSearchResult struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	ModTime string `json:"mod_time"`
}

// SearchFilesOptions contains input parameters for SearchFiles
type SearchFilesOptions struct {
	Path        string
	Pattern     string
	Recursive   bool
	ShowHidden  bool
	MaxResults  int
	AllowedDirs []string
}

// SearchFilesResult represents the result of a file search
type SearchFilesResult struct {
	Pattern string             `json:"pattern"`
	Path    string             `json:"path"`
	Matches []FileSearchResult `json:"matches"`
	// Total counts every match, including those past MaxResults that were
	// counted but not stored. Shown and Truncated are set only when the cap
	// cut results, so an uncut search carries no marker.
	Total     int  `json:"total"`
	Shown     int  `json:"shown,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// SearchFiles searches for files by name pattern
func SearchFiles(opts SearchFilesOptions) (*SearchFilesResult, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if opts.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// NormalizePath only expands ~, cleans and absolutizes; ValidatePath answers
	// "is this inside the sandbox", not "is this there". Without this, a missing
	// path walked nothing and returned a clean zero — and "I found nothing" and
	// "that directory does not exist" are answers an agent must tell apart.
	if info, statErr := os.Stat(normalizedPath); statErr != nil {
		return nil, fmt.Errorf("search path is not readable: %w", statErr)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("search path is not a directory: %s", normalizedPath)
	}

	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 1000
	}

	var matches []FileSearchResult
	total := 0

	walkFn := func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files with errors
		}

		name := info.Name()

		// Skip hidden unless requested
		if !opts.ShowHidden && strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip directories in non-recursive mode
		if !opts.Recursive && info.IsDir() && filePath != normalizedPath {
			return filepath.SkipDir
		}

		// Match pattern against filename
		matched, _ := filepath.Match(opts.Pattern, name)
		if !matched {
			return nil
		}

		// The cap limits what is stored, not what is counted: stopping here
		// once reported the cap itself as the total (epic 10 T4).
		total++
		if len(matches) >= maxResults {
			return nil
		}

		matches = append(matches, FileSearchResult{
			Path:    filePath,
			Name:    name,
			Size:    info.Size(),
			IsDir:   info.IsDir(),
			ModTime: info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
		})

		return nil
	}

	if err := filepath.Walk(normalizedPath, walkFn); err != nil {
		return nil, fmt.Errorf("failed to search files: %w", err)
	}

	result := &SearchFilesResult{
		Pattern: opts.Pattern,
		Path:    normalizedPath,
		Matches: matches,
		Total:   total,
	}
	if total > len(matches) {
		result.Shown = len(matches)
		result.Truncated = true
	}
	return result, nil
}

// CodeMatch represents a single code match
type CodeMatch struct {
	File    string   `json:"file"`
	Line    int      `json:"line"`
	Content string   `json:"content"`
	Context []string `json:"context,omitempty"`
}

// SearchCodeOptions contains input parameters for SearchCode
type SearchCodeOptions struct {
	Path            string
	Pattern         string
	CaseInsensitive bool
	Regex           bool
	ContextLines    int
	FileTypes       []string
	MaxResults      int
	ShowHidden      bool
	AllowedDirs     []string
}

// SearchCodeResult represents the result of a code search
type SearchCodeResult struct {
	Pattern string      `json:"pattern"`
	Path    string      `json:"path"`
	Matches []CodeMatch `json:"matches"`
	// TotalFiles and TotalMatches count everything that matched, including
	// matches past MaxResults that were counted but not stored. Shown and
	// Truncated are set only when the cap cut results.
	TotalFiles   int  `json:"total_files"`
	TotalMatches int  `json:"total_matches"`
	Shown        int  `json:"shown,omitempty"`
	Truncated    bool `json:"truncated,omitempty"`
}

// SearchCode searches for patterns in file contents
func SearchCode(opts SearchCodeOptions) (*SearchCodeResult, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if opts.Pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// NormalizePath only expands ~, cleans and absolutizes; ValidatePath answers
	// "is this inside the sandbox", not "is this there". Without this, a missing
	// path walked nothing and returned a clean zero — and "I found nothing" and
	// "that directory does not exist" are answers an agent must tell apart.
	if info, statErr := os.Stat(normalizedPath); statErr != nil {
		return nil, fmt.Errorf("search path is not readable: %w", statErr)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("search path is not a directory: %s", normalizedPath)
	}

	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 1000
	}

	// Prepare search pattern
	var searchFn func(line string) bool

	if opts.Regex {
		flags := ""
		if opts.CaseInsensitive {
			flags = "(?i)"
		}
		re, err := regexp.Compile(flags + opts.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %w", err)
		}
		searchFn = func(line string) bool {
			return re.MatchString(line)
		}
	} else {
		searchPattern := opts.Pattern
		if opts.CaseInsensitive {
			searchPattern = strings.ToLower(opts.Pattern)
			searchFn = func(line string) bool {
				return strings.Contains(strings.ToLower(line), searchPattern)
			}
		} else {
			searchFn = func(line string) bool {
				return strings.Contains(line, opts.Pattern)
			}
		}
	}

	var matches []CodeMatch
	totalMatches, totalFiles := 0, 0

	walkFn := func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		name := info.Name()

		// Skip hidden unless requested
		if !opts.ShowHidden && strings.HasPrefix(name, ".") {
			return nil
		}

		// Check file type filter
		if len(opts.FileTypes) > 0 {
			ext := filepath.Ext(name)
			matched := false
			for _, ft := range opts.FileTypes {
				if ext == ft || "."+ext == ft || ext == "."+ft {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
		}

		// Skip binary files (simple heuristic)
		if isBinaryFile(filePath) {
			return nil
		}

		// Search in file. The cap limits what is stored, not what is counted:
		// stopping at it once reported the cap itself as total_matches (epic
		// 10 T4), so files past it are still read, only to count.
		fileMatches, count, err := searchInFile(filePath, searchFn, opts.ContextLines, maxResults-len(matches))
		if err != nil {
			return nil // Skip files that can't be read
		}

		for _, m := range fileMatches {
			m.File = filePath
			matches = append(matches, m)
		}
		totalMatches += count
		if count > 0 {
			totalFiles++
		}

		return nil
	}

	if err := filepath.Walk(normalizedPath, walkFn); err != nil {
		return nil, fmt.Errorf("failed to search code: %w", err)
	}

	result := &SearchCodeResult{
		Pattern:      opts.Pattern,
		Path:         normalizedPath,
		Matches:      matches,
		TotalFiles:   totalFiles,
		TotalMatches: totalMatches,
	}
	if totalMatches > len(matches) {
		result.Shown = len(matches)
		result.Truncated = true
	}
	return result, nil
}

// searchInFile returns at most keep matches and the count of every match in the
// file. Matches past keep are counted but never built, so a search past its cap
// costs a scan, not memory.
func searchInFile(filePath string, matchFn func(string) bool, contextLines int, keep int) ([]CodeMatch, int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	var matches []CodeMatch
	count := 0
	scanner := bufio.NewScanner(file)

	// Read all lines only if context is needed for a match that will be kept
	if contextLines > 0 && keep > 0 {
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return nil, 0, err
		}

		for i, line := range lines {
			if !matchFn(line) {
				continue
			}
			count++
			if len(matches) >= keep {
				continue
			}
			start := max(0, i-contextLines)
			end := min(len(lines), i+contextLines+1)
			context := make([]string, 0, end-start)
			for j := start; j < end; j++ {
				context = append(context, lines[j])
			}
			matches = append(matches, CodeMatch{
				Line:    i + 1,
				Content: line,
				Context: context,
			})
		}
	} else {
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if !matchFn(line) {
				continue
			}
			count++
			if len(matches) < keep {
				matches = append(matches, CodeMatch{
					Line:    lineNum,
					Content: line,
				})
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, 0, err
		}
	}

	return matches, count, nil
}

func isBinaryFile(path string) bool {
	// Check common binary extensions
	ext := strings.ToLower(filepath.Ext(path))
	binaryExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".bin": true, ".dat": true, ".db": true, ".sqlite": true,
		".zip": true, ".tar": true, ".gz": true, ".bz2": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".pdf": true, ".doc": true, ".docx": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
	}
	if binaryExts[ext] {
		return true
	}

	// Quick check of file content (first 512 bytes)
	file, err := os.Open(path)
	if err != nil {
		return true // Assume binary if can't read
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil || n == 0 {
		return true
	}

	// Check for null bytes (common in binary files)
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true
		}
	}

	return false
}
