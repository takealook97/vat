package metrics_test

import (
	"testing"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/metrics"
	"github.com/takealook97/vat/internal/workspace"
)

// These numbers exist to answer a question the methodology cannot answer about
// itself. A measure that silently reads zero is worse than one that is absent,
// because a flat line reads as a discipline that is holding.

func brainFixture(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	ws := fixture(t)
	next := manifest.WithRepo(ws.Manifest, manifest.Repo{
		Name: "knowledge", Origin: "https://example.invalid/acme/knowledge.git",
		Role: manifest.RoleBrain,
	})
	if err := ws.SaveManifest(next); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	reopened, err := workspace.OpenAt(ws.Root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	root, ok := reopened.BrainPath()
	if !ok {
		t.Fatal("the workspace declares a brain repository and reports no path for it")
	}
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	return reopened, root
}

func TestCollectCountsCitableRecordsSeparatelyFromAllOfThem(t *testing.T) {
	// Arrange: the number worth watching is how much of the knowledge is usable
	// as evidence right now, not how much of it has been written.
	ws, root := brainFixture(t)
	for _, record := range []struct {
		id     string
		status brain.Status
	}{
		{"D-0001", brain.StatusActive},
		{"D-0002", brain.StatusProvisional},
		{"D-0003", brain.StatusStale},
	} {
		if _, err := brain.Create(root, brain.NewRecordInput{
			Kind: brain.KindDecision, ID: record.id, Title: "Record " + record.id,
			Status: record.status, Now: reference,
		}); err != nil {
			t.Fatalf("create %s: %v", record.id, err)
		}
	}

	// Act
	snapshot, err := metrics.Collect(t.Context(), ws, reference)

	// Assert
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snapshot.BrainRecords != 3 {
		t.Errorf("counted %d records, want 3", snapshot.BrainRecords)
	}
	if snapshot.BrainCitable != 1 {
		t.Errorf("counted %d citable records, want 1; only the active one is an answer",
			snapshot.BrainCitable)
	}
	if snapshot.At == "" {
		t.Error("the snapshot carries no timestamp, so no trend can be built from it")
	}
}

func TestCollectReportsTheMedianAgeOfCurrentStateEvidence(t *testing.T) {
	// Arrange: a rising median is the signal that the repository is drifting
	// away from reality faster than anyone is re-checking it.
	ws, root := brainFixture(t)
	for _, record := range []struct {
		id       string
		observed time.Time
	}{
		{"G-0001", reference.AddDate(0, 0, -10)},
		{"G-0002", reference.AddDate(0, 0, -20)},
		{"G-0003", reference.AddDate(0, 0, -30)},
	} {
		if _, err := brain.Create(root, brain.NewRecordInput{
			Kind: brain.KindGap, ID: record.id, Title: "Claim " + record.id,
			Status: brain.StatusActive, ClaimKind: brain.ClaimCurrentState,
			OwnedBy: "payments", SourceRef: "payments@3f9a1c2e",
			Now: record.observed,
		}); err != nil {
			t.Fatalf("create %s: %v", record.id, err)
		}
	}

	// Act
	snapshot, err := metrics.Collect(t.Context(), ws, reference)

	// Assert
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snapshot.MedianClaimAgeDays != 20 {
		t.Errorf("the median claim age is %d days, want 20 for evidence 10, 20, and 30 days old",
			snapshot.MedianClaimAgeDays)
	}
}

func TestCollectInventsNoNumbersForALayerThatWasNeverAdopted(t *testing.T) {
	// Arrange: a workspace that never adopted the knowledge layer must not be
	// handed a measurement of it.
	ws := fixture(t)

	// Act
	snapshot, err := metrics.Collect(t.Context(), ws, reference)

	// Assert
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if snapshot.MedianClaimAgeDays != 0 || snapshot.BrainRecords != 0 {
		t.Errorf("a workspace with no knowledge layer reported %+v", snapshot)
	}
	if snapshot.Repositories == 0 {
		t.Error("the repository count is zero for a workspace that governs one")
	}
}

func TestCompareReadsAGrowingReviewQueueAsGettingWorse(t *testing.T) {
	// Arrange: this is the leading indicator of decay — claims being written
	// faster than they are verified — so its direction must not be ambiguous.
	current := metrics.Snapshot{
		At: reference.Format(time.RFC3339), ReviewQueue: 14, LintErrors: 0,
	}
	history := []metrics.Snapshot{{
		At: reference.AddDate(0, 0, -7).Format(time.RFC3339), ReviewQueue: 8, LintErrors: 2,
	}}

	// Act
	trends := metrics.Compare(current, history)

	// Assert
	if len(trends) == 0 {
		t.Fatal("comparing two snapshots produced no trends at all")
	}
	byName := map[string]metrics.Trend{}
	for _, trend := range trends {
		byName[trend.Name] = trend
		if trend.Improved && trend.Worsened {
			t.Errorf("%s is reported as both better and worse", trend.Name)
		}
		if trend.Reading == "" {
			t.Errorf("%s has no reading, so the number means nothing on its own", trend.Name)
		}
	}
	if queue, found := byName["review queue"]; found && !queue.Worsened {
		t.Errorf("a review queue that grew from 8 to 14 is not reported as worse: %+v", queue)
	}
	if errors, found := byName["lint errors"]; found && !errors.Improved {
		t.Errorf("lint errors falling from 2 to 0 is not reported as better: %+v", errors)
	}
}

func TestCompareWithNoHistoryStatesTheNumbersWithoutInventingADelta(t *testing.T) {
	// Arrange: the first snapshot has nothing to be compared against, and a
	// fabricated change of zero would read as a measure that is holding steady.
	current := metrics.Snapshot{At: reference.Format(time.RFC3339), ReviewQueue: 14}

	// Act
	trends := metrics.Compare(current, nil)

	// Assert
	if len(trends) == 0 {
		t.Fatal("the first snapshot produced no readings at all")
	}
	for _, trend := range trends {
		if trend.Improved || trend.Worsened {
			t.Errorf("%s claims a direction with nothing to compare against: %+v", trend.Name, trend)
		}
	}
}

func TestHistoryReadsBackEverySnapshotThatWasAppended(t *testing.T) {
	// Arrange: a ledger the next run cannot read is the same as no ledger.
	ws := fixture(t)
	for queue := 0; queue < 3; queue++ {
		if err := metrics.Append(ws, metrics.Snapshot{
			At: reference.AddDate(0, 0, queue).Format(time.RFC3339), ReviewQueue: queue,
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	// Act
	history, err := metrics.History(ws)

	// Assert
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("read back %d snapshots, wrote 3", len(history))
	}
	for i, snapshot := range history {
		if snapshot.ReviewQueue != i {
			t.Errorf("snapshot %d came back with queue %d; the ledger is out of order",
				i, snapshot.ReviewQueue)
		}
	}
}
