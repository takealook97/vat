package brain_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

// The case this file exists for: a knowledge repository that predates vat.
// Adopting one used to overwrite whatever it already kept at CURRENT.md,
// because the only question asked was whether the content matched what vat
// would render — which a file vat never wrote never does.

func TestBuildLeavesAProjectionItDidNotWrite(t *testing.T) {
	// Arrange: an index somebody maintained by hand, in the place vat
	// generates its own.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")
	handWritten := "# Cortex — current state\n\nMaintained by hand since 2023.\n"
	path := filepath.Join(root, brain.CurrentFile)
	if err := os.WriteFile(path, []byte(handWritten), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result, err := brain.Build(reload(t, root), reference)

	// Assert
	if err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != handWritten {
		t.Errorf("Build overwrote a file vat did not write:\n%s", after)
	}
	if !slices.Contains(result.Skipped, brain.CurrentFile) {
		t.Errorf("Skipped = %v, want it to name %s", result.Skipped, brain.CurrentFile)
	}
	if slices.Contains(result.Changed, brain.CurrentFile) {
		t.Errorf("Changed = %v, want it to leave out the file that was left alone", result.Changed)
	}
	// The projection vat does own is still written: one foreign file must not
	// stop the rest of the repository being brought up to date.
	if !slices.Contains(result.Changed, brain.GraphFile) {
		t.Errorf("Changed = %v, want it to include %s", result.Changed, brain.GraphFile)
	}
}

func TestBuildRewritesAProjectionCarryingItsOwnProvenance(t *testing.T) {
	// Arrange: vat's own output, hand-edited. This is drift, not a foreign
	// file, and the difference is the marker the render puts in it.
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")
	if _, err := brain.Build(reload(t, root), reference); err != nil {
		t.Fatalf("first Build returned an error: %v", err)
	}
	path := filepath.Join(root, brain.CurrentFile)
	generated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(path, append(generated, []byte("\nedited by hand\n")...), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	result, err := brain.Build(reload(t, root), reference)

	// Assert
	if err != nil {
		t.Fatalf("Build returned an error: %v", err)
	}
	if !slices.Contains(result.Changed, brain.CurrentFile) {
		t.Errorf("Changed = %v, want vat's own projection rebuilt", result.Changed)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("Skipped = %v, want nothing skipped", result.Skipped)
	}
}

func TestAForeignProjectionIsReportedAsUnmanagedRatherThanDrift(t *testing.T) {
	// Arrange
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-x.md", "id: D-0001\nstatus: active", "# D-0001 — Something")
	if err := os.WriteFile(filepath.Join(root, brain.CurrentFile),
		[]byte("# hand written\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	unmanaged, err := brain.Unmanaged(root)
	if err != nil {
		t.Fatalf("Unmanaged returned an error: %v", err)
	}
	drift, err := brain.Drift(reload(t, root), reference)
	if err != nil {
		t.Fatalf("Drift returned an error: %v", err)
	}

	// Assert: reporting it as drift would offer `vat brain build` as the fix,
	// and the fix would destroy the file.
	if !slices.Contains(unmanaged, brain.CurrentFile) {
		t.Errorf("Unmanaged = %v, want it to name %s", unmanaged, brain.CurrentFile)
	}
	if slices.Contains(drift, brain.CurrentFile) {
		t.Errorf("Drift = %v, want a foreign file reported as unmanaged instead", drift)
	}
}

func TestAForeignGraphIsRecognisedByItsMissingProvenance(t *testing.T) {
	// Arrange: valid JSON that is not vat's graph.
	root, _ := newStore(t)
	if err := os.WriteFile(filepath.Join(root, brain.GraphFile),
		[]byte("{\"nodes\": []}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	unmanaged, err := brain.Unmanaged(root)

	// Assert
	if err != nil {
		t.Fatalf("Unmanaged returned an error: %v", err)
	}
	if !slices.Contains(unmanaged, brain.GraphFile) {
		t.Errorf("Unmanaged = %v, want it to name %s", unmanaged, brain.GraphFile)
	}
}

func TestUnmanagedSaysNothingAboutAProjectionThatIsNotThere(t *testing.T) {
	// Arrange
	root := t.TempDir()

	// Act
	unmanaged, err := brain.Unmanaged(root)

	// Assert
	if err != nil {
		t.Fatalf("Unmanaged returned an error: %v", err)
	}
	if len(unmanaged) != 0 {
		t.Errorf("Unmanaged = %v, want nothing for a directory with no projections", unmanaged)
	}
}
