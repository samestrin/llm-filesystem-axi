package core

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// FileOpResult represents the result of a file operation
type FileOpResult struct {
	Path        string `json:"path"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Success     bool   `json:"success"`
	Operation   string `json:"operation"`
	Message     string `json:"message"`
	// Deleted and Existed are set by a delete only, so a delete of a path that
	// was already gone (a success, not an error) cannot be read as a deletion.
	Deleted *bool `json:"deleted,omitempty"`
	Existed *bool `json:"existed,omitempty"`
}

// CopyFileOptions contains input parameters for CopyFile
type CopyFileOptions struct {
	Source      string
	Destination string
	AllowedDirs []string
}

// CopyFile copies a file or directory
func CopyFile(opts CopyFileOptions) (*FileOpResult, error) {
	if opts.Source == "" {
		return nil, fmt.Errorf("source is required")
	}
	if opts.Destination == "" {
		return nil, fmt.Errorf("destination is required")
	}

	// Normalize and validate paths
	srcPath, err := NormalizePath(opts.Source)
	if err != nil {
		return nil, fmt.Errorf("invalid source path: %w", err)
	}

	dstPath, err := NormalizePath(opts.Destination)
	if err != nil {
		return nil, fmt.Errorf("invalid destination path: %w", err)
	}

	if err := ValidatePath(srcPath, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := ValidatePath(dstPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Check source exists
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("source does not exist: %w", err)
	}

	if srcInfo.IsDir() {
		if err := checkTreeLinks(srcPath, opts.AllowedDirs); err != nil {
			return nil, err
		}
	}

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	if srcInfo.IsDir() {
		if err := copyDir(srcPath, dstPath); err != nil {
			return nil, fmt.Errorf("failed to copy directory: %w", err)
		}
	} else {
		if err := copyFile(srcPath, dstPath); err != nil {
			return nil, fmt.Errorf("failed to copy file: %w", err)
		}
	}

	return &FileOpResult{
		Source:      srcPath,
		Destination: dstPath,
		Success:     true,
		Operation:   "copy",
		Message:     "Copy completed successfully",
	}, nil
}

// copyFile copies src over dst atomically.
//
// It writes to a temporary file alongside the destination and renames it into
// place. os.Create TRUNCATES its target before any bytes are copied, so a copy
// that failed part-way — EISDIR, ENOSPC, EIO, a permission change — destroyed
// whatever was already at the destination and left nothing to recover. A rename
// either replaces the destination completely or not at all.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		// filepath.Walk uses Lstat, so a symlink to a directory arrives here
		// classed as a file. Refusing early keeps it from reaching the copy.
		return fmt.Errorf("not a regular file: %s", src)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".llmfs-copy-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, srcFile); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, srcInfo.Mode()); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// MoveFileOptions contains input parameters for MoveFile
type MoveFileOptions struct {
	Source      string
	Destination string
	AllowedDirs []string
}

// MoveFile moves or renames a file or directory
func MoveFile(opts MoveFileOptions) (*FileOpResult, error) {
	if opts.Source == "" {
		return nil, fmt.Errorf("source is required")
	}
	if opts.Destination == "" {
		return nil, fmt.Errorf("destination is required")
	}

	// Normalize and validate paths
	srcPath, err := NormalizePath(opts.Source)
	if err != nil {
		return nil, fmt.Errorf("invalid source path: %w", err)
	}

	dstPath, err := NormalizePath(opts.Destination)
	if err != nil {
		return nil, fmt.Errorf("invalid destination path: %w", err)
	}

	// No-follow on the source: a rename moves a final link, not its target.
	if err := ValidatePathNoFollow(srcPath, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := ValidatePath(dstPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Check source exists
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return nil, fmt.Errorf("source does not exist: %w", err)
	}

	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Try rename first (works across same filesystem)
	if err := os.Rename(srcPath, dstPath); err != nil {
		// Fallback to copy + delete (cross-filesystem)
		if srcInfo.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return nil, fmt.Errorf("failed to move directory: %w", err)
			}
			os.RemoveAll(srcPath)
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return nil, fmt.Errorf("failed to move file: %w", err)
			}
			os.Remove(srcPath)
		}
	}

	return &FileOpResult{
		Source:      srcPath,
		Destination: dstPath,
		Success:     true,
		Operation:   "move",
		Message:     "Move completed successfully",
	}, nil
}

// DeleteFileOptions contains input parameters for DeleteFile
type DeleteFileOptions struct {
	Path        string
	Recursive   bool
	AllowedDirs []string
}

// DeleteFile deletes a file or directory
func DeleteFile(opts DeleteFileOptions) (*FileOpResult, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Normalize and validate path
	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// No-follow: a delete removes a final link, not what it points to.
	if err := ValidatePathNoFollow(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Lstat, not Stat: a dangling symlink exists and is deleted, rather than
	// being reported as already gone.
	info, err := os.Lstat(normalizedPath)
	if errors.Is(err, fs.ErrNotExist) {
		// The state asked for already holds, so this is a no-op success. A
		// missing parent is not: the path is more likely a typo than a file
		// deleted earlier, and acknowledging it would hide that.
		parent, perr := os.Stat(filepath.Dir(normalizedPath))
		if perr != nil || !parent.IsDir() {
			return nil, fmt.Errorf("path does not exist, and neither does its parent directory: %w", err)
		}
		no := false
		return &FileOpResult{
			Path:      normalizedPath,
			Success:   true,
			Operation: "delete",
			Message:   "Path does not exist; nothing to delete",
			Deleted:   &no,
			Existed:   &no,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot stat path: %w", err)
	}

	yes := true
	if info.IsDir() {
		if opts.Recursive {
			if err := os.RemoveAll(normalizedPath); err != nil {
				return nil, fmt.Errorf("failed to delete directory: %w", err)
			}
		} else {
			if err := os.Remove(normalizedPath); err != nil {
				return nil, fmt.Errorf("failed to delete directory (use recursive=true for non-empty): %w", err)
			}
		}
	} else {
		if err := os.Remove(normalizedPath); err != nil {
			return nil, fmt.Errorf("failed to delete file: %w", err)
		}
	}

	return &FileOpResult{
		Path:      normalizedPath,
		Success:   true,
		Operation: "delete",
		Message:   "Delete completed successfully",
		Deleted:   &yes,
		Existed:   &yes,
	}, nil
}

// BatchOperation represents a single batch operation
type BatchOperation struct {
	Operation   string `json:"operation"`
	Source      string `json:"source"`
	Destination string `json:"destination,omitempty"`
	Recursive   bool   `json:"recursive,omitempty"`
}

// BatchFileOperationsOptions contains input parameters for BatchFileOperations
type BatchFileOperationsOptions struct {
	Operations  []BatchOperation
	AllowedDirs []string
}

// BatchOpResult represents the result of batch operations
type BatchOpResult struct {
	Success    int            `json:"success"`
	Failed     int            `json:"failed"`
	Operations []FileOpResult `json:"operations"`
}

// BatchFileOperations performs multiple file operations
func BatchFileOperations(opts BatchFileOperationsOptions) (*BatchOpResult, error) {
	var results []FileOpResult
	success := 0
	failed := 0

	for _, op := range opts.Operations {
		var result *FileOpResult
		var err error

		switch op.Operation {
		case "copy":
			result, err = CopyFile(CopyFileOptions{
				Source:      op.Source,
				Destination: op.Destination,
				AllowedDirs: opts.AllowedDirs,
			})
		case "move":
			result, err = MoveFile(MoveFileOptions{
				Source:      op.Source,
				Destination: op.Destination,
				AllowedDirs: opts.AllowedDirs,
			})
		case "delete":
			result, err = DeleteFile(DeleteFileOptions{
				Path:        op.Source,
				Recursive:   op.Recursive,
				AllowedDirs: opts.AllowedDirs,
			})
		default:
			err = fmt.Errorf("unknown operation: %s", op.Operation)
		}

		opResult := FileOpResult{
			Operation: op.Operation,
		}

		if err != nil {
			opResult.Success = false
			opResult.Message = err.Error()
			failed++
		} else {
			opResult = *result
			success++
		}

		results = append(results, opResult)
	}

	return &BatchOpResult{
		Success:    success,
		Failed:     failed,
		Operations: results,
	}, nil
}
