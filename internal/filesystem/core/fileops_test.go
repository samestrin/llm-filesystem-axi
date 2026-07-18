package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := CopyFile(CopyFileOptions{Source: src, Destination: dst})
	if err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	if !res.Success || res.Operation != "copy" {
		t.Errorf("result = %+v, want success/copy", res)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "hello" {
		t.Errorf("dst content = %q (err %v), want %q", got, err, "hello")
	}
	// Source must still exist after a copy.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source removed by copy: %v", err)
	}
}

func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "moved.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := MoveFile(MoveFileOptions{Source: src, Destination: dst})
	if err != nil {
		t.Fatalf("MoveFile: %v", err)
	}
	if !res.Success || res.Operation != "move" {
		t.Errorf("result = %+v, want success/move", res)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source still exists after move")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("destination missing after move: %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := DeleteFile(DeleteFileOptions{Path: target})
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if !res.Success || res.Operation != "delete" {
		t.Errorf("result = %+v, want success/delete", res)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("file still exists after delete")
	}
}

func TestDeleteNonEmptyDirRequiresRecursive(t *testing.T) {
	dir := t.TempDir()
	nonEmpty := filepath.Join(dir, "d")
	if err := os.Mkdir(nonEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteFile(DeleteFileOptions{Path: nonEmpty, Recursive: false}); err == nil {
		t.Error("deleting non-empty dir without recursive should error")
	}
	if _, err := DeleteFile(DeleteFileOptions{Path: nonEmpty, Recursive: true}); err != nil {
		t.Errorf("recursive delete failed: %v", err)
	}
}
