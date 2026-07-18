package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mkTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bb"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "deep.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestListDirectoryStructure(t *testing.T) {
	dir := mkTree(t)
	res, err := ListDirectory(ListDirectoryOptions{Path: dir})
	if err != nil {
		t.Fatalf("ListDirectory: %v", err)
	}
	if res.Path != dir {
		t.Errorf("Path = %q, want %q", res.Path, dir)
	}
	if res.Total != 3 {
		t.Errorf("Total = %d, want 3 (a.go, b.txt, sub)", res.Total)
	}
	byName := map[string]DirectoryEntry{}
	for _, e := range res.Items {
		byName[e.Name] = e
	}
	if e, ok := byName["a.go"]; !ok {
		t.Error("a.go missing")
	} else {
		if e.Type != "file" || e.IsDir {
			t.Errorf("a.go type=%q is_dir=%v, want file/false", e.Type, e.IsDir)
		}
		if e.SizeReadable == "" {
			t.Error("a.go size_readable empty")
		}
	}
	if e, ok := byName["sub"]; !ok {
		t.Error("sub missing")
	} else if e.Type != "directory" || !e.IsDir {
		t.Errorf("sub type=%q is_dir=%v, want directory/true", e.Type, e.IsDir)
	}
}

func TestListDirectoryPatternFilter(t *testing.T) {
	dir := mkTree(t)
	res, err := ListDirectory(ListDirectoryOptions{Path: dir, Pattern: "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Items {
		if !e.IsDir && filepath.Ext(e.Name) != ".go" {
			t.Errorf("pattern *.go returned non-go file: %s", e.Name)
		}
	}
}

func TestListDirectorySortBySize(t *testing.T) {
	dir := mkTree(t)
	res, err := ListDirectory(ListDirectoryOptions{Path: dir, SortBy: "size"})
	if err != nil {
		t.Fatal(err)
	}
	// Ascending by size: each entry's size >= the previous.
	for i := 1; i < len(res.Items); i++ {
		if res.Items[i].Size < res.Items[i-1].Size {
			t.Errorf("not sorted by size ascending at %d: %d < %d", i, res.Items[i].Size, res.Items[i-1].Size)
		}
	}
}

func TestListDirectoryRejectsMissingPath(t *testing.T) {
	if _, err := ListDirectory(ListDirectoryOptions{Path: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestGetDirectoryTreeNesting(t *testing.T) {
	dir := mkTree(t)
	res, err := GetDirectoryTree(GetDirectoryTreeOptions{Path: dir, IncludeFiles: true})
	if err != nil {
		t.Fatalf("GetDirectoryTree: %v", err)
	}
	if res.Tree == nil {
		t.Fatal("Tree is nil")
	}
	if res.TotalDirs < 1 {
		t.Errorf("TotalDirs = %d, want >=1", res.TotalDirs)
	}
	// The "sub" directory must appear as a child with its own children slice.
	var sub *TreeNode
	for _, c := range res.Tree.Children {
		if c.Name == "sub" {
			sub = c
		}
	}
	if sub == nil {
		t.Fatal("sub directory not found in tree children")
	}
	if !sub.IsDir {
		t.Error("sub node is_dir=false, want true")
	}
}

// TestAXIProjectionContract locks the JSON field names that the CLI's minimal
// projection specs depend on (internal/filesystem/commands). If core renames a
// field, the projection would silently drop it — this test fails loudly instead.
func TestAXIProjectionContract(t *testing.T) {
	dir := mkTree(t)

	assertKeys := func(name string, v interface{}, arrayKey string, keys ...string) {
		b, _ := json.Marshal(v)
		var generic map[string]interface{}
		if err := json.Unmarshal(b, &generic); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
		arr, ok := generic[arrayKey].([]interface{})
		if !ok || len(arr) == 0 {
			t.Fatalf("%s: %q is not a non-empty array: %v", name, arrayKey, generic[arrayKey])
		}
		item := arr[0].(map[string]interface{})
		for _, k := range keys {
			if _, present := item[k]; !present {
				t.Errorf("%s: projection key %q missing from %s item (have %v)", name, k, arrayKey, keysOf(item))
			}
		}
	}

	ld, err := ListDirectory(ListDirectoryOptions{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	assertKeys("list-directory", ld, "items", "name", "type", "size_readable")

	sf, err := SearchFiles(SearchFilesOptions{Path: dir, Pattern: "*.go", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	assertKeys("search-files", sf, "matches", "path", "name", "size")

	sc, err := SearchCode(SearchCodeOptions{Path: dir, Pattern: "package", MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertKeys("search-code", sc, "matches", "file", "line", "content")
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
