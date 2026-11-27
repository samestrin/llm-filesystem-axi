package core

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Both searches normalize the path and check it against the sandbox, but neither
// ever stats it. NormalizePath only expands ~, cleans, and makes the path
// absolute; ValidatePath answers "is this inside the allowed directories", not
// "does this exist". So a path that is not there walks nothing and returns a
// clean zero — no error, and the CLI exits 0.
//
// "I searched and found nothing" and "that directory is not there" are different
// answers, and an agent that cannot tell them apart will conclude the code it
// was looking for does not exist when it simply mistyped the path.
func TestSearchRefusesAMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "definitely-not-here")

	t.Run("search-code", func(t *testing.T) {
		res, err := SearchCode(SearchCodeOptions{
			Path:    missing,
			Pattern: "anything",
		})

		if err == nil {
			t.Errorf("a missing path returned a clean result instead of an error: %+v", res)
		}
	})

	t.Run("search-files", func(t *testing.T) {
		res, err := SearchFiles(SearchFilesOptions{
			Path:    missing,
			Pattern: "*.go",
		})

		if err == nil {
			t.Errorf("a missing path returned a clean result instead of an error: %+v", res)
		}
	})
}

// A path that exists but is a FILE is equally not searchable as a directory.
func TestSearchRefusesAFilePath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	if res, err := SearchCode(SearchCodeOptions{Path: file, Pattern: "hello"}); err == nil {
		t.Errorf("a file path was accepted as a search root: %+v", res)
	}
}

// The happy path must keep working: a real directory with a real match still
// returns that match and no error.
func TestSearchStillFindsMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle here\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle"})
	if err != nil {
		t.Fatalf("searching a real directory failed: %v", err)
	}
	if res.TotalMatches != 1 {
		t.Errorf("total_matches = %d, want 1", res.TotalMatches)
	}

	files, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*.txt"})
	if err != nil {
		t.Fatalf("searching a real directory failed: %v", err)
	}
	if files.Total != 1 {
		t.Errorf("total = %d, want 1", files.Total)
	}
}

// manyMatchesFixture writes files*perFile lines containing "needle", one match
// per line, plus one file named per match for search-files (a<n>.hit).
func manyMatchesFixture(t *testing.T, files, perFile int) string {
	t.Helper()
	dir := t.TempDir()
	line := "a needle on this line\n"
	for f := 0; f < files; f++ {
		body := make([]byte, 0, len(line)*perFile)
		for i := 0; i < perFile; i++ {
			body = append(body, line...)
		}
		name := filepath.Join(dir, "f"+strconv.Itoa(f)+".txt")
		if err := os.WriteFile(name, body, 0o600); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < perFile; i++ {
			hit := filepath.Join(dir, "f"+strconv.Itoa(f)+"_"+strconv.Itoa(i)+".hit")
			if err := os.WriteFile(hit, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// Epic 10 T4. Both searches stopped walking at --max-results (default 1000)
// and then reported the number they had KEPT as total_matches / total. 1,500
// matches came back as "total_matches: 1000" with no marker, which is a wrong
// number, not merely a missing hint: an agent sizing a rename across the
// codebase was told it had seen every occurrence. The cap now limits what is
// stored, not what is counted.
func TestSearchCodeCountsPastTheCap(t *testing.T) {
	// Three files of 500, so the cap of 1000 falls between files and the third
	// file is reached only by counting: total_files must still include it.
	dir := manyMatchesFixture(t, 3, 500)
	// A readable text file with no match: total_files counts files that
	// matched, not files scanned, so this one must not raise it to 4.
	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), []byte("haystack\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalMatches != 1500 {
		t.Errorf("total_matches = %d, want the exact 1500", res.TotalMatches)
	}
	if res.TotalFiles != 3 {
		t.Errorf("total_files = %d, want the exact 3", res.TotalFiles)
	}
	if len(res.Matches) != 1000 {
		t.Errorf("stored %d matches, want the cap of 1000", len(res.Matches))
	}
	if res.Shown != 1000 || !res.Truncated {
		t.Errorf("shown = %d, truncated = %v; want 1000, true", res.Shown, res.Truncated)
	}
}

// The cap falling mid-file must not lose that file's remaining matches from
// the count, and context mode (which reads the file whole) must count the same.
func TestSearchCodeCountsPastACapInsideAFile(t *testing.T) {
	dir := manyMatchesFixture(t, 2, 7)

	for _, ctx := range []int{0, 2} {
		res, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle", MaxResults: 10, ContextLines: ctx})
		if err != nil {
			t.Fatal(err)
		}
		if res.TotalMatches != 14 || res.TotalFiles != 2 || len(res.Matches) != 10 || res.Shown != 10 || !res.Truncated {
			t.Errorf("context %d: total_matches %d, total_files %d, stored %d, shown %d, truncated %v; want 14, 2, 10, 10, true",
				ctx, res.TotalMatches, res.TotalFiles, len(res.Matches), res.Shown, res.Truncated)
		}
	}
}

// Ten matches under the cap: nothing was cut, so there is no marker at all.
// A truncated: false on every ordinary search would be noise, and a shown count
// that always equals the total says nothing.
func TestSearchCodeUnderTheCapHasNoMarker(t *testing.T) {
	dir := manyMatchesFixture(t, 1, 10)

	res, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalMatches != 10 || len(res.Matches) != 10 || res.Truncated || res.Shown != 0 {
		t.Errorf("total_matches %d, stored %d, truncated %v, shown %d; want 10, 10, false, 0",
			res.TotalMatches, len(res.Matches), res.Truncated, res.Shown)
	}
	// Exactly at the cap is not truncated either: every match was returned.
	res, err = SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle", MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated {
		t.Error("10 matches with --max-results 10 were reported as truncated")
	}
}

// Epic 14 T1, the task's "done when". --max-results -1 listed nothing:
// maxResults was defaulted only when 0, so a negative cap stored no match and
// reported matches[0] with truncated: true — success with an empty list.
// --max-size uses -1 for no limit, and an agent expects the same here: a
// negative cap must return every match, with no marker, in both searches.
func TestSearchNegativeMaxResultsReturnsEveryMatch(t *testing.T) {
	dir := manyMatchesFixture(t, 3, 500) // 1,500 matches, past the default cap

	for _, max := range []int{-1, -100} {
		code, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle", MaxResults: max})
		if err != nil {
			t.Fatal(err)
		}
		if code.TotalMatches != 1500 || len(code.Matches) != 1500 || code.TotalFiles != 3 || code.Truncated || code.Shown != 0 {
			t.Errorf("max %d: total_matches %d, stored %d, total_files %d, truncated %v, shown %d; want 1500, 1500, 3, false, 0",
				max, code.TotalMatches, len(code.Matches), code.TotalFiles, code.Truncated, code.Shown)
		}

		files, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*.hit", Recursive: true, MaxResults: max})
		if err != nil {
			t.Fatal(err)
		}
		if files.Total != 1500 || len(files.Matches) != 1500 || files.Truncated || files.Shown != 0 {
			t.Errorf("max %d: total %d, stored %d, truncated %v, shown %d; want 1500, 1500, false, 0",
				max, files.Total, len(files.Matches), files.Truncated, files.Shown)
		}
	}

	// Context mode reads each file whole and keeps matches on a different
	// branch of searchInFile; no limit must hold there too, with the context
	// still attached.
	code, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle", MaxResults: -1, ContextLines: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(code.Matches) != 1500 || code.Truncated {
		t.Errorf("context mode: stored %d, truncated %v; want 1500, false", len(code.Matches), code.Truncated)
	}
	if len(code.Matches) > 0 && len(code.Matches[0].Context) == 0 {
		t.Error("context mode with no limit dropped the context lines")
	}
}

// The other half of T1: 0 keeps the default cap of 1000. Treating every
// non-positive value as no limit would silently uncap callers that pass 0.
func TestSearchZeroMaxResultsKeepsTheDefaultCap(t *testing.T) {
	dir := manyMatchesFixture(t, 3, 500)

	code, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle", MaxResults: 0})
	if err != nil {
		t.Fatal(err)
	}
	if code.TotalMatches != 1500 || len(code.Matches) != 1000 || !code.Truncated {
		t.Errorf("total_matches %d, stored %d, truncated %v; want 1500, 1000, true",
			code.TotalMatches, len(code.Matches), code.Truncated)
	}

	files, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*.hit", Recursive: true, MaxResults: 0})
	if err != nil {
		t.Fatal(err)
	}
	if files.Total != 1500 || len(files.Matches) != 1000 || !files.Truncated {
		t.Errorf("total %d, stored %d, truncated %v; want 1500, 1000, true",
			files.Total, len(files.Matches), files.Truncated)
	}
}

// search-files had the same bug: total was len(matches), capped.
func TestSearchFilesCountsPastTheCap(t *testing.T) {
	dir := manyMatchesFixture(t, 3, 500)

	res, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*.hit", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1500 || len(res.Matches) != 1000 || res.Shown != 1000 || !res.Truncated {
		t.Errorf("total %d, stored %d, shown %d, truncated %v; want 1500, 1000, 1000, true",
			res.Total, len(res.Matches), res.Shown, res.Truncated)
	}

	small := manyMatchesFixture(t, 1, 10)
	res, err = SearchFiles(SearchFilesOptions{Path: small, Pattern: "*.hit", Recursive: true, MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 10 || len(res.Matches) != 10 || res.Truncated || res.Shown != 0 {
		t.Errorf("total %d, stored %d, truncated %v, shown %d; want 10, 10, false, 0",
			res.Total, len(res.Matches), res.Truncated, res.Shown)
	}
}

// unreadableFixture builds a tree with one readable match, one unreadable file
// and one unreadable directory, each holding the same match. It skips the test
// as root, where permission bits are not enforced and nothing is unreadable.
func unreadableFixture(t *testing.T) (dir, lockedFile, lockedDir string) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("running as root; permission bits would not be enforced")
	}
	dir = t.TempDir()
	lockedFile = filepath.Join(dir, "locked.txt")
	lockedDir = filepath.Join(dir, "lockeddir")
	for path, body := range map[string]string{
		filepath.Join(dir, "ok.txt"):          "needle\n",
		lockedFile:                            "needle\n",
		filepath.Join(lockedDir, "inner.txt"): "needle\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{lockedFile, lockedDir} {
		if err := os.Chmod(p, 0o000); err != nil {
			t.Skip("cannot remove permissions")
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
	}
	return dir, lockedFile, lockedDir
}

// missingSkips returns each want path that has no entry in s in the
// "path: reason" form, with a reason after it.
func missingSkips(s *SkipReport, want ...string) []string {
	var missing []string
	for _, w := range want {
		found := false
		if s != nil {
			for _, e := range s.Paths {
				if strings.HasPrefix(e, w+": ") && len(e) > len(w)+2 {
					found = true
				}
			}
		}
		if !found {
			missing = append(missing, w)
		}
	}
	return missing
}

// Epic 14 T2, the task's "done when". An unreadable file was counted as binary
// (isBinaryFile assumed binary when it could not open the file) and an
// unreadable directory was a walk error the visitor swallowed, so search-code
// answered "1 match" for a tree holding 3, exit 0, with nothing to say two
// places were never looked at. Both must be listed under skipped.
func TestSearchCodeListsWhatItCouldNotRead(t *testing.T) {
	dir, lockedFile, lockedDir := unreadableFixture(t)

	res, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalMatches != 1 {
		t.Errorf("total_matches = %d, want the 1 readable match", res.TotalMatches)
	}
	if res.Skipped == nil || res.Skipped.Count != 2 || len(res.Skipped.Paths) != 2 {
		t.Fatalf("skipped = %+v, want 2 entries", res.Skipped)
	}
	if missing := missingSkips(res.Skipped, lockedFile, lockedDir); len(missing) > 0 {
		t.Errorf("not listed under skipped: %v (got %v)", missing, res.Skipped.Paths)
	}
}

// search-files never opens a file, so an unreadable file is still found by
// name; an unreadable directory cannot be listed and must be reported.
func TestSearchFilesListsADirectoryItCouldNotRead(t *testing.T) {
	dir, _, lockedDir := unreadableFixture(t)

	res, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*.txt", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Errorf("total = %d, want ok.txt and locked.txt", res.Total)
	}
	if res.Skipped == nil || res.Skipped.Count != 1 {
		t.Fatalf("skipped = %+v, want the one directory", res.Skipped)
	}
	if missing := missingSkips(res.Skipped, lockedDir); len(missing) > 0 {
		t.Errorf("not listed under skipped: %v (got %v)", missing, res.Skipped.Paths)
	}
}

// A clean tree carries no skipped marker at all: a skipped: 0 on every search
// would be noise that teaches an agent to ignore the real one.
func TestSearchCleanTreeHasNoSkipped(t *testing.T) {
	dir := manyMatchesFixture(t, 2, 3)

	code, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if code.Skipped != nil {
		t.Errorf("search-code on a clean tree reported skipped: %+v", code.Skipped)
	}
	files, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	if files.Skipped != nil {
		t.Errorf("search-files on a clean tree reported skipped: %+v", files.Skipped)
	}
}

// The entries are a sample capped at maxPlannedPaths, like failures; the count
// is exact. A tree of thousands of unreadable files must not print thousands of
// lines, and must not report the cap as the count either.
func TestSearchSkippedIsCappedButCountedExactly(t *testing.T) {
	var s SkipReport
	for i := 0; i < maxPlannedPaths+5; i++ {
		s.add("p"+strconv.Itoa(i), os.ErrPermission)
	}
	if s.Count != maxPlannedPaths+5 || len(s.Paths) != maxPlannedPaths {
		t.Errorf("count %d, entries %d; want %d, %d", s.Count, len(s.Paths), maxPlannedPaths+5, maxPlannedPaths)
	}
}

// Walk does not follow a symlink to a directory, and isBinaryFile opens it and
// fails to read a directory. That is the walk's policy, not something the
// search could not read, so it must not be listed under skipped: every linked
// tree in a checkout would otherwise read as unreadable.
func TestSearchCodeDoesNotSkipADirectorySymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real", "a.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Skip("cannot create a symlink")
	}

	res, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "needle"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != nil {
		t.Errorf("a directory symlink was reported as skipped: %+v", res.Skipped)
	}
}
