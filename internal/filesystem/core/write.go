package core

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/samestrin/llm-filesystem-axi/pkg/pathvalidation"
)

// WriteFileOptions contains input parameters for WriteFile
type WriteFileOptions struct {
	Path        string
	Content     string
	CreateDirs  bool
	Append      bool
	AllowedDirs []string
}

// WriteFileResult represents the result of a write operation
type WriteFileResult struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Created bool   `json:"created"`
	Backup  string `json:"backup,omitempty"`
	// InPlace is set when the write was not atomic; Message says why.
	InPlace bool   `json:"in_place,omitempty"`
	Message string `json:"message"`
}

// WriteFile writes content to a file
func WriteFile(opts WriteFileOptions) (*WriteFileResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}
	if EndsInSeparator(opts.Path) {
		return nil, usageErrorf("path %q ends in a separator, which names a directory; name a file", opts.Path)
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Create parent directories if needed
	if opts.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(normalizedPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directories: %w", err)
		}
	}

	// Determine if file exists (for created flag)
	info, err := os.Stat(normalizedPath)
	created := os.IsNotExist(err)

	if err == nil {
		if err := refuseHardLinks(normalizedPath, opts.Path, info, opts.AllowedDirs); err != nil {
			return nil, err
		}
	}

	// Set file flags based on mode
	flags := os.O_WRONLY | os.O_CREATE
	if opts.Append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	// Write file
	file, err := os.OpenFile(normalizedPath, flags, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	n, err := file.WriteString(opts.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to write: %w", err)
	}

	return &WriteFileResult{
		Path:    normalizedPath,
		Size:    int64(n),
		Created: created,
		Message: "File written successfully",
	}, nil
}

// refuseHardLinks refuses a write to a file with more than one name when
// --allowed-dirs is set. A hard link has no target to resolve, so with A
// allowed and A/hl a second name for a file outside A, the allowed-dirs check
// passed and write-file truncated or appended to the outside file in place
// (EST-8338). The other names cannot be found, so a file with more than one is
// refused rather than written through or silently unlinked. Edits write a
// linked file in place too (EST-8805), so they share this refusal. shown is
// the path as the caller gave it.
func refuseHardLinks(path, shown string, info os.FileInfo, allowedDirs []string) error {
	if len(allowedDirs) == 0 || !info.Mode().IsRegular() {
		return nil
	}
	if n := linkCount(path, info); n > 1 {
		return usageErrorf("path %q has %d hard links, and the others may be outside the allowed directories; writing would change them too", shown, n)
	}
	return nil
}

// EndsInSeparator reports whether a path ends in a separator. Such a path
// names a directory, but cleaning drops the separator, so a write to "newd/"
// created a FILE named newd (EST-8264).
func EndsInSeparator(path string) bool {
	return len(path) > 1 && os.IsPathSeparator(path[len(path)-1])
}

// WriteFileEntry represents a single file to write in a batch operation
type WriteFileEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteMultipleFilesOptions contains input parameters for WriteMultipleFiles
type WriteMultipleFilesOptions struct {
	Files       []WriteFileEntry
	AllowedDirs []string
}

// WriteMultipleFilesResult represents the result of writing multiple files
type WriteMultipleFilesResult struct {
	Files   []WriteFileResult `json:"files"`
	Success int               `json:"success"`
	Failed  int               `json:"failed"`
	Message string            `json:"message"`
	// FailedEntries numbers the entries that failed, from 1. Not serialized:
	// a failed entry's message already says why; this lets the caller name
	// the entries without guessing failure from the result's other fields.
	FailedEntries []int `json:"-"`
}

// WriteMultipleFiles writes multiple files sequentially with auto-mkdir
func WriteMultipleFiles(opts WriteMultipleFilesOptions) (*WriteMultipleFilesResult, error) {
	if len(opts.Files) == 0 {
		return nil, usageErrorf("files is required")
	}

	results := make([]WriteFileResult, len(opts.Files))
	success := 0
	failed := 0
	var failedEntries []int

	for i, entry := range opts.Files {
		result, err := WriteFile(WriteFileOptions{
			Path:        entry.Path,
			Content:     entry.Content,
			CreateDirs:  true,
			AllowedDirs: opts.AllowedDirs,
		})
		if err != nil {
			results[i] = WriteFileResult{
				Path:    entry.Path,
				Message: err.Error(),
			}
			failed++
			failedEntries = append(failedEntries, i+1)
		} else {
			results[i] = *result
			success++
		}
	}

	return &WriteMultipleFilesResult{
		Files:         results,
		Success:       success,
		Failed:        failed,
		Message:       fmt.Sprintf("Wrote %d files, %d failed", success, failed),
		FailedEntries: failedEntries,
	}, nil
}

// LargeWriteFileOptions contains input parameters for LargeWriteFile
type LargeWriteFileOptions struct {
	Path        string
	Content     string
	CreateDirs  bool
	Append      bool
	Backup      bool
	VerifyWrite bool
	AllowedDirs []string
}

// LargeWriteFile writes large files with backup and verification
func LargeWriteFile(opts LargeWriteFileOptions) (*WriteFileResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}
	if EndsInSeparator(opts.Path) {
		return nil, usageErrorf("path %q ends in a separator, which names a directory; name a file", opts.Path)
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Create parent directories if needed
	if opts.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(normalizedPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directories: %w", err)
		}
	}

	// An existing file keeps its mode; a new one gets what os.Create would
	// have given it. The fixed path+".tmp" this wrote through made every
	// overwrite 0644 and let two writers to one path collide (EST-8806).
	data := []byte(opts.Content)
	mode := 0o666 &^ processUmask()
	info, err := os.Stat(normalizedPath)
	fileExists := err == nil
	if fileExists {
		if err := refuseHardLinks(normalizedPath, opts.Path, info, opts.AllowedDirs); err != nil {
			return nil, err
		}
		mode = fileMode(info)
		// The temp file held only the new content, so --append replaced
		// the file and reported success.
		if opts.Append {
			existing, err := os.ReadFile(normalizedPath)
			if err != nil {
				return nil, fmt.Errorf("failed to read file to append to: %w", err)
			}
			data = append(existing, data...)
		}
	}

	// A requested backup that cannot be written refuses the write: writing
	// anyway left the caller without the copy they asked for.
	var backupPath string
	if fileExists && opts.Backup && !opts.Append {
		backupPath, err = createEditBackup(normalizedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create backup: %w", err)
		}
	}

	inPlace, err := writeFileAtomic(normalizedPath, data, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// The file is already replaced, so a mismatch is reported, not undone.
	if opts.VerifyWrite {
		written, err := os.ReadFile(normalizedPath)
		if err != nil || string(written) != string(data) {
			return nil, fmt.Errorf("write verification failed: %s does not hold the content written", normalizedPath)
		}
	}

	result := &WriteFileResult{
		Path:    normalizedPath,
		Size:    int64(len(opts.Content)),
		Created: !fileExists,
		Backup:  backupPath,
		Message: "File written successfully",
	}
	if inPlace != "" {
		result.InPlace = true
		result.Message += inPlaceNote(inPlace)
	}
	return result, nil
}

// GetFileInfoOptions contains input parameters for GetFileInfo
type GetFileInfoOptions struct {
	Path        string
	AllowedDirs []string
}

// GetFileInfoResult represents file information
type GetFileInfoResult struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Mode     string `json:"mode"`
	Modified string `json:"modified"`
}

// GetFileInfo gets detailed information about a file or directory
func GetFileInfo(opts GetFileInfoOptions) (*GetFileInfoResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}

	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	info, err := os.Stat(normalizedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat: %w", err)
	}

	return &GetFileInfoResult{
		Path:     normalizedPath,
		Name:     info.Name(),
		Size:     info.Size(),
		IsDir:    info.IsDir(),
		Mode:     info.Mode().String(),
		Modified: info.ModTime().Format(time.RFC3339),
	}, nil
}

// CreateDirectoryOptions contains input parameters for CreateDirectory
type CreateDirectoryOptions struct {
	Path        string
	Recursive   bool
	AllowedDirs []string
}

// CreateDirectoryResult represents the result of creating a directory
type CreateDirectoryResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
	Exists  bool   `json:"exists"`
	Message string `json:"message"`
}

// CreateDirectory creates a directory
func CreateDirectory(opts CreateDirectoryOptions) (*CreateDirectoryResult, error) {
	if opts.Path == "" {
		return nil, usageErrorf("path is required")
	}

	// Check for unresolved template variables before normalizing
	if err := pathvalidation.ValidatePathForCreation(opts.Path); err != nil {
		return nil, err
	}

	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// MkdirAll is idempotent, so it cannot say whether it created anything.
	// Look first: "created" must mean this call made the directory.
	if info, statErr := os.Stat(normalizedPath); statErr == nil && info.IsDir() {
		return &CreateDirectoryResult{
			Path:    normalizedPath,
			Created: false,
			Exists:  true,
			Message: "Directory already exists",
		}, nil
	}

	if opts.Recursive {
		err = os.MkdirAll(normalizedPath, 0755)
	} else {
		err = os.Mkdir(normalizedPath, 0755)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	return &CreateDirectoryResult{
		Path:    normalizedPath,
		Created: true,
		Message: "Directory created successfully",
	}, nil
}
