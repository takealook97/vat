// Package fsx provides filesystem helpers that never leave a half-written file
// behind. Every workspace mutation vat performs goes through WriteFileAtomic so
// an interrupted run cannot corrupt a manifest, a harness file, or a record.
package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultFileMode is the permission applied to files vat creates.
const DefaultFileMode fs.FileMode = 0o644

// DefaultDirMode is the permission applied to directories vat creates.
const DefaultDirMode fs.FileMode = 0o755

// WriteFileAtomic writes data to path via a temporary file in the same
// directory followed by a rename, so readers observe either the old content or
// the new content and never a truncated blend of the two.
func WriteFileAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DefaultDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".vat-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

// Exists reports whether path exists, without distinguishing file from
// directory. A permission error is reported as "does not exist" only when the
// underlying error is ErrNotExist.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

// IsDir reports whether path exists and is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsEmptyDir reports whether path is a directory with no entries.
func IsEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// EnsureDir creates path and all missing parents.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, DefaultDirMode); err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	return nil
}

// ReadFileIfExists returns the file content and true when the file exists, or
// an empty slice and false when it does not. Any other error is returned.
func ReadFileIfExists(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}
