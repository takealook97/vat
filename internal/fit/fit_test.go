package fit_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/fit"
)

func verdictFor(verdicts []fit.Verdict, layer string) (fit.Verdict, bool) {
	for _, verdict := range verdicts {
		if verdict.Layer == layer {
			return verdict, true
		}
	}
	return fit.Verdict{}, false
}

func TestASoloDeveloperWithTwoRepositoriesIsToldToAdoptNothing(t *testing.T) {
	// Arrange
	signals := fit.Signals{Repositories: 2, Contracts: 0, People: 1}

	// Act
	verdicts := fit.Assess(signals)

	// Assert
	for _, verdict := range verdicts {
		if verdict.Adopt {
			t.Errorf("%s was recommended at a scale where it is pure overhead", verdict.Layer)
		}
	}
	if !strings.Contains(fit.Summary(verdicts), "None of this pays for itself") {
		t.Errorf("summary should say to adopt nothing: %q", fit.Summary(verdicts))
	}
}

func TestCrossRepositoryContractsAreWhatTriggerChangesets(t *testing.T) {
	// Arrange: repository count alone is not the driver.
	many := fit.Signals{Repositories: 9, Contracts: 0, People: 1}
	few := fit.Signals{Repositories: 3, Contracts: 4, People: 1}

	// Act
	withoutContracts, _ := verdictFor(fit.Assess(many), fit.LayerChangesets)
	withContracts, _ := verdictFor(fit.Assess(few), fit.LayerChangesets)

	// Assert
	if withoutContracts.Adopt {
		t.Error("changesets recommended with no cross-repository contracts")
	}
	if !withContracts.Adopt {
		t.Error("changesets not recommended despite four cross-repository contracts")
	}
}

func TestALostDecisionAloneJustifiesTheKnowledgeLayer(t *testing.T) {
	// Arrange: the cost is already being paid, so scale does not matter.
	signals := fit.Signals{Repositories: 2, People: 1, DecisionsLost: true}

	// Act
	verdict, found := verdictFor(fit.Assess(signals), fit.LayerBrain)

	// Assert
	if !found || !verdict.Adopt {
		t.Fatal("the knowledge layer was not recommended after a decision was already lost")
	}
	if !strings.Contains(verdict.Because, "re-argued") {
		t.Errorf("the reason should name the cost already paid: %q", verdict.Because)
	}
}

func TestSecretsInASinglePlaceDoNotJustifyACredentialRepository(t *testing.T) {
	// Arrange
	one := fit.Signals{Repositories: 6, SecretRepos: 1}
	two := fit.Signals{Repositories: 6, SecretRepos: 2}

	// Act
	single, _ := verdictFor(fit.Assess(one), fit.LayerCredential)
	multiple, _ := verdictFor(fit.Assess(two), fit.LayerCredential)

	// Assert
	if single.Adopt {
		t.Error("a credential repository was recommended for a single secret location")
	}
	if !multiple.Adopt {
		t.Error("a credential repository was not recommended when secrets live in two places")
	}
}

func TestEveryLayerCarriesAThresholdAndAStartingCommand(t *testing.T) {
	// Act
	verdicts := fit.Assess(fit.Signals{Repositories: 6, Contracts: 3, People: 3, AgentSessions: 5, SecretRepos: 3})

	// Assert
	for _, verdict := range verdicts {
		if verdict.Threshold == "" {
			t.Errorf("%s has no stated threshold", verdict.Layer)
		}
		if verdict.Because == "" {
			t.Errorf("%s has no stated reason", verdict.Layer)
		}
		if verdict.Adopt && verdict.Command == "" {
			t.Errorf("%s is recommended with no starting command", verdict.Layer)
		}
	}
}

func TestSummaryReadsCorrectlyForASingleLayer(t *testing.T) {
	// Arrange: "Adopt workspace now, in that order" is not a sentence.
	verdicts := fit.Assess(fit.Signals{Repositories: 4, People: 1})

	// Act
	summary := fit.Summary(verdicts)

	// Assert
	if strings.Contains(summary, "in that order") {
		t.Errorf("a single-layer summary claims an ordering: %q", summary)
	}
	if !strings.Contains(summary, "Adopt workspace now.") {
		t.Errorf("summary = %q", summary)
	}
}

func TestSummaryKeepsTheOrderingWhenSeveralLayersApply(t *testing.T) {
	// Arrange
	verdicts := fit.Assess(fit.Signals{
		Repositories: 6, Contracts: 3, People: 3, AgentSessions: 5, SecretRepos: 3,
	})

	// Act
	summary := fit.Summary(verdicts)

	// Assert
	if !strings.Contains(summary, "Every layer is justified") && !strings.Contains(summary, "in that order") {
		t.Errorf("a multi-layer summary lost its ordering: %q", summary)
	}
}
