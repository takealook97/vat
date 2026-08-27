// Package fsx provides filesystem helpers: writes that never leave a
// half-written file behind, and the rules about what a name may be before it
// becomes one. Every workspace mutation vat performs goes through
// WriteFileAtomic so an interrupted run cannot corrupt a manifest, a harness
// file, or a record.
//
// PortableName lives here rather than beside any one identifier because four
// packages join a name to a path — manifest, harness, brain, evidence — and
// internal/brain deliberately imports neither manifest nor gitx. One definition,
// reachable from all of them, is the only way that rule and this one both hold.
package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// windowsDevices are the names Windows reserves for devices. A directory cannot
// be created with any of them, with or without an extension.
var windowsDevices = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com0": true, "com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt0": true, "lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true,
	"lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// PortableName reports why a name cannot become a directory on every platform
// vat supports, and nil when it can.
//
// vat ships Windows binaries and runs Windows CI, and a manifest or a role
// definition is committed and shared. A name that works on the machine it was
// typed on and cannot exist on a colleague's is not a name a shared file may
// carry, so it is refused everywhere rather than on whichever machine happens
// to hit it first — a rule that depends on who ran it is not a rule.
func PortableName(name string) error {
	// Windows matches the device before the first dot, so "con.api" is the CON
	// device too.
	stem, _, _ := strings.Cut(strings.ToLower(name), ".")
	if windowsDevices[stem] {
		return fmt.Errorf("%q is a reserved device name on Windows, where no file or directory may be called that", stem)
	}
	// Windows strips a trailing dot or space silently, so the name on disk is
	// not the name in the file — and it collides with whatever it strips to.
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return errors.New("name may not end in '.' or a space; Windows strips both, so what lands on disk would not be what is named here")
	}
	return nil
}
