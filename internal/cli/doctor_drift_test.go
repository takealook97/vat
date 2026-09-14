package cli

import (
	"strings"
	"testing"
)

// `vat doctor` is the command a person runs first, and its verdict is the one
// they act on. It read the review queue and nothing else, so a workspace
// carrying 46 claims pinned to evidence that had moved — one of them 213
// commits behind — was told its knowledge layer was in order. A diagnosis that
// certifies the state it exists to find is worse than one that says nothing.

func TestDoctorReportsClaimsWhoseEvidenceMoved(t *testing.T) {
	// Arrange
	h := brainFixture(t, "payments")
	driftedClaim(t, h, "payments")

	// Act
	code, output := h.run("doctor")

	// Assert
	if !strings.Contains(output, "evidence") {
		t.Fatalf("doctor said nothing about a claim whose evidence moved:\n%s", output)
	}
	if !strings.Contains(output, "vat brain review --drifted") {
		t.Errorf("doctor named no way to see what drifted:\n%s", output)
	}
	if code == ExitOK && !strings.Contains(output, "WARN") {
		t.Errorf("drifted evidence was reported at no severity at all:\n%s", output)
	}
}

func TestDoctorSaysNothingAboutDriftWhenEveryClaimIsCurrent(t *testing.T) {
	// Arrange: the other half. A finding that appears on a healthy workspace
	// teaches people to skip the section it lives in.
	h := brainFixture(t, "payments")
	h.mustRun("brain", "new", "gap", "--title", "Ordering is not retry-safe",
		"--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	h.mustRun("brain", "promote", "G-0001", "--reviewer", "alex")

	// Act
	output := h.mustRun("doctor")

	// Assert
	if strings.Contains(output, "evidence") {
		t.Errorf("doctor reported drift in a workspace where nothing moved:\n%s", output)
	}
}
