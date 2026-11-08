package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiskUsageOptions contains input parameters for GetDiskUsage
type DiskUsageOptions struct {
	Path        string
	AllowedDirs []string
}

// DiskUsageResult represents disk usage information
type DiskUsageResult struct {
	Path       string `json:"path"`
	TotalSize  int64  `json:"total_size"`
	TotalFiles int    `json:"total_files"`
	TotalDirs  int    `json:"total_dirs"`
}

// GetDiskUsage calculates disk usage for a path
func GetDiskUsage(opts DiskUsageOptions) (*DiskUsageResult, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	var totalSize int64
	var totalFiles, totalDirs int

	err = filepath.Walk(normalizedPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			totalDirs++
		} else {
			totalFiles++
			totalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to calculate disk usage: %w", err)
	}

	return &DiskUsageResult{
		Path:       normalizedPath,
		TotalSize:  totalSize,
		TotalFiles: totalFiles,
		TotalDirs:  totalDirs,
	}, nil
}

// LargeFileInfo represents a large file found
type LargeFileInfo struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

// FindLargeFilesOptions contains input parameters for FindLargeFiles
type FindLargeFilesOptions struct {
	Path        string
	MinSize     int64
	Limit       int
	AllowedDirs []string
}

// FindLargeFilesResult represents find large files result
type FindLargeFilesResult struct {
	Path    string          `json:"path"`
	MinSize int64           `json:"min_size"`
	Files   []LargeFileInfo `json:"files"`
	Total   int             `json:"total"`
}

// FindLargeFiles finds files larger than a minimum size
func FindLargeFiles(opts FindLargeFilesOptions) (*FindLargeFilesResult, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	normalizedPath, err := NormalizePath(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if err := ValidatePath(normalizedPath, opts.AllowedDirs); err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 100
	}

	var files []LargeFileInfo

	err = filepath.Walk(normalizedPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.Size() >= opts.MinSize {
			files = append(files, LargeFileInfo{
				Path:    path,
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to find large files: %w", err)
	}

	// Sort by size descending
	sort.Slice(files, func(i, j int) bool {
		return files[i].Size > files[j].Size
	})

	// Apply limit
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}

	return &FindLargeFilesResult{
		Path:    normalizedPath,
		MinSize: opts.MinSize,
		Files:   files,
		Total:   len(files),
	}, nil
}

// CompressFilesOptions contains input parameters for CompressFiles
type CompressFilesOptions struct {
	Paths       []string
	Output      string
	Format      string
	AllowedDirs []string
}

// CompressResult represents compression result.
//
// Success is Failed == 0, as in ExtractResult. A missing input, a Walk error
// and an add error were each skipped and the run still printed success: true.
// Failures is a sample of at most maxPlannedPaths "path: reason" lines; Failed
// is the true count. Overwritten is true when Output already existed.
type CompressResult struct {
	Output      string   `json:"output"`
	Format      string   `json:"format"`
	Size        int64    `json:"size"`
	FilesAdded  int      `json:"files_added"`
	Overwritten bool     `json:"overwritten"`
	Failed      int      `json:"failed"`
	Failures    []string `json:"failures,omitempty"`
	Success     bool     `json:"success"`
}

// CompressFiles compresses files into an archive
func CompressFiles(opts CompressFilesOptions) (*CompressResult, error) {
	if len(opts.Paths) == 0 {
		return nil, fmt.Errorf("paths is required")
	}
	if opts.Output == "" {
		return nil, fmt.Errorf("output is required")
	}

	format := opts.Format
	if format == "" {
		format = "zip"
	}

	normalizedOutput, err := NormalizePath(opts.Output)
	if err != nil {
		return nil, fmt.Errorf("invalid output path: %w", err)
	}

	if err := ValidatePath(normalizedOutput, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Validate and normalize input paths
	paths := make([]string, len(opts.Paths))
	for i, p := range opts.Paths {
		normalized, err := NormalizePath(p)
		if err != nil {
			return nil, fmt.Errorf("invalid path: %w", err)
		}
		if err := ValidatePath(normalized, opts.AllowedDirs); err != nil {
			return nil, err
		}
		paths[i] = normalized
	}

	// The create below truncates --output before any input is read, so an
	// output that is also an input destroys it and reports success.
	outInfo, outStatErr := os.Stat(normalizedOutput)
	for _, p := range paths {
		if p == normalizedOutput {
			return nil, fmt.Errorf("output %s is also an input; refusing to overwrite it", normalizedOutput)
		}
		if outStatErr == nil {
			if pi, err := os.Stat(p); err == nil && os.SameFile(outInfo, pi) {
				return nil, fmt.Errorf("output %s is also an input (%s); refusing to overwrite it", normalizedOutput, p)
			}
		}
	}

	var writeArchive func([]string, io.Writer, *extractTally, string) error
	switch format {
	case "zip":
		writeArchive = createZipArchive
	case "tar.gz", "tgz":
		writeArchive = createTarGzArchive
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	// Checked before the create truncates it. Only a file this run created or
	// replaced is removed if the archive cannot be finished: an --output that
	// names a device or a symlink is not ours to delete.
	prior, statErr := os.Lstat(normalizedOutput)
	overwritten := statErr == nil
	removable := !overwritten || prior.Mode().IsRegular()

	file, err := createArchiveFile(normalizedOutput)
	if err != nil {
		return nil, fmt.Errorf("failed to create archive: %w", err)
	}

	var tally extractTally
	err = writeArchive(paths, file, &tally, normalizedOutput)
	if closeErr := file.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("close: %w", closeErr)
	}
	if err != nil {
		if removable {
			_ = os.Remove(normalizedOutput)
		}
		return nil, fmt.Errorf("failed to create archive: %w", err)
	}

	var archiveSize int64
	if info, err := os.Stat(normalizedOutput); err == nil {
		archiveSize = info.Size()
	}

	return &CompressResult{
		Output:      normalizedOutput,
		Format:      format,
		Size:        archiveSize,
		FilesAdded:  tally.extracted,
		Overwritten: overwritten,
		Failed:      tally.failed,
		Failures:    tally.failures,
		Success:     tally.failed == 0,
	}, nil
}

// createArchiveFile is a variable so a test can make the archive's Close or
// writes fail, which no real test filesystem does.
var createArchiveFile = func(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

// failInput records an input that was not added. The path is already the
// failure's name, so an *fs.PathError naming the same path is trimmed to its
// operation and cause rather than printing the path twice.
func failInput(tally *extractTally, path string, err error) {
	var pe *fs.PathError
	if errors.As(err, &pe) && pe.Path == path {
		err = fmt.Errorf("%s: %w", pe.Op, pe.Err)
	}
	tally.fail(path, err)
}

// addInputs walks every input and hands each regular file to add, under the
// name it takes in the archive. The tally's extracted count is files added.
// A missing input, a Walk error and an add error are each a reported failure:
// all three were once skipped while the run said success.
//
// skip is the archive being written. An --output inside a directory input was
// walked into the archive, so the archive held a truncated copy of itself and
// counted it as an added file.
func addInputs(paths []string, tally *extractTally, skip string, add func(filePath, name string) error) {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			failInput(tally, path, err)
			continue
		}

		if !info.IsDir() {
			if err := add(path, filepath.Base(path)); err != nil {
				failInput(tally, path, err)
				continue
			}
			tally.extracted++
			continue
		}

		_ = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				failInput(tally, filePath, err)
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if skip != "" && filePath == skip {
				return nil
			}
			relPath, err := filepath.Rel(filepath.Dir(path), filePath)
			if err != nil {
				failInput(tally, filePath, err)
				return nil
			}
			if err := add(filePath, relPath); err != nil {
				failInput(tally, filePath, err)
				return nil
			}
			tally.extracted++
			return nil
		})
	}
}

// createZipArchive writes the archive to w. The zip writer's Close writes the
// central directory and flushes, so its error is the archive's.
func createZipArchive(paths []string, w io.Writer, tally *extractTally, skip string) error {
	zipWriter := zip.NewWriter(w)
	addInputs(paths, tally, skip, func(filePath, name string) error {
		return addToZip(zipWriter, filePath, name)
	})
	return zipWriter.Close()
}

func addToZip(zipWriter *zip.Writer, filePath, name string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	w, err := zipWriter.Create(name)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, file)
	return err
}

// createTarGzArchive writes the archive to w. The tar writer's Close writes
// the end-of-archive blocks and the gzip writer's writes the trailer; either
// failing leaves an archive extract-archive cannot read.
func createTarGzArchive(paths []string, w io.Writer, tally *extractTally, skip string) error {
	gzipWriter := gzip.NewWriter(w)
	tarWriter := tar.NewWriter(gzipWriter)
	addInputs(paths, tally, skip, func(filePath, name string) error {
		return addToTar(tarWriter, filePath, name)
	})
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return err
	}
	return gzipWriter.Close()
}

func addToTar(tarWriter *tar.Writer, filePath, name string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name: name,
		Mode: int64(info.Mode()),
		Size: info.Size(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tarWriter, file)
	return err
}

// ExtractArchiveOptions contains input parameters for ExtractArchive
type ExtractArchiveOptions struct {
	Archive     string
	Destination string
	AllowedDirs []string
}

// ExtractResult represents extraction result.
//
// Success is Failed == 0, as in SyncResult. An extract that refused an entry
// used to print files_extracted: 1, success: true, and the caller had no way
// to learn anything was dropped. Failed is the true count; Failures is a
// sample of at most maxPlannedPaths "name: reason" lines, because the names
// come from the archive and a hostile one can hold any number of them.
type ExtractResult struct {
	Archive        string   `json:"archive"`
	Destination    string   `json:"destination"`
	FilesExtracted int      `json:"files_extracted"`
	Overwritten    int      `json:"overwritten"`
	Failed         int      `json:"failed"`
	Failures       []string `json:"failures,omitempty"`
	Success        bool     `json:"success"`
}

// extractTally counts what an extraction did and did not do.
type extractTally struct {
	extracted   int
	overwritten int
	failed      int
	failures    []string
}

func (t *extractTally) fail(entry string, err error) {
	t.failed++
	if len(t.failures) < maxPlannedPaths {
		t.failures = append(t.failures, entry+": "+err.Error())
	}
}

// ExtractArchive extracts an archive to a destination
func ExtractArchive(opts ExtractArchiveOptions) (*ExtractResult, error) {
	if opts.Archive == "" {
		return nil, fmt.Errorf("archive is required")
	}
	if opts.Destination == "" {
		return nil, fmt.Errorf("destination is required")
	}

	normalizedArchive, err := NormalizePath(opts.Archive)
	if err != nil {
		return nil, fmt.Errorf("invalid archive path: %w", err)
	}

	normalizedDest, err := NormalizePath(opts.Destination)
	if err != nil {
		return nil, fmt.Errorf("invalid destination path: %w", err)
	}

	if err := ValidatePath(normalizedArchive, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := ValidatePath(normalizedDest, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// Create destination directory
	if err := os.MkdirAll(normalizedDest, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	var tally extractTally

	// Detect format
	if strings.HasSuffix(normalizedArchive, ".zip") {
		err = extractZip(normalizedArchive, normalizedDest, &tally)
	} else if strings.HasSuffix(normalizedArchive, ".tar.gz") || strings.HasSuffix(normalizedArchive, ".tgz") {
		err = extractTarGz(normalizedArchive, normalizedDest, &tally)
	} else {
		return nil, fmt.Errorf("unsupported archive format")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to extract archive: %w", err)
	}

	return &ExtractResult{
		Archive:        normalizedArchive,
		Destination:    normalizedDest,
		FilesExtracted: tally.extracted,
		Overwritten:    tally.overwritten,
		Failed:         tally.failed,
		Failures:       tally.failures,
		Success:        tally.failed == 0,
	}, nil
}

var errEntryEscapes = errors.New("refused: resolves outside the destination")

// entryTarget resolves an archive entry name under destination. isDest is true
// for an entry that names the destination itself, such as the `./` that
// `tar czf x.tgz -C dir .` writes: that is not an escape. The old guard
// compared against destination+"/", which the destination itself fails, so
// once every refusal is reported an ordinary archive would have read as broken.
func entryTarget(destination, name string) (path string, isDest bool, err error) {
	path = filepath.Join(destination, name)
	rel, err := filepath.Rel(destination, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false, errEntryEscapes
	}
	return path, rel == ".", nil
}

// createExtractedFile is a variable so a test can make Close fail, which no
// real test filesystem does.
var createExtractedFile = func(path string) (io.WriteCloser, error) {
	return os.Create(path)
}

// writeEntry writes one regular file and counts it. Every error is the
// entry's failure: mkdir, create, copy and close were each discarded once, and
// a dropped Close error is how a write that never reached the disk got counted.
//
// A symlink already at the target path is refused rather than followed.
// os.Create follows it, and entryTarget only compares names lexically, so a
// link planted in the destination made the entry write outside it.
func writeEntry(path, entry string, r io.Reader, tally *extractTally) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		tally.fail(entry, err)
		return
	}

	prior, statErr := os.Lstat(path)
	existed := statErr == nil
	if existed && prior.Mode()&os.ModeSymlink != 0 {
		tally.fail(entry, fmt.Errorf("refused: destination path is a symlink"))
		return
	}

	out, err := createExtractedFile(path)
	if err != nil {
		tally.fail(entry, err)
		return
	}
	// Create has truncated the old file, so it is overwritten whether or not
	// the copy that follows succeeds.
	if existed {
		tally.overwritten++
	}

	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		tally.fail(entry, fmt.Errorf("copy: %w", err))
		return
	}
	if err := out.Close(); err != nil {
		tally.fail(entry, fmt.Errorf("close: %w", err))
		return
	}
	tally.extracted++
}

func extractZip(archive, destination string, tally *extractTally) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		// Prevent zip slip vulnerability
		path, isDest, err := entryTarget(destination, file.Name)
		if err != nil {
			tally.fail(file.Name, err)
			continue
		}

		if file.FileInfo().IsDir() {
			if isDest {
				continue
			}
			if err := os.MkdirAll(path, file.Mode()); err != nil {
				tally.fail(file.Name, err)
			}
			continue
		}

		// As in the tar case, a file entry naming the destination itself
		// fails at create and is reported there.
		//
		// A symlink entry carries its link target as the entry body and a
		// symlink mode. Writing it out made a plain file holding that text and
		// counted it in files_extracted, while the same tar entry was refused.
		if file.Mode()&os.ModeSymlink != 0 {
			tally.fail(file.Name, fmt.Errorf("refused: unsupported entry type (symlink)"))
			continue
		}
		rc, err := file.Open()
		if err != nil {
			tally.fail(file.Name, fmt.Errorf("open: %w", err))
			continue
		}
		writeEntry(path, file.Name, rc, tally)
		// The read side: a bad checksum or a damaged stream surfaces in the
		// copy, which writeEntry has already reported.
		_ = rc.Close()
	}

	return nil
}

// tarTypeNames names the entry types extract-archive does not write.
var tarTypeNames = map[byte]string{
	tar.TypeSymlink: "symlink",
	tar.TypeLink:    "hard link",
	tar.TypeChar:    "character device",
	tar.TypeBlock:   "block device",
	tar.TypeFifo:    "fifo",
}

func extractTarGz(archive, destination string, tally *extractTally) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A damaged stream ends the run, but what was already written is
			// still reported rather than thrown away with the error.
			tally.fail(filepath.Base(archive), fmt.Errorf("read: %w", err))
			break
		}

		// A pax global header is metadata for the archive, not an entry.
		// `git archive` writes one into every tarball.
		if header.Typeflag == tar.TypeXGlobalHeader {
			continue
		}

		// Prevent tar slip vulnerability
		path, isDest, err := entryTarget(destination, header.Name)
		if err != nil {
			tally.fail(header.Name, err)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if isDest {
				continue
			}
			if err := os.MkdirAll(path, os.FileMode(header.Mode)); err != nil {
				tally.fail(header.Name, err)
			}
		case tar.TypeReg:
			// A file entry naming the destination itself fails at create,
			// because the destination is a directory, and is reported there.
			writeEntry(path, header.Name, tarReader, tally)
		default:
			kind, ok := tarTypeNames[header.Typeflag]
			if !ok {
				kind = fmt.Sprintf("type %q", header.Typeflag)
			}
			tally.fail(header.Name, fmt.Errorf("refused: unsupported entry type (%s)", kind))
		}
	}

	return nil
}

// SyncDirectoriesOptions contains input parameters for SyncDirectories
type SyncDirectoriesOptions struct {
	Source      string
	Destination string
	AllowedDirs []string
	DryRun      bool
}

// maxPlannedPaths bounds the preview. A sync of 50,000 files must not answer
// with 50,000 strings; files_copied stays the authoritative count, and planned
// is a sample of what would be written.
const maxPlannedPaths = 100

// SyncResult represents sync result.
//
// DryRun is the discriminator, and it carries the same field names in both
// modes so the shape never forks. safe-edit and search-and-replace once marked
// a preview only in their TEXT renderer, which left a dry run indistinguishable
// from a real one in TOON and JSON — the default and the machine format. They
// now carry dry_run too.
type SyncResult struct {
	Source      string   `json:"source"`
	Destination string   `json:"destination"`
	FilesCopied int      `json:"files_copied"`
	DirsCreated int      `json:"dirs_created"`
	Success     bool     `json:"success"`
	Failed      int      `json:"failed,omitempty"`
	Failures    []string `json:"failures,omitempty"`
	DryRun      bool     `json:"dry_run,omitempty"`
	Planned     []string `json:"planned,omitempty"`
}

// SyncDirectories synchronizes two directories
func SyncDirectories(opts SyncDirectoriesOptions) (*SyncResult, error) {
	if opts.Source == "" {
		return nil, fmt.Errorf("source is required")
	}
	if opts.Destination == "" {
		return nil, fmt.Errorf("destination is required")
	}

	normalizedSrc, err := NormalizePath(opts.Source)
	if err != nil {
		return nil, fmt.Errorf("invalid source path: %w", err)
	}

	normalizedDst, err := NormalizePath(opts.Destination)
	if err != nil {
		return nil, fmt.Errorf("invalid destination path: %w", err)
	}

	if err := ValidatePath(normalizedSrc, opts.AllowedDirs); err != nil {
		return nil, err
	}
	if err := ValidatePath(normalizedDst, opts.AllowedDirs); err != nil {
		return nil, err
	}

	// A source that is missing or is not a directory used to walk nothing and
	// report success with zero files. filepath.Walk hands the callback an error
	// for an unreadable root, and the callback discarded it.
	srcInfo, err := os.Stat(normalizedSrc)
	if err != nil {
		return nil, fmt.Errorf("source is not readable: %w", err)
	}
	if !srcInfo.IsDir() {
		return nil, fmt.Errorf("source is not a directory: %s", normalizedSrc)
	}

	var filesCopied, dirsCreated int
	var planned []string
	var failures []string
	var failed int

	// dirs_created counts directories CREATED, not directories visited:
	// MkdirAll returns nil for one that already exists, so a sync into a
	// populated destination used to claim work it had not done. The preview
	// models the same rule against the destination and remembers what it has
	// already predicted, because nothing it reports as created is on disk yet.
	// A preview that does not match the apply is worse than no preview.
	predictedCreated := make(map[string]bool)
	willCreate := func(dstPath string) bool {
		if _, statErr := os.Lstat(dstPath); statErr == nil || !os.IsNotExist(statErr) {
			return false
		}
		return !predictedCreated[dstPath]
	}

	// A dry run walks the same tree and counts the same way, skipping only the
	// two calls that write.
	//
	// It predicts rather than guarantees: the real walk skips a file whose copy
	// fails, which nothing can know in advance.
	// Every failure is recorded rather than discarded. A bare `return nil` here
	// is what let a sync destroy data and still report success: the copy failed,
	// nobody counted it, and Success stayed true regardless.
	note := func(relPath string, err error) {
		failed++
		if len(failures) < maxPlannedPaths {
			failures = append(failures, relPath+": "+err.Error())
		}
	}

	err = filepath.Walk(normalizedSrc, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			rel, _ := filepath.Rel(normalizedSrc, path)
			note(rel, walkErr)
			return nil
		}

		relPath, _ := filepath.Rel(normalizedSrc, path)
		dstPath := filepath.Join(normalizedDst, relPath)

		if info.IsDir() {
			missing := willCreate(dstPath)
			if !opts.DryRun {
				if mkErr := os.MkdirAll(dstPath, info.Mode()); mkErr != nil {
					note(relPath, mkErr)
					// Skip the subtree: returning nil would descend and every
					// copy below would fail into a directory that is not there.
					return filepath.SkipDir
				}
				if missing {
					dirsCreated++
				}
			} else if missing {
				predictedCreated[dstPath] = true
				dirsCreated++
			}
		} else {
			if !opts.DryRun {
				// copyFile takes (src, dst). This passed (dst, src), so it
				// opened a destination that did not exist yet and failed.
				if cpErr := copyFile(path, dstPath); cpErr != nil {
					note(relPath, cpErr)
					return nil
				}
			} else if len(planned) < maxPlannedPaths {
				planned = append(planned, relPath)
			}
			filesCopied++
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to sync directories: %w", err)
	}

	return &SyncResult{
		Source:      normalizedSrc,
		Destination: normalizedDst,
		FilesCopied: filesCopied,
		DirsCreated: dirsCreated,
		Success:     failed == 0,
		Failed:      failed,
		Failures:    failures,
		DryRun:      opts.DryRun,
		Planned:     planned,
	}, nil
}

// ListAllowedDirectoriesResult represents the allowed directories
type ListAllowedDirectoriesResult struct {
	AllowedDirectories []string `json:"allowed_directories"`
}

// ListAllowedDirectories returns the list of allowed directories
func ListAllowedDirectories(allowedDirs []string) *ListAllowedDirectoriesResult {
	return &ListAllowedDirectoriesResult{
		AllowedDirectories: allowedDirs,
	}
}
