package cli

import (
	"context"
	"fmt"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/ui"
)

// The half of the lifecycle that had states, check rules, and review-queue
// weights, but no command — so reaching a quarantine meant hand-editing the
// YAML of the record whose trustworthiness was already in doubt. Plus the
// archive, which is what keeps the entry point a fixed-size place to start.

func brainQuarantineCommand() *Command {
	return &Command{
		Name:    "quarantine",
		Summary: "Withhold a suspect record from answers, keeping why",
		Usage:   `vat brain quarantine <id> --reason "..."`,
		Long: `Mark a record as suspected wrong or contaminated.

A claim can be suspect without being disproven, and deleting it would destroy
the record of why it was doubted. Quarantine withholds it from answers instead,
and lifts it above unreviewed items in the queue, because a suspect claim may
already have been quoted as fact.

The reason is required. A withdrawal nobody explained cannot be reviewed,
reversed, or learned from.`,
		Run: retireRun(brain.StatusQuarantined, true),
	}
}

func brainRevokeCommand() *Command {
	return &Command{
		Name:    "revoke",
		Summary: "Withdraw a record, keeping the tombstone",
		Usage:   `vat brain revoke <id> --reason "..."`,
		Long: `Withdraw a claim that turned out to be wrong.

The record is kept, not deleted: a cached answer or an external index that still
quotes it has to be traceable back to the retraction. An end state is one-way —
if the claim turns out to hold after all, record a new one rather than reviving
this.`,
		Run: retireRun(brain.StatusRevoked, true),
	}
}

func brainResolveCommand() *Command {
	return &Command{
		Name:    "resolve",
		Summary: "Close a gap that no longer exists",
		Usage:   `vat brain resolve <id> [--reason "..."]`,
		Long: `Mark a gap as closed.

Only a gap can be resolved: the status describes a distance that has been
removed, which is not something a goal, a decision, or an observation has.`,
		Run: retireRun(brain.StatusResolved, false),
	}
}

// retireRun builds the three commands that move a record out of the answerable
// set. They differ only in the status they write and whether a cause is
// mandatory, so writing them out three times would be three places for the
// refusals to drift apart.
func retireRun(status brain.Status, reasonRequired bool) func(context.Context, *Env, []string) error {
	return func(ctx context.Context, env *Env, args []string) error {
		set := newFlagSet("brain " + string(status))
		reason := set.String("reason", "", "why, in one sentence")
		if err := parseFlags(set, args); err != nil {
			return err
		}
		if set.NArg() != 1 {
			return usageErrorf("expected exactly one record identifier")
		}
		if reasonRequired && *reason == "" {
			return usageErrorf("--reason is required: a %s record nobody explained cannot be reviewed later", status)
		}
		_, store, err := openBrain(env)
		if err != nil {
			return err
		}
		record, ok := store.ByID()[set.Arg(0)]
		if !ok {
			return usageErrorf("no record with id %q", set.Arg(0))
		}
		if err := brain.Retire(store.Root, record, status, *reason); err != nil {
			return err
		}
		env.Printer.Status(ui.LevelOK, record.ID, string(status))
		env.Printer.Hint("Run `vat brain build` to refresh the index.")
		return nil
	}
}

func brainArchiveCommand() *Command {
	return &Command{
		Name:    "archive",
		Summary: "Move records that reached an end state out of the working set",
		Usage:   "vat brain archive [--apply]",
		Long: `Move superseded, revoked, and resolved records into archive/.

Without --apply nothing is written; the moves are only listed.

Nothing is deleted, and an archived record is still loaded — the supersession
chain it belongs to is still checked from both ends. What changes is where it
lives, and two things depend on that. The index is meant to be a fixed-size
place to start a question, which it cannot be while every record ever written
stays in the working directories. And an external search index can only exclude
withdrawn and replaced claims cheaply — by directory — if they are in one.

The relative links inside a moved record are repointed so they still resolve.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain archive")
			apply := set.Bool("apply", false, "write the moves")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			_, store, err := openBrain(env)
			if err != nil {
				return err
			}
			moves, err := brain.Archive(store, *apply)
			if err != nil {
				return err
			}
			if env.JSON {
				return emitJSON(env, moves)
			}
			if len(moves) == 0 {
				env.Printer.Status(ui.LevelOK, "archive", "the working set holds no finished records")
				return nil
			}
			for _, move := range moves {
				level := ui.LevelWarn
				if move.Applied {
					level = ui.LevelOK
				}
				env.Printer.Status(level, move.ID, fmt.Sprintf("%s → %s", move.Status, move.To))
			}
			if !*apply {
				env.Printer.Hint("\nNothing written. Re-run with --apply to move these.")
				return nil
			}
			env.Printer.Hint("\n%s archived. Run `vat brain build` to refresh the index.",
				pluralise(len(moves), "record", "records"))
			return nil
		},
	}
}
