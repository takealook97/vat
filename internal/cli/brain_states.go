package cli

import (
	"context"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/ui"
)

// The half of the lifecycle that had states, check rules, and review-queue
// weights, but no command — so reaching a quarantine meant hand-editing the
// YAML of the record whose trustworthiness was already in doubt.

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
