package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// history/ and archive/ were created by init and written to by nothing, so a
// replaced decision stayed in decisions/ forever. That is not only clutter: an
// external index cannot exclude a withdrawn claim by directory when the
// withdrawn claim still sits in the same directory as the current ones, and a
// superseded decision surfacing as an answer is the failure this whole layer
// exists to prevent.
func TestArchiveMovesTerminalRecordsAndLeavesTheWorkingSetAlone(t *testing.T) {
	// Arrange
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Old", Status: StatusSuperseded,
	})
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0002", Title: "Current", Status: StatusActive,
	})

	// Act
	moves, err := Archive(mustLoad(t, root), true)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Assert
	if len(moves) != 1 || moves[0].ID != "D-0001" {
		t.Fatalf("wrong records moved: %+v", moves)
	}
	if moves[0].To != "archive/decisions/D-0001-old.md" {
		t.Errorf("moved to %q", moves[0].To)
	}
	if _, err := os.Stat(filepath.Join(root, "decisions", "D-0001-old.md")); !os.IsNotExist(err) {
		t.Error("the archived record is still in the working directory")
	}
	if _, err := os.Stat(filepath.Join(root, "decisions", "D-0002-current.md")); err != nil {
		t.Errorf("a live record was moved: %v", err)
	}

	// An archived record is out of the working set but still loaded, because
	// the supersession chain it belongs to is still checked from both ends.
	reloaded := mustLoad(t, root)
	archived, ok := reloaded.ByID()["D-0001"]
	if !ok {
		t.Fatal("the archived record stopped being loadable, so no check can see the chain any more")
	}
	if !archived.Archived {
		t.Error("the archived record is not marked as archived")
	}
	if len(reloaded.WorkingSet()) != 1 {
		t.Errorf("working set = %d records, want 1", len(reloaded.WorkingSet()))
	}
}

func TestArchiveWithoutApplyWritesNothing(t *testing.T) {
	// Arrange
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindGap, ID: "G-0001", Title: "Closed", Status: StatusResolved,
	})

	// Act
	moves, err := Archive(mustLoad(t, root), false)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Assert
	if len(moves) != 1 || moves[0].Applied {
		t.Fatalf("a dry run reported an applied move: %+v", moves)
	}
	if _, err := os.Stat(filepath.Join(root, "gaps", "G-0001-closed.md")); err != nil {
		t.Errorf("a dry run moved the record anyway: %v", err)
	}
}

// Moving a file changes what every relative link inside it points at. Leaving
// them would turn one archive into a page of dead links and a batch of
// brain/link-broken findings against records nobody is working on.
func TestArchiveRepointsRelativeLinksSoTheyStillResolve(t *testing.T) {
	// Arrange
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{Kind: KindGoal, ID: "O-0001", Title: "Ship weekly"})
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Old", Status: StatusRevoked,
		Body: "# D-0001 — Old\n\nSupports [O-0001](../goals/O-0001-ship-weekly.md) and https://example.com.\n",
	})

	// Act
	if _, err := Archive(mustLoad(t, root), true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Assert
	moved, err := os.ReadFile(filepath.Join(root, "archive", "decisions", "D-0001-old.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(moved), "(../../goals/O-0001-ship-weekly.md)") {
		t.Errorf("the link was not repointed:\n%s", moved)
	}
	if !strings.Contains(string(moved), "https://example.com") {
		t.Errorf("an absolute link was rewritten:\n%s", moved)
	}
	for _, finding := range Check(mustLoad(t, root), CheckPolicy{}, observedOn) {
		if finding.Rule == "brain/link-broken" {
			t.Errorf("archiving broke a link: %+v", finding)
		}
	}
}
