package core

import (
	"os"
	"path/filepath"
	"strconv"
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
