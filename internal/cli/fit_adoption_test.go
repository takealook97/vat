package cli

import (
	"strings"
	"testing"
)

// The pure advisor tests cannot see this: they are handed the adopted layers
// rather than reading them, so a detector pointed at the wrong directory passes
// every one of them and still reports a workspace holding 222 completion
// records as having adopted no changesets. These run the command against a real
// workspace, which is the only place the reading can be wrong.

func TestFitSeesTheLayersTheWorkspaceActuallyRuns(t *testing.T) {
	// Arrange
	h := brainFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments,console")

	// Act
	output := h.mustRun("fit")

	// Assert
	for _, layer := range []string{"workspace", "changesets", "brain"} {
		if !strings.Contains(output, layer) {
			t.Fatalf("fit says nothing about %s:\n%s", layer, output)
		}
	}
	for _, line := range strings.Split(output, "\n") {
		for _, layer := range []string{"workspace", "changesets", "brain"} {
			if strings.Contains(line, layer) && strings.Contains(line, "not yet") {
				t.Errorf("%s is in use and fit reported it as not yet reached:\n%s", layer, line)
			}
		}
	}
}

func TestFitCountsContractsFromTheChangesetsThatRecordThem(t *testing.T) {
	// Arrange: a changeset naming two repositories is recorded evidence that an
	// interface crossed a boundary. Asking somebody to count their own
	// contracts is asking for a number vat is already holding.
	h := adoptedFixture(t, "payments", "console")
	h.mustRun("changeset", "new", "Move cancellation to v2", "--repos", "payments,console")
	h.mustRun("changeset", "new", "Refunds settle next day", "--repos", "payments,console")

	// Act
	output := h.mustRun("fit")

	// Assert
	if strings.Contains(output, "count them with --contracts") {
		t.Errorf("fit asked for a count it could read from the changesets:\n%s", output)
	}
}
