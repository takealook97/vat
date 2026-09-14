package cli

import (
	"context"
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fit"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/harness"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

func fitCommand() *Command {
	return &Command{
		Name:    "fit",
		Summary: "Decide which layers are worth adopting yet",
		Usage:   "vat fit [--repos n] [--contracts n] [--people n] [--agent-sessions n] [--secret-repos n] [--decisions-lost]",
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

	for name, value := range map[string]int{
		"--repos": *repos, "--contracts": *contracts, "--people": *people,
		"--agent-sessions": *sessions, "--secret-repos": *secretRepos,
	} {
		if value < 0 {
			return usageErrorf("%s cannot be negative", name)
		}
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
		signals.Adopted = adoptedLayers(ws)
		if signals.Repositories == 0 {
			signals.Repositories = len(ws.Manifest.Active())
		}
		if signals.SecretRepos == 0 {
			signals.SecretRepos = countCredentialRepos(ws)
		}
		// A workspace that already defines agent roles is one where agents are
		// in the loop, whatever the caller typed. Advising against a layer that
		// is visibly already in use reads as the tool not looking.
		//
		// Roles and not skills: `vat init` seeds two procedures into every
		// workspace it creates, so their presence is evidence that vat ran, not
		// that anybody is running agents. Counting them would make this signal
		// true everywhere and therefore worth nothing. A role is written by
		// hand, with `vat harness role new`.
		if signals.AgentSessions == 0 && definesRoles(ws) {
			signals.AgentSessions = 1
		}
		// A changeset naming two or more repositories is a recorded fact that an
		// interface crossed a boundary. The comment above this signal used to
		// say it could never be read from the workspace, and that was true
		// before completion records existed: asking somebody to count their own
		// contracts, in a workspace holding 222 of these, is asking for a number
		// vat is already holding.
		if signals.Contracts == 0 {
			signals.Contracts = countCrossRepositoryChangesets(ws)
		}
		// Defaults to one, so only a larger count is worth reading. Authors
		// across the governed repositories is what "people working across more
		// than one repository" actually means.
		if signals.People <= 1 {
			if authors := countAuthors(ctx, ws); authors > signals.People {
				signals.People = authors
			}
		}
	} else if signals.Repositories == 0 {
		return usageErrorf("not in a workspace; pass --repos to describe your situation")
	}

	verdicts := fit.Assess(signals)
	if env.JSON {
		return emitJSON(env, verdicts)
	}

	for _, verdict := range verdicts {
		level := ui.LevelSkip
		state := "not yet"
		switch {
		case verdict.Adopted:
			level = ui.LevelInfo
			state = "in use"
		case verdict.Adopt:
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

// definesRoles reports whether the workspace has any agent role defined.
func definesRoles(ws *workspace.Workspace) bool {
	// A role file nobody can parse is still evidence that somebody wrote one.
	roles, malformed, err := harness.LoadRoles(ws.Root)
	return err == nil && len(roles)+len(malformed) > 0
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

// adoptedLayers names the layers this workspace already runs.
//
// Each is read from something the workspace actually holds rather than from a
// flag, because the failure being fixed is an advisor telling a workspace of
// twelve repositories and three hundred records to run `vat init`.
func adoptedLayers(ws *workspace.Workspace) []string {
	// Reaching this function at all means a manifest was found and parsed.
	layers := []string{fit.LayerWorkspace}
	if definesRoles(ws) {
		layers = append(layers, fit.LayerHarness)
	}
	if sets, err := changeset.LoadAll(ws.ChangesetsDir()); err == nil && len(sets) > 0 {
		layers = append(layers, fit.LayerChangesets)
	}
	// Declared is not adopted: a repository named as the brain but never
	// initialised holds no records, and `vat doctor` already says so rather
	// than treating the declaration as the layer.
	if root, ok := ws.BrainPath(); ok && brain.IsBrain(root) {
		layers = append(layers, fit.LayerBrain)
	}
	if countCredentialRepos(ws) > 0 {
		layers = append(layers, fit.LayerCredential)
	}
	return layers
}

// countCrossRepositoryChangesets counts completion records naming two or more
// repositories. Each is evidence that an interface crossed a boundary, which is
// the thing --contracts asks the caller to count by hand.
func countCrossRepositoryChangesets(ws *workspace.Workspace) int {
	sets, err := changeset.LoadAll(ws.ChangesetsDir())
	if err != nil {
		return 0
	}
	count := 0
	for _, set := range sets {
		if len(set.Repositories) >= 2 {
			count++
		}
	}
	return count
}

// countAuthors counts distinct commit authors across the governed repositories.
//
// It is a floor, not a headcount: a shared machine, a squashed history, or a
// repository nobody has cloned all read low. Reading low is the safe direction
// — it advises a layer later rather than sooner, which is the whole posture of
// this command.
func countAuthors(ctx context.Context, ws *workspace.Workspace) int {
	seen := map[string]struct{}{}
	for _, repo := range ws.Manifest.Active() {
		dir := ws.RepoPath(repo)
		if !gitx.IsRepository(dir) {
			continue
		}
		out, err := gitx.Run(ctx, dir, "log", "--format=%ae", "-n", "200")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			if email := strings.TrimSpace(line); email != "" {
				seen[email] = struct{}{}
			}
		}
	}
	return len(seen)
}
