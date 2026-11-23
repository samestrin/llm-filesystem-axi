package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidatePath checks if a path is allowed based on the allowed directories
// list. The check follows symlinks, a final one included; see ResolvePath.
func ValidatePath(path string, allowedDirs []string) error {
	// Empty allowed dirs means all paths are allowed
	if len(allowedDirs) == 0 {
		return nil
	}

	// Normalize the path
	normalized, err := NormalizePath(path)
	if err != nil {
		return fmt.Errorf("failed to normalize path: %w", err)
	}

	// Check if path is within any allowed directory
	if !IsPathAllowed(normalized, allowedDirs) {
		return fmt.Errorf("path %q is not within allowed directories", path)
	}

	return nil
}

// NormalizePath cleans and normalizes a file path
func NormalizePath(path string) (string, error) {
	// Expand home directory
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[1:])
	}

	// Clean the path (resolves . and .., removes double slashes)
	cleaned := filepath.Clean(path)

	// Convert to absolute path if relative
	if !filepath.IsAbs(cleaned) {
		abs, err := filepath.Abs(cleaned)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path: %w", err)
		}
		cleaned = abs
	}

	return cleaned, nil
}

// ValidatePathNoFollow is ValidatePath for an operation that acts on a final
// symlink itself rather than on what it points to: delete removes the link,
// rename moves it. Only the parent is resolved, so removing a link to a file
// outside the allowed dirs is allowed, and removing through one is not.
func ValidatePathNoFollow(path string, allowedDirs []string) error {
	if len(allowedDirs) == 0 {
		return nil
	}
	normalized, err := NormalizePath(path)
	if err != nil {
		return fmt.Errorf("failed to normalize path: %w", err)
	}
	parent, err := ResolvePath(filepath.Dir(normalized))
	if err != nil {
		return fmt.Errorf("path %q: %w", path, err)
	}
	if !withinAllowed(filepath.Join(parent, filepath.Base(normalized)), allowedDirs) {
		return fmt.Errorf("path %q is not within allowed directories", path)
	}
	return nil
}

// maxLinkHops matches the kernel's ELOOP limit on Linux.
const maxLinkHops = 40

// ResolvePath returns where an absolute path really lands, following every
// symlink in it, including the final component and a dangling final link.
// The components that do not exist yet are kept as given, so a path about to
// be created resolves to where it will be created.
//
// The allowed-dirs check compared text prefixes, so with A allowed and
// A/link -> /outside, A/link/x passed the check and every write, copy, delete,
// archive and sync through it landed outside A with exit 0 (EST-8264).
func ResolvePath(path string) (string, error) {
	return resolvePath(filepath.Clean(path), 0)
}

func resolvePath(path string, hops int) (string, error) {
	if hops > maxLinkHops {
		return "", fmt.Errorf("too many levels of symbolic links")
	}
	cur, rest := path, ""
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest), nil
		}
		// A dangling link: EvalSymlinks fails, but a write through it creates
		// its target, so the target is what has to be inside.
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(cur)
			if err != nil {
				return "", err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(cur), target)
			}
			return resolvePath(filepath.Join(target, rest), hops+1)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Join(cur, rest), nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// IsPathAllowed checks if a path is within any of the allowed directories.
// Both the path and each allowed dir are resolved through their symlinks, so
// the answer is about where the path really lands, not how it is spelled.
func IsPathAllowed(path string, allowedDirs []string) bool {
	// Empty allowed dirs means all paths are allowed
	if len(allowedDirs) == 0 {
		return true
	}

	// Normalize the path for comparison
	normalizedPath, err := NormalizePath(path)
	if err != nil {
		return false
	}
	resolved, err := ResolvePath(normalizedPath)
	if err != nil {
		return false
	}
	return withinAllowed(resolved, allowedDirs)
}

// withinAllowed reports whether an already-resolved path is inside one of the
// allowed dirs, each resolved in turn.
func withinAllowed(resolved string, allowedDirs []string) bool {
	for _, dir := range allowedDirs {
		normalizedDir, err := NormalizePath(dir)
		if err != nil {
			continue
		}
		resolvedDir, err := ResolvePath(normalizedDir)
		if err != nil {
			continue
		}
		if IsWithin(resolved, resolvedDir) {
			return true
		}
	}
	return false
}

// UsageError is a refusal of the request as made: nothing was attempted, so
// the CLI exits 2 ("fix the invocation") rather than 1 ("the tool tried and
// failed"), which invites retrying the same command.
//
// Hint, when set, is the command that fixes the invocation.
type UsageError struct {
	msg  string
	Hint string
}

func (e *UsageError) Error() string { return e.msg }

func usageErrorf(format string, a ...any) error {
	return &UsageError{msg: fmt.Sprintf(format, a...)}
}

// CheckNotIntoItself refuses a copy, move or sync of a directory whose
// destination is the directory itself or inside it. The copy recursed into
// what it had just written until the path was too long, leaving hundreds of
// files behind (EST-8264). Both spellings and the resolved paths are compared,
// so a link to the source is caught too. A source that is not a directory has
// nothing inside it and passes.
func CheckNotIntoItself(source, destination string) error {
	src, err := NormalizePath(source)
	if err != nil {
		return nil
	}
	dst, err := NormalizePath(destination)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil
	}
	realSrc, err1 := ResolvePath(src)
	realDst, err2 := ResolvePath(dst)
	if IsWithin(dst, src) || (err1 == nil && err2 == nil && IsWithin(realDst, realSrc)) {
		return usageErrorf("destination %s is the source directory %s or inside it; a directory cannot be copied into itself", dst, src)
	}
	return nil
}

// CheckDeletable refuses to delete the filesystem root, HOME, the working
// directory, or any directory that holds one of them. delete-file --path ~
// --recursive removed all of HOME and --path . the working directory
// (EST-8264); setup already refused HOME. A final symlink is not followed,
// because deleting it removes only the link.
func CheckDeletable(path string) error {
	p, err := NormalizePath(path)
	if err != nil {
		return nil
	}
	real := p
	if parent, err := ResolvePath(filepath.Dir(p)); err == nil {
		real = filepath.Join(parent, filepath.Base(p))
	}

	type guarded struct{ dir, name string }
	protected := []guarded{{filepath.VolumeName(p) + string(filepath.Separator), "the filesystem root"}}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		protected = append(protected, guarded{filepath.Clean(home), "HOME"})
	}
	if cwd, err := os.Getwd(); err == nil {
		protected = append(protected, guarded{cwd, "the working directory"})
	}
	for _, g := range protected {
		dir, name := g.dir, g.name
		realDir, err := ResolvePath(dir)
		if err != nil {
			realDir = dir
		}
		if IsWithin(dir, p) || IsWithin(realDir, real) {
			if dir == p || realDir == real {
				return usageErrorf("refusing to delete %s: it is %s", p, name)
			}
			return usageErrorf("refusing to delete %s: it holds %s (%s)", p, name, dir)
		}
	}
	return nil
}

// checkTreeLinks refuses a directory tree holding a symlink that resolves
// outside the allowed dirs. A copy, sync or archive of the tree follows such a
// link and reads the outside file in, though the tree's root passed the check.
// It runs before anything is written.
func checkTreeLinks(root string, allowedDirs []string) error {
	if len(allowedDirs) == 0 {
		return nil
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil // the operation itself reports the unreadable root
	}
	return filepath.Walk(real, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		if ValidatePath(p, allowedDirs) != nil {
			return fmt.Errorf("%s links outside the allowed directories", p)
		}
		return nil
	})
}

// checkTreeDest refuses a tree copy or sync whose writes would land outside
// the allowed dirs. The destination root passing the check is not enough: a
// link already inside the destination (dest/sub -> /outside) is followed by
// MkdirAll and the copy, so every path the walk would write is resolved and
// checked before anything is written (review of PR #87).
func checkTreeDest(src, dst string, allowedDirs []string) error {
	if len(allowedDirs) == 0 {
		return nil
	}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return nil
		}
		target := filepath.Join(dst, rel)
		if ValidatePath(target, allowedDirs) != nil {
			return fmt.Errorf("%s leads outside the allowed directories", target)
		}
		return nil
	})
}

// IsWithin reports whether path is dir or below it. Both must be clean and
// absolute; nothing is resolved here.
func IsWithin(path, dir string) bool {
	if path == dir {
		return true
	}
	if dir == string(filepath.Separator) {
		return strings.HasPrefix(path, dir)
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// ResolveSymlink resolves a symlink to its target path
func ResolveSymlink(path string) (string, error) {
	// Get file info following symlinks
	info, err := os.Lstat(path)
	if err != nil {
		return path, nil // Return original path if can't stat
	}

	// If it's a symlink, resolve it
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("failed to resolve symlink: %w", err)
		}
		return target, nil
	}

	return path, nil
}

// CreateBackupPath generates a backup path
func CreateBackupPath(path string) string {
	return path + ".bak"
}

// EnsureDir creates directory and parents if needed
func EnsureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0755)
}
