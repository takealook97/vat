package doctor_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/doctor"
)

// The brain section read the review queue and nothing else, so a workspace
// whose claims were pinned to evidence that had moved was told its knowledge
// layer was in order. Both sides are pinned here: a caller that asked the
// question and got an answer, and one that did not ask.

func TestDoctorWarnsAboutClaimsWhoseEvidenceMoved(t *testing.T) {
	// Arrange
	ws, root := brainFixture(t)
	mustCreateRecord(t, root, "G-0001", "Ordering is not retry-safe", brain.StatusActive)

	// Act
	report := doctor.Run(t.Context(), ws, doctor.Options{
		Now: reference, DriftedClaims: []string{"G-0001"},
	})

	// Assert
	finding, found := findingFor(report, "brain", "evidence")
	if !found {
		t.Fatalf("doctor said nothing about evidence that moved: %+v", report.Findings)
	}
	if finding.Status != doctor.StatusWarn {
		t.Errorf("status = %q; a claim awaiting re-verification is not a broken environment", finding.Status)
	}
	if !strings.Contains(finding.Fix, "--drifted") {
		t.Errorf("fix = %q; it names no way to see what moved", finding.Fix)
	}
}

func TestDoctorSaysNothingAboutEvidenceNobodyAskedAbout(t *testing.T) {
	// Arrange: resolving a revision is the caller's job, and a caller that did
	// not do it must not be handed a cheerful report that nothing moved.
	ws, root := brainFixture(t)
	mustCreateRecord(t, root, "G-0001", "Ordering is not retry-safe", brain.StatusActive)

	// Act
	report := judge(t, ws)

	// Assert
	if finding, found := findingFor(report, "brain", "evidence"); found {
		t.Errorf("doctor answered a question it was never given the input for: %+v", finding)
	}
}
