package fit_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/fit"
)

// The advisor answered one question — should you adopt this — and kept
// answering it after the answer was yes. Three workspaces running twelve
// repositories, three hundred records, and an encrypted credential repository
// were each told to `vat init`, and the one that had consolidated its secrets
// into a single repository was told to skip the credential layer, because
// succeeding at that consolidation is what drives the signal to one.
//
// After adoption the question is not "is this worth starting" but "is this
// still earning its keep", and an advisor that cannot tell the two apart gives
// advice that is exactly backwards.

func TestAnAdoptedLayerIsNotAdvisedToBeAdopted(t *testing.T) {
	// Arrange
	signals := fit.Signals{
		Repositories: 12, Contracts: 6, People: 3, AgentSessions: 9, SecretRepos: 1,
		Adopted: []string{fit.LayerWorkspace, fit.LayerBrain, fit.LayerCredential},
	}

	// Act
	verdicts := fit.Assess(signals)

	// Assert
	for _, layer := range []string{fit.LayerWorkspace, fit.LayerBrain, fit.LayerCredential} {
		verdict, ok := verdictFor(verdicts, layer)
		if !ok {
			t.Fatalf("no verdict for %s", layer)
		}
		if !verdict.Adopted {
			t.Errorf("%s is in use and the advisor did not notice", layer)
		}
		if verdict.Adopt {
			t.Errorf("%s is already adopted and the advisor said to adopt it", layer)
		}
	}
}

func TestAnAdoptedLayerBelowItsThresholdIsNotAdvisedAgainst(t *testing.T) {
	// Arrange: one credential repository is what a workspace that consolidated
	// its secrets looks like. Reading that as "secrets live in one place, so
	// this layer would not pay" advises undoing the thing that worked.
	signals := fit.Signals{
		Repositories: 12, SecretRepos: 1, Adopted: []string{fit.LayerCredential},
	}

	// Act
	verdicts := fit.Assess(signals)

	// Assert
	verdict, _ := verdictFor(verdicts, fit.LayerCredential)
	if strings.Contains(verdict.Because, "no repository here is declared as holding secrets") {
		t.Errorf("because = %q; the workspace declares one and that is why the count is one", verdict.Because)
	}
	if verdict.Command != "" {
		t.Errorf("command = %q; there is nothing to start", verdict.Command)
	}
}

func TestAFullyAdoptedWorkspaceIsToldSo(t *testing.T) {
	// Arrange
	signals := fit.Signals{
		Repositories: 12, Contracts: 6, People: 3, AgentSessions: 9, SecretRepos: 1,
		Adopted: []string{
			fit.LayerWorkspace, fit.LayerHarness, fit.LayerChangesets,
			fit.LayerBrain, fit.LayerCredential,
		},
	}

	// Act
	summary := fit.Summary(fit.Assess(signals))

	// Assert
	if strings.Contains(summary, "Adopt") {
		t.Errorf("summary = %q; every layer is already in use", summary)
	}
	if !strings.Contains(summary, "in use") {
		t.Errorf("summary = %q; it does not say what the workspace actually is", summary)
	}
}

func TestAnUnadoptedWorkspaceStillGetsTheOriginalAdvice(t *testing.T) {
	// Arrange: the question this command was written for has not gone away.
	signals := fit.Signals{Repositories: 2, People: 1}

	// Act
	verdicts := fit.Assess(signals)

	// Assert
	for _, verdict := range verdicts {
		if verdict.Adopt || verdict.Adopted {
			t.Errorf("%s was recommended to a solo developer with two repositories", verdict.Layer)
		}
	}
	if !strings.Contains(fit.Summary(verdicts), "None of this pays for itself") {
		t.Errorf("summary = %q", fit.Summary(verdicts))
	}
}
