package fsx_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/takealook97/vat/internal/fsx"
)

func TestWriteFileAtomicLeavesNoTemporaryFilesBehind(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")

	// Act
	if err := fsx.WriteFileAtomic(path, []byte("content\n"), fsx.DefaultFileMode); err != nil {
		t.Fatalf("WriteFileAtomic returned an error: %v", err)
	}

	// Assert
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "manifest.yaml" {
		t.Errorf("directory holds %v; a temporary file survived", entries)
	}
}

func TestWriteFileAtomicReplacesExistingContentWholesale(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("a much longer original body\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	if err := fsx.WriteFileAtomic(path, []byte("short\n"), fsx.DefaultFileMode); err != nil {
		t.Fatalf("WriteFileAtomic returned an error: %v", err)
	}

	// Assert
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(content) != "short\n" {
		t.Errorf("content = %q; the old body was not fully replaced", content)
	}
}

func TestWriteFileAtomicCreatesMissingParentDirectories(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "a", "b", "c.md")

	// Act
	err := fsx.WriteFileAtomic(path, []byte("x"), fsx.DefaultFileMode)

	// Assert
	if err != nil {
		t.Fatalf("WriteFileAtomic returned an error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the file was not created: %v", err)
	}
}

func TestReadFileIfExistsDistinguishesAbsentFromEmpty(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	_, absentExists, absentErr := fsx.ReadFileIfExists(filepath.Join(dir, "absent"))
	content, emptyExists, emptyErr := fsx.ReadFileIfExists(empty)

	// Assert
	if absentErr != nil || emptyErr != nil {
		t.Fatalf("unexpected errors: %v %v", absentErr, emptyErr)
	}
	if absentExists {
		t.Error("a missing file reported as existing")
	}
	if !emptyExists || len(content) != 0 {
		t.Errorf("an empty file reported as exists=%v content=%q", emptyExists, content)
	}
}
