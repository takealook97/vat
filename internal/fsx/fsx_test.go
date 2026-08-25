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

func TestExistsAndIsDirDistinguishFileFromDirectory(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act & Assert
	if !fsx.Exists(file) || !fsx.Exists(dir) {
		t.Error("Exists reported a present path as absent")
	}
	if fsx.Exists(filepath.Join(dir, "absent")) {
		t.Error("Exists reported a missing path as present")
	}
	if !fsx.IsDir(dir) {
		t.Error("IsDir reported a directory as not one")
	}
	if fsx.IsDir(file) {
		t.Error("IsDir reported a file as a directory")
	}
}

func TestIsEmptyDirReportsBothStates(t *testing.T) {
	// Arrange
	empty := t.TempDir()
	occupied := t.TempDir()
	if err := os.WriteFile(filepath.Join(occupied, "file"), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	isEmpty, err := fsx.IsEmptyDir(empty)
	if err != nil {
		t.Fatalf("IsEmptyDir returned an error: %v", err)
	}
	isOccupied, err := fsx.IsEmptyDir(occupied)
	if err != nil {
		t.Fatalf("IsEmptyDir returned an error: %v", err)
	}

	// Assert
	if !isEmpty {
		t.Error("an empty directory was reported as occupied")
	}
	if isOccupied {
		t.Error("an occupied directory was reported as empty")
	}
}

func TestEnsureDirIsIdempotent(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "a", "b")

	// Act
	if err := fsx.EnsureDir(path); err != nil {
		t.Fatalf("first EnsureDir returned an error: %v", err)
	}
	err := fsx.EnsureDir(path)

	// Assert
	if err != nil {
		t.Fatalf("second EnsureDir returned an error: %v", err)
	}
	if !fsx.IsDir(path) {
		t.Error("the directory was not created")
	}
}

func TestWriteFileAtomicAppliesTheRequestedMode(t *testing.T) {
	// Arrange: a credential-adjacent file must not land world-readable because
	// the umask happened to allow it.
	path := filepath.Join(t.TempDir(), "restricted")

	// Act
	if err := fsx.WriteFileAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic returned an error: %v", err)
	}

	// Assert
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}
