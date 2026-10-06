package brain_test

import (
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

// Projections used to embed a build date and observation ages. These changed
// overnight without any record edits. Keeping rendering clock-free prevents
// both false drift findings and rebuild churn.
func TestTimePassingIsNotProjectionDrift(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := brain.Build(reload(t, root), brain.CheckPolicy{StaleAfterDays: 90}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Act: projection checks take no clock, so time passing cannot change them.
	drifted, err := brain.Drift(reload(t, root), brain.CheckPolicy{StaleAfterDays: 90})
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
	if _, err := brain.Build(reload(t, root), brain.CheckPolicy{StaleAfterDays: 90}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindDecision, ID: "D-0001", Title: "Adopt the thing", Now: reference,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Act: a record was added and nothing rebuilt.
	drifted, err := brain.Drift(reload(t, root), brain.CheckPolicy{StaleAfterDays: 90})
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}

	// Assert
	if len(drifted) == 0 {
		t.Error("a projection missing a record it should hold was reported as current")
	}
}
