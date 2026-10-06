package cli

import (
	"context"
	"flag"
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

func changesetRecordCommand() *Command {
	return &Command{
		Name:    "record",
		Summary: "Link the knowledge a changeset produced, or explain why it produced none",
		Usage:   `vat changeset record <id> (--knowledge <ids> | --no-record "<reason>")`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset record")
			knowledge := changesetKnowledgeFlags(set)
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one changeset identifier")
			}
			if err := knowledge.validate(set, true); err != nil {
				return err
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			current, err := changeset.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			current, err = knowledge.apply(ws, current)
			if err != nil {
				return err
			}
			if err := changeset.Save(ws.Root, current); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, current.ID, "knowledge recorded")
			return nil
		},
	}
}

type changesetKnowledge struct {
	ids          *string
	reason       *string
	hasKnowledge bool
	hasReason    bool
}

func changesetKnowledgeFlags(set *flag.FlagSet) *changesetKnowledge {
	return &changesetKnowledge{
		ids:    set.String("knowledge", "", "produced brain record identifiers, comma-separated; appends to knowledge without duplicates"),
		reason: set.String("no-record", "", "why this change produced no knowledge record"),
	}
}

func (k *changesetKnowledge) validate(set *flag.FlagSet, required bool) error {
	set.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "knowledge":
			k.hasKnowledge = true
		case "no-record":
			k.hasReason = true
		}
	})
	if k.hasKnowledge && k.hasReason {
		return usageErrorf("--knowledge and --no-record are mutually exclusive")
	}
	if required && !k.hasKnowledge && !k.hasReason {
		return usageErrorf("provide --knowledge <ids> or --no-record \"<reason>\"")
	}
	if k.hasKnowledge && len(splitList(*k.ids)) == 0 {
		return usageErrorf("--knowledge requires at least one record identifier")
	}
	if k.hasReason && strings.TrimSpace(*k.reason) == "" {
		return usageErrorf("--no-record requires a non-empty reason")
	}
	return nil
}

func (k *changesetKnowledge) apply(ws *workspace.Workspace, current changeset.Changeset) (changeset.Changeset, error) {
	if !k.hasKnowledge && !k.hasReason {
		return current, nil
	}
	if !current.Status.Open() && current.Status != changeset.StatusClosed {
		return current, usageErrorf("%s is already %s; knowledge can be recorded only on open, verified, or closed changesets", current.ID, current.Status)
	}
	if k.hasReason && len(current.Knowledge) > 0 {
		return current, usageErrorf("--no-record refused: knowledge already recorded: %s", strings.Join(current.Knowledge, ", "))
	}
	ids := splitList(*k.ids)
	if root, ok := ws.BrainPath(); k.hasKnowledge && ok {
		store, err := brain.Load(root)
		if err != nil {
			return current, err
		}
		records := store.ByID()
		var unresolved []string
		seen := map[string]bool{}
		for _, id := range ids {
			if _, exists := records[id]; !exists && !seen[id] {
				unresolved = append(unresolved, id)
				seen[id] = true
			}
		}
		if len(unresolved) > 0 {
			return current, usageErrorf("unresolved brain record identifiers: %s", strings.Join(unresolved, ", "))
		}
	}
	return changeset.WithKnowledge(current, ids, strings.TrimSpace(*k.reason)), nil
}
