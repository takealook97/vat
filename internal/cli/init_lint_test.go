package cli

import (
	"strings"
	"testing"
)

// `vat init` seeds two procedures, and a skill is a harness definition, so a
// rule that counts every skill as adoption fires on the workspace vat has just
// finished creating — for content vat wrote itself, on the first command a new
// user runs. Nothing the reader did caused it and no repair is theirs to make,
// which is how a rule set teaches people to skip its warnings.
//
// The repair is not vat's either. `workspace.checks` is the evidence
// `vat changeset verify` consumes, so seeding a value there to quiet this would
// record the control plane as proven by a check nobody chose —
// TestAWorkspaceWithNoChecksIsUnverifiableRatherThanVerified holds the other
// end of that line.
func TestAFreshlyInitialisedWorkspaceIsNotWarnedAboutTheLayerItWasSeeded(t *testing.T) {
	// Arrange
	h := newFixture(t)

	// Act
	h.mustRun("init", "--name", "acme")
	_, output := h.run("lint")

	// Assert
	if strings.Contains(output, "workspace/layer-unchecked") {
		t.Errorf("a workspace vat had just created was warned about a layer vat seeded into it:\n%s", output)
	}
}
