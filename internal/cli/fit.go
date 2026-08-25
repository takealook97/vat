package cli

import (
	"context"
	"encoding/json"

	"github.com/takealook97/vat/internal/fit"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

func fitCommand() *Command {
	return &Command{
		Name:    "fit",
		Summary: "Decide which layers are worth adopting yet",
		Usage:   "vat fit [--repos n] [--contracts n] [--people n] [--decisions-lost]",
		Long: `Say where the break-even point is, per layer.

Every layer here is overhead until the problem it solves is real. One developer
with two repositories who adopts a knowledge repository, a credential
repository, and cross-repository changesets has bought ceremony and no benefit —
and will abandon all of it within a month.

Numbers are read from the workspace where they can be, and taken from flags
where they cannot. --contracts is the important one: how many interfaces cross a
repository boundary. That, not repository count, is what makes a multi-repo
layout expensive.`,
		Examples: []string{
			"vat fit --contracts 3 --people 4",
			"vat fit --repos 2 --contracts 0     # check whether you need any of this",
		},
		Run: runFit,
	}
}

func runFit(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("fit")
	repos := set.Int("repos", 0, "repositories worked in together (default: read from the manifest)")
	contracts := set.Int("contracts", 0, "interfaces that cross a repository boundary")
	people := set.Int("people", 1, "people working across more than one repository")
	sessions := set.Int("agent-sessions", 0, "coding-agent sessions per week")
	secretRepos := set.Int("secret-repos", 0, "repositories that hold their own secrets")
	decisionsLost := set.Bool("decisions-lost", false, "a decision has already been re-argued because nobody could find the reasoning")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	signals := fit.Signals{
		Repositories:  *repos,
		Contracts:     *contracts,
		People:        *people,
		AgentSessions: *sessions,
		SecretRepos:   *secretRepos,
		DecisionsLost: *decisionsLost,
	}
	// Reading what can be read keeps the advice grounded in the actual
	// workspace rather than in what someone typed.
	if ws, err := env.Workspace(); err == nil {
		if signals.Repositories == 0 {
			signals.Repositories = len(ws.Manifest.Active())
		}
		if signals.SecretRepos == 0 {
			signals.SecretRepos = countCredentialRepos(ws)
		}
	} else if signals.Repositories == 0 {
		return usageErrorf("not in a workspace; pass --repos to describe your situation")
	}

	verdicts := fit.Assess(signals)
	if env.JSON {
		encoder := json.NewEncoder(env.Printer.Out())
		encoder.SetIndent("", "  ")
		return encoder.Encode(verdicts)
	}

	for _, verdict := range verdicts {
		level := ui.LevelSkip
		state := "not yet"
		if verdict.Adopt {
			level = ui.LevelOK
			state = "adopt"
		}
		env.Printer.Status(level, verdict.Layer, state+" — "+verdict.Because)
		env.Printer.Hint("      threshold: %s", verdict.Threshold)
		if verdict.Adopt {
			env.Printer.Hint("      start with: %s", verdict.Command)
		}
	}
	env.Printer.Heading("Conclusion")
	env.Printer.Println(fit.Summary(verdicts))
	return nil
}

func countCredentialRepos(ws *workspace.Workspace) int {
	count := 0
	for _, repo := range ws.Manifest.Active() {
		if repo.Role == manifest.RoleCredential {
			count++
		}
	}
	return count
}
