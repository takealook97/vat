package brain_test

import (
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

// The projection embeds the day it was built and an age in days for every
// record, both derived from the clock. Comparing it against a re-render made
// with today's clock therefore reports drift on the first run of every new
// calendar day, on a repository nobody has touched.
//
// It was live in two of the three workspaces running vat: both had committed
// CURRENT.md alongside the records it was built from, both had clean trees and
// no commits since, and both failed `vat lint` the next morning. The third had
// rebuilt that day and was green. The remedy genuinely works, which is why it
// went unexamined — `vat brain build` rewrites the date, so the error clears
// every day and returns every night.
//
// This is the same class as the line-ending case: a difference that is not a
// difference between the records and their projection.
func TestTimePassingIsNotProjectionDrift(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := brain.Build(reload(t, root), reference); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Act: the same records, a day later, nothing else changed.
	drifted, err := brain.Drift(reload(t, root), reference.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}

	// Assert
	if len(drifted) != 0 {
		t.Errorf("a projection nobody touched drifted overnight: %v", drifted)
	}
}

// The other side: the check still has to catch a projection that is genuinely
// behind its records. A fix for the clock that stopped reporting real staleness
// would be worse than the defect.
func TestAProjectionBehindItsRecordsStillDriftsLater(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := brain.Build(reload(t, root), reference); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindDecision, ID: "D-0001", Title: "Adopt the thing", Now: reference,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Act: a record was added and nothing rebuilt, a day later.
	drifted, err := brain.Drift(reload(t, root), reference.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}

	// Assert
	if len(drifted) == 0 {
		t.Error("a projection missing a record it should hold was reported as current")
	}
}
