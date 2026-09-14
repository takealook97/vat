package metrics_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/metrics"
)

// The measure named "lint warnings" was not the number `vat lint` prints.
// Collect ran the rules offline, which skips every rule that resolves a git
// revision, so a workspace reporting 47 warnings was measured at nought — and
// the one measure that would have shown the knowledge layer decaying was the
// one being skipped.

func TestEveryMeasureCanReadItsOwnPreviousValue(t *testing.T) {
	// Arrange: the comparison switches on the measure's display name, so a
	// measure added to the table and not to that switch silently reports a
	// delta of zero forever. That is worse than no trend, because it reads as
	// a workspace holding steady.
	before := metrics.Snapshot{
		LintErrors: 1, LintWarnings: 2, ReviewQueue: 3, ReviewOverdue: 4,
		MedianClaimAgeDays: 5, ClaimsMeasured: 1, BrainCitable: 6,
		ChangesetsOpen: 7, ChangesetsStale: 8, DriftedClaims: 9, BrainClaims: 10,
	}
	after := metrics.Snapshot{
		LintErrors: 2, LintWarnings: 4, ReviewQueue: 6, ReviewOverdue: 8,
		MedianClaimAgeDays: 10, ClaimsMeasured: 1, BrainCitable: 12,
		ChangesetsOpen: 14, ChangesetsStale: 16, DriftedClaims: 18, BrainClaims: 20,
	}

	// Act
	trends := metrics.Compare(after, []metrics.Snapshot{before})

	// Assert
	for _, trend := range trends {
		if trend.Name == "rework rate" {
			continue
		}
		if trend.Delta == "—" {
			t.Errorf("%q reported no change between two different snapshots; it has no case in previousValue", trend.Name)
		}
	}
}

func TestDriftedEvidenceIsMeasured(t *testing.T) {
	// Arrange
	current := metrics.Snapshot{DriftedClaims: 3, BrainClaims: 10}

	// Act
	trends := metrics.Compare(current, nil)

	// Assert
	var found bool
	for _, trend := range trends {
		if strings.Contains(trend.Name, "drift") {
			found = true
			if trend.Current != "3" {
				t.Errorf("current = %q; want 3", trend.Current)
			}
		}
	}
	if !found {
		t.Error("nothing measures how much of the knowledge layer is pinned to evidence that moved")
	}
}

func TestDriftedEvidenceOverNoClaimsIsNotZero(t *testing.T) {
	// Arrange: a workspace with no current-state claims at all has not achieved
	// perfect provenance. Printed as 0 it is the most flattering reading
	// available, which is the failure the other empty populations already guard.
	current := metrics.Snapshot{DriftedClaims: 0, BrainClaims: 0}

	// Act
	trends := metrics.Compare(current, nil)

	// Assert
	for _, trend := range trends {
		if strings.Contains(trend.Name, "drift") && trend.Current != "—" {
			t.Errorf("current = %q; a measure over an empty population is not a zero", trend.Current)
		}
	}
}
