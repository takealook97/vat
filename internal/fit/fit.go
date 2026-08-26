// Package fit answers the question a methodology document should answer first
// and almost never does: at what point is this worth its cost?
//
// Every layer here is overhead until the problem it solves is real. A single
// developer with two repositories who adopts a knowledge repository, a
// credential repository, and cross-repository changesets has bought ceremony
// and no benefit. Naming the threshold for each layer is what stops that.
package fit

import (
	"fmt"
	"strings"
)

// Signals describe a team's situation. Anything left at zero is treated as
// unknown and the recommendation says so rather than guessing.
type Signals struct {
	// Repositories actively worked in.
	Repositories int
	// Contracts is how many interfaces cross a repository boundary — an API
	// one repository serves and another consumes, a shared schema, a published
	// package. This, not repository count, is what makes multi-repo expensive.
	Contracts int
	// People working across more than one repository.
	People int
	// AgentSessions is roughly how many coding-agent sessions run per week.
	AgentSessions int
	// SecretRepos is how many repositories currently hold their own secrets.
	SecretRepos int
	// DecisionsLost reports whether the team has already had to re-litigate a
	// decision nobody could find. It is the strongest single signal for the
	// knowledge layer, because it means the cost is already being paid.
	DecisionsLost bool
}

// Verdict is a recommendation about one layer.
type Verdict struct {
	Layer     string
	Adopt     bool
	Threshold string
	Because   string
	Command   string
}

// Layer names, in adoption order. Each one assumes the ones before it.
const (
	LayerWorkspace  = "workspace"
	LayerHarness    = "harness"
	LayerChangesets = "changesets"
	LayerBrain      = "brain"
	LayerCredential = "credential"
)

// Assess returns a verdict per layer, in the order they should be adopted.
//
// The ordering is not cosmetic. Adopting the knowledge layer before the
// workspace is stable produces records about a state nobody can reproduce;
// adopting semantic search before canonical ownership produces fast answers
// with no way to tell which one is true.
func Assess(signals Signals) []Verdict {
	verdicts := []Verdict{
		{
			Layer:     LayerWorkspace,
			Adopt:     signals.Repositories >= 3,
			Threshold: "3 or more repositories worked in together",
			Because: because(signals.Repositories >= 3,
				fmt.Sprintf("%d repositories: knowing what to clone, and what state each is in, has stopped being memorable",
					signals.Repositories),
				"with one or two repositories you can hold the whole picture without a manifest"),
			Command: "vat init",
		},
		{
			Layer:     LayerHarness,
			Adopt:     signals.Repositories >= 3 && signals.AgentSessions >= 1,
			Threshold: "agents work across more than one repository",
			Because: because(signals.Repositories >= 3 && signals.AgentSessions >= 1,
				"an agent opening a session in any repository needs to find the same boundary rules there",
				"without agents in the loop, a written contract per repository is enough"),
			Command: "vat harness render",
		},
		{
			Layer:     LayerChangesets,
			Adopt:     signals.Contracts >= 2,
			Threshold: "2 or more interfaces cross a repository boundary",
			Because: because(signals.Contracts >= 2,
				fmt.Sprintf("%d cross-repository contracts: you already cannot say which revisions were verified together",
					signals.Contracts),
				"with no shared contracts, each repository's own history is a complete record"),
			Command: "vat changeset new",
		},
		{
			Layer:     LayerBrain,
			Adopt:     signals.DecisionsLost || signals.People >= 2 && signals.Repositories >= 4,
			Threshold: "a decision has already been lost, or 2+ people across 4+ repositories",
			Because: because(signals.DecisionsLost || (signals.People >= 2 && signals.Repositories >= 4),
				brainReason(signals),
				"one person across a few repositories still remembers why; the layer would store what you already know"),
			Command: "vat brain init",
		},
		{
			Layer:     LayerCredential,
			Adopt:     signals.SecretRepos >= 2,
			Threshold: "2 or more repositories hold their own secrets",
			Because: because(signals.SecretRepos >= 2,
				fmt.Sprintf("secrets live in %d places: nobody can say which copy is current, and a new machine cannot be rebuilt reliably",
					signals.SecretRepos),
				"a single secret location is still auditable by looking at it"),
			Command: "vat repo new credential --role credential --private",
		},
	}
	return verdicts
}

func brainReason(signals Signals) string {
	if signals.DecisionsLost {
		return "a decision has already been re-argued because nobody could find the original reasoning; that cost recurs"
	}
	return fmt.Sprintf("%d people across %d repositories: what each of you believes is true has started to differ",
		signals.People, signals.Repositories)
}

func because(adopt bool, yes, no string) string {
	if adopt {
		return yes
	}
	return no
}

// Summary renders a one-paragraph conclusion.
func Summary(verdicts []Verdict) string {
	var adopt []string
	for _, verdict := range verdicts {
		if verdict.Adopt {
			adopt = append(adopt, verdict.Layer)
		}
	}
	switch len(adopt) {
	case 0:
		return "None of this pays for itself yet. Keep the repositories as they are and " +
			"re-run this when a second interface starts crossing a boundary, or when you " +
			"first cannot find why something was decided."
	case len(verdicts):
		return "Every layer is justified. Adopt them in the order listed — each one assumes " +
			"the one before it is already stable."
	case 1:
		return fmt.Sprintf("Adopt %s now. Leave the rest until its threshold is met; "+
			"adopting a layer early costs ceremony and buys nothing.", adopt[0])
	default:
		return fmt.Sprintf("Adopt %s now, in that order. Leave the rest until each "+
			"threshold is met; adopting a layer early costs ceremony and buys nothing.",
			strings.Join(adopt, ", then "))
	}
}
