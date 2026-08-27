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
	Layer     string `json:"layer"`
	Adopt     bool   `json:"adopt"`
	Threshold string `json:"threshold"`
	Because   string `json:"because"`
	Command   string `json:"command"`
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
			Layer: LayerHarness,
			// Deliberately not gated on repository count. The problems this
			// layer solves — one role body drifting into two runtime adapters,
			// a contract that has to hold wherever a session was opened, a
			// written trust boundary — all exist with a single repository. The
			// trigger is whether agents touch the code at all.
			Adopt:     signals.AgentSessions >= 1,
			Threshold: "coding agents work in this code at all",
			Because: because(signals.AgentSessions >= 1,
				harnessReason(signals),
				// Not "there are no agents": nothing here can see that. Zero is
				// what Signals calls unknown, and an advisor that states a fact
				// about somebody's situation from a flag default has stopped
				// advising. Name what would settle it instead.
				"nothing here says agents work in this code; if any do, this layer pays from the first one — pass --agent-sessions"),
			Command: "vat harness render",
		},
		{
			Layer:     LayerChangesets,
			Adopt:     signals.Contracts >= 2,
			Threshold: "2 or more interfaces cross a repository boundary",
			Because: because(signals.Contracts >= 2,
				fmt.Sprintf("%d cross-repository contracts: you already cannot say which revisions were verified together",
					signals.Contracts),
				// Contracts is never read from the workspace — an interface that
				// crosses a boundary is not something a manifest records — so
				// zero here is always "you have not said".
				"nothing here says an interface crosses a repository boundary; count them with --contracts"),
			Command: "vat changeset new",
		},
		{
			Layer: LayerBrain,
			// An agent that re-derives a settled decision costs the same as a
			// person who forgot it, and does so far more often, so heavy agent
			// use reaches the threshold on its own.
			Adopt: signals.DecisionsLost ||
				signals.AgentSessions >= 5 ||
				(signals.People >= 2 && signals.Repositories >= 4),
			Threshold: "a decision has already been lost, agents work here weekly, or 2+ people across 4+ repositories",
			Because: because(signals.DecisionsLost ||
				signals.AgentSessions >= 5 ||
				(signals.People >= 2 && signals.Repositories >= 4),
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

// harnessReason explains what the harness is buying at this scale.
func harnessReason(signals Signals) string {
	if signals.Repositories >= 3 {
		return fmt.Sprintf(
			"agents open sessions across %d repositories and need to find the same boundary rules in each one",
			signals.Repositories)
	}
	return "one role body, generated into every runtime you use, and checked for drift — two hand-maintained copies diverge within weeks and nobody diffs a prompt"
}

func brainReason(signals Signals) string {
	if signals.DecisionsLost {
		return "a decision has already been re-argued because nobody could find the original reasoning; that cost recurs"
	}
	if signals.AgentSessions >= 5 {
		return fmt.Sprintf(
			"roughly %d agent sessions a week, each starting without what the last one settled; re-deriving a decision costs the same whoever forgets it",
			signals.AgentSessions)
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
