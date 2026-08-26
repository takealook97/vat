package cli

import (
	"context"
	"strings"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// The changeset command tree, opening a record, and enrolling a repository
// in one — which captures where that repository stood, because after the
// change lands it can no longer be observed.

func changesetCommand() *Command {
	return &Command{
		Name:    "changeset",
		Summary: "Record the evidence for a change that spans several repositories",
		Usage:   "vat changeset <new|add|verify|show|list|close|abandon|undo-plan>",
		Long: `Pay back the cost of choosing many repositories over one.

Separate repositories mean a cross-cutting change is several commits with no
relationship recorded anywhere. Weeks later nobody can say which revisions were
ever verified together, or what to revert if it turns out wrong. That is the
real price of a multi-repo layout, and it is usually never paid.

A changeset is the record that pays it: the repositories involved, the point
each one can return to, the exact revision its checks passed on, and the single
end-to-end outcome that proves the pieces work together.`,
		Examples: []string{
			`vat changeset new "Move order cancellation to v2"`,
			"vat changeset add CS-0001 payments console",
			"vat changeset verify CS-0001",
			`vat changeset close CS-0001 --acceptance "cancel-then-refund passes end to end"`,
		},
		Subcommands: []*Command{
			changesetNewCommand(),
			changesetAddCommand(),
			changesetVerifyCommand(),
			changesetShowCommand(),
			changesetListCommand(),
			changesetCloseCommand(),
			changesetAbandonCommand(),
			changesetUndoPlanCommand(),
		},
	}
}

func changesetNewCommand() *Command {
	return &Command{
		Name:    "new",
		Summary: "Open a changeset",
		Usage:   `vat changeset new "<objective>" [--repos a,b] [--non-goal "..."] [--contract "..."] [--decision D-0001]`,
		Run:     runChangesetNew,
	}
}

func runChangesetNew(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("changeset new")
	repos := set.String("repos", "", "repositories to enrol immediately")
	nonGoals := set.String("non-goal", "", "things explicitly out of scope, comma-separated")
	contracts := set.String("contract", "", "interfaces this change touches")
	decisions := set.String("decision", "", "brain decision identifiers that authorised this")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() == 0 {
		return usageErrorf("expected an objective")
	}
	objective := strings.Join(set.Args(), " ")

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	if err := fsx.EnsureDir(ws.ChangesetsDir()); err != nil {
		return err
	}
	id, err := changeset.NextID(ws.Root)
	if err != nil {
		return err
	}
	current := changeset.New(id, objective, env.Now)
	current.NonGoals = splitList(*nonGoals)
	current.Contracts = splitList(*contracts)
	current.Decisions = splitList(*decisions)

	for _, name := range splitList(*repos) {
		participant, err := enrol(ctx, ws, name)
		if err != nil {
			return err
		}
		current = changeset.WithParticipant(current, participant)
	}
	if err := changeset.Save(ws.Root, current); err != nil {
		return err
	}

	env.Printer.Status(ui.LevelOK, id, changeset.Path(id))
	for _, participant := range current.Repositories {
		env.Printer.Status(ui.LevelInfo, participant.Name,
			"return point "+shortRev(participant.RollbackPoint))
	}
	env.Printer.Hint("\nEnrol more:  vat changeset add %s <repo>...", id)
	env.Printer.Hint("When ready:  vat changeset verify %s", id)
	return nil
}

// enrol records where a repository stands before the change begins. Captured
// now because after the change lands it can no longer be observed.

func enrol(ctx context.Context, ws *workspace.Workspace, name string) (changeset.Participant, error) {
	repo, ok := ws.Manifest.Find(name)
	if !ok {
		return changeset.Participant{}, usageErrorf("%q is not in %s", name, manifest.FileName)
	}
	dir := ws.RepoPath(repo)
	if !gitx.IsRepository(dir) {
		return changeset.Participant{}, usageErrorf("%s is not cloned", name)
	}
	revision, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		return changeset.Participant{}, err
	}
	branch, _ := gitx.CurrentBranch(ctx, dir)
	return changeset.Participant{Name: name, RollbackPoint: revision, Branch: branch}, nil
}

func changesetAddCommand() *Command {
	return &Command{
		Name:    "add",
		Summary: "Enrol more repositories in an open changeset",
		Usage:   "vat changeset add <id> <repo>...",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset add")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() < 2 {
				return usageErrorf("expected a changeset id and at least one repository")
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			current, err := changeset.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			// `abandon` already refused a finished changeset and this did not.
			// Enrolling a repository after the fact rewrites the one claim the
			// record exists to make: which revisions were verified together.
			if !current.Status.Open() {
				return usageErrorf("%s is already %s; a finished changeset records what was verified, so it does not take new repositories",
					current.ID, current.Status)
			}
			for _, name := range set.Args()[1:] {
				if _, exists := current.Participant(name); exists {
					env.Printer.Status(ui.LevelSkip, name, "already enrolled")
					continue
				}
				participant, err := enrol(ctx, ws, name)
				if err != nil {
					return err
				}
				current = changeset.WithParticipant(current, participant)
				env.Printer.Status(ui.LevelOK, name,
					"return point "+shortRev(participant.RollbackPoint))
			}
			return changeset.Save(ws.Root, current)
		},
	}
}

func shortRev(revision string) string {
	if revision == "" {
		return "-"
	}
	if len(revision) > 8 {
		return revision[:8]
	}
	return revision
}
