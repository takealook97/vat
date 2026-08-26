package metrics_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/metrics"
	"github.com/takealook97/vat/internal/workspace"
)

var reference = time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

func fixture(t *testing.T) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	built := manifest.Default("acme")
	built = manifest.WithRepo(built, manifest.Repo{
		Name: "payments", Origin: "u", Role: manifest.RoleProduct, Checks: []string{"true"},
	})
	if err := manifest.Save(filepath.Join(root, manifest.FileName), built); err != nil {
		t.Fatalf("Save: %v", err)
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return ws
}

func TestAppendAccumulatesSnapshotsInOrder(t *testing.T) {
	// Arrange
	ws := fixture(t)

	// Act
	for i := 0; i < 3; i++ {
		snapshot := metrics.Snapshot{At: reference.Format(time.RFC3339), ReviewQueue: i}
		if err := metrics.Append(ws, snapshot); err != nil {
			t.Fatalf("Append returned an error: %v", err)
		}
	}
	history, err := metrics.History(ws)

	// Assert
	if err != nil {
		t.Fatalf("History returned an error: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3", len(history))
	}
	for i, snapshot := range history {
		if snapshot.ReviewQueue != i {
			t.Errorf("history[%d].ReviewQueue = %d, want %d", i, snapshot.ReviewQueue, i)
		}
	}
}

func TestAppendLeavesNoTemporaryFileBehind(t *testing.T) {
	// Arrange: the ledger is rewritten atomically, and vat's contract is that
	// no write leaves a half-finished file.
	ws := fixture(t)

	// Act
	if err := metrics.Append(ws, metrics.Snapshot{At: reference.Format(time.RFC3339)}); err != nil {
		t.Fatalf("Append returned an error: %v", err)
	}

	// Assert
	entries, err := os.ReadDir(ws.StateDir())
	if err != nil {
		t.Fatalf("read state directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != metrics.Ledger {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("state directory holds %v, want only %s", names, metrics.Ledger)
	}
}

func TestHistorySkipsACorruptLineRatherThanFailing(t *testing.T) {
	// Arrange: the ledger is derived local state; one bad line is not worth
	// failing a reporting command over.
	ws := fixture(t)
	if err := metrics.Append(ws, metrics.Snapshot{At: reference.Format(time.RFC3339), ReviewQueue: 7}); err != nil {
		t.Fatalf("Append returned an error: %v", err)
	}
	path := filepath.Join(ws.StateDir(), metrics.Ledger)
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := os.WriteFile(path, append([]byte("{not json\n"), existing...), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	history, err := metrics.History(ws)

	// Assert
	if err != nil {
		t.Fatalf("History returned an error: %v", err)
	}
	if len(history) != 1 || history[0].ReviewQueue != 7 {
		t.Errorf("history = %+v, want the one readable snapshot", history)
	}
}

func TestHistoryOnAWorkspaceWithNoLedgerIsEmpty(t *testing.T) {
	// Act
	history, err := metrics.History(fixture(t))

	// Assert
	if err != nil {
		t.Fatalf("History returned an error: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("history = %+v, want empty", history)
	}
}

func TestCollectCountsTheWorkspaceWithoutNetworkAccess(t *testing.T) {
	// Arrange
	ws := fixture(t)

	// Act
	snapshot, err := metrics.Collect(context.Background(), ws, reference)

	// Assert
	if err != nil {
		t.Fatalf("Collect returned an error: %v", err)
	}
	if snapshot.Repositories != 1 {
		t.Errorf("repositories = %d, want 1", snapshot.Repositories)
	}
	if snapshot.At == "" {
		t.Error("the snapshot has no timestamp")
	}
}

func TestCompareMarksADirectionForEveryMeasure(t *testing.T) {
	// Arrange: a growing review queue is the signal the whole layer exists to
	// surface, so it must read as worse rather than merely different.
	previous := []metrics.Snapshot{{ReviewQueue: 4, BrainCitable: 10, LintErrors: 2}}
	current := metrics.Snapshot{ReviewQueue: 9, BrainCitable: 14, LintErrors: 0}

	// Act
	trends := metrics.Compare(current, previous)

	// Assert
	byName := map[string]metrics.Trend{}
	for _, trend := range trends {
		byName[trend.Name] = trend
	}
	if queue := byName["review queue"]; !queue.Worsened {
		t.Errorf("a growing review queue was not marked as worse: %+v", queue)
	}
	if citable := byName["citable records"]; !citable.Improved {
		t.Errorf("more citable records was not marked as better: %+v", citable)
	}
	if lint := byName["lint errors"]; !lint.Improved {
		t.Errorf("fewer lint errors was not marked as better: %+v", lint)
	}
}

func TestCompareWithNoHistoryReportsNoDirection(t *testing.T) {
	// Act
	trends := metrics.Compare(metrics.Snapshot{ReviewQueue: 3}, nil)

	// Assert
	for _, trend := range trends {
		if trend.Improved || trend.Worsened {
			t.Errorf("%s claimed a direction with nothing to compare against", trend.Name)
		}
		if trend.Delta != "—" && !strings.Contains(trend.Delta, "—") {
			t.Errorf("%s delta = %q, want the no-comparison marker", trend.Name, trend.Delta)
		}
	}
}

func TestEveryMeasureExplainsWhatItMeans(t *testing.T) {
	// Arrange: a row with an empty explanation is a number nobody can act on.
	trends := metrics.Compare(metrics.Snapshot{}, nil)

	// Act & Assert
	for _, trend := range trends {
		if strings.TrimSpace(trend.Reading) == "" {
			t.Errorf("measure %q has no explanation", trend.Name)
		}
		if strings.TrimSpace(trend.Name) == "" {
			t.Error("a measure has no name")
		}
	}
}
