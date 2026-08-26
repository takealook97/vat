package fsx_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if runtime.GOOS == "windows" {
		// Windows has no POSIX permission bits: os.Chmod there only toggles the
		// read-only attribute, so a mode always reads back as 0666. The
		// guarantee this test asserts genuinely does not hold on Windows, and
		// docs/SECURITY_MODEL.md says so rather than pretending otherwise.
		t.Skip("file modes are not POSIX on Windows")
	}
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

// The tests below drive WriteFileAtomic's failure paths. They exist because the
// package's whole promise is about what happens when a write does *not*
// succeed, and every test above this point only exercised the path where
// everything works — the guarantee was the least verified code in the module.

func TestWriteFileAtomicReportsAParentThatIsAFile(t *testing.T) {
	// Arrange: a path whose parent component is a regular file. MkdirAll cannot
	// create a directory there, and the caller has to be told which path was
	// impossible rather than being handed a bare ENOTDIR.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "occupied")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	// Act
	err := fsx.WriteFileAtomic(filepath.Join(blocker, "child.yaml"), []byte("body"), fsx.DefaultFileMode)

	// Assert
	if err == nil {
		t.Fatal("WriteFileAtomic succeeded through a file used as a directory")
	}
	if !strings.Contains(err.Error(), blocker) {
		t.Errorf("error %q does not name the path that could not be created", err)
	}
}

func TestWriteFileAtomicReportsADirectoryItCannotWriteInto(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions are not POSIX on Windows")
	}
	if os.Geteuid() == 0 {
		// root ignores the permission bits entirely, so the condition under
		// test cannot be produced rather than merely being awkward to produce.
		t.Skip("root bypasses directory permissions")
	}
	// Arrange: an existing directory with no write bit. MkdirAll returns nil for
	// a directory that already exists whatever its mode, so this is the first
	// point at which the failure can surface — the temporary file.
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	// Act
	err := fsx.WriteFileAtomic(filepath.Join(locked, "manifest.yaml"), []byte("body"), fsx.DefaultFileMode)

	// Assert
	if err == nil {
		t.Fatal("WriteFileAtomic succeeded in a directory it may not write to")
	}
	if !strings.Contains(err.Error(), "temp file") {
		t.Errorf("error %q does not say the temporary file was the step that failed", err)
	}
}

func TestWriteFileAtomicLeavesNoTemporaryFileWhenTheRenameFails(t *testing.T) {
	// Arrange: the destination is an existing directory, so the final rename
	// cannot succeed. This is the case the package exists for — a write that
	// fails at the last step must leave the destination untouched *and* must
	// not litter the workspace with a half-written .vat-* file that the next
	// `git add .` would commit.
	dir := t.TempDir()
	destination := filepath.Join(dir, "in-the-way")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "occupant"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write occupant: %v", err)
	}

	// Act
	err := fsx.WriteFileAtomic(destination, []byte("body"), fsx.DefaultFileMode)

	// Assert
	if err == nil {
		t.Fatal("WriteFileAtomic replaced a non-empty directory")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read directory: %v", readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".vat-") {
			t.Errorf("a temporary file survived the failed rename: %s", entry.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the untouched destination", len(entries))
	}
}

func TestIsEmptyDirReportsAnErrorRatherThanFalseForANonDirectory(t *testing.T) {
	// Arrange: "not empty" and "not a directory" are different answers, and a
	// caller deciding whether it is safe to delete must not conflate them.
	path := filepath.Join(t.TempDir(), "regular")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	empty, err := fsx.IsEmptyDir(path)

	// Assert
	if err == nil {
		t.Fatal("IsEmptyDir reported on a regular file without an error")
	}
	if empty {
		t.Error("IsEmptyDir returned true alongside an error")
	}
	if _, err := fsx.IsEmptyDir(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("IsEmptyDir reported on a path that does not exist without an error")
	}
}

func TestEnsureDirReportsAParentThatIsAFile(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	blocker := filepath.Join(dir, "occupied")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	// Act
	err := fsx.EnsureDir(filepath.Join(blocker, "child"))

	// Assert
	if err == nil {
		t.Fatal("EnsureDir succeeded through a file used as a directory")
	}
	if !strings.Contains(err.Error(), "create ") {
		t.Errorf("error %q does not say what it failed to create", err)
	}
}

func TestReadFileIfExistsSeparatesAbsentFromUnreadable(t *testing.T) {
	// Arrange: "not there" is a normal state a caller handles, and "there but
	// unreadable" is a problem it must not silently treat as the first.
	dir := t.TempDir()

	// Act
	_, found, err := fsx.ReadFileIfExists(dir)

	// Assert: a directory exists, so this is the unreadable case.
	if err == nil {
		t.Fatal("ReadFileIfExists read a directory without an error")
	}
	if found {
		t.Error("ReadFileIfExists reported a directory as a file it found")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the path it could not read", err)
	}
}
