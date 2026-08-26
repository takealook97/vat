package cli

import (
	"context"
	"fmt"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/ui"
)

// Reading the knowledge layer: the bounded search surface, the re-check
// queue ordered by what it costs to ignore, and the sweep that demotes a
// claim nobody has verified.

func brainQueryCommand() *Command {
	return &Command{
		Name:    "query",
		Summary: "Search the bounded surface for the records that matter",
		Usage:   "vat brain query <terms...> [--all] [--limit n]",
		Long: `Find the few records relevant to a question.

The default surface is deliberately narrow: the generated index, the root
projections, and the non-terminal records. Reading everything makes answers
worse, not better — superseded reasoning and current fact stop being
distinguishable. --all widens the search to history and archives, for when you
are auditing why something was decided rather than asking what is true now.`,
		Examples: []string{
			"vat brain query idempotency retries",
			"vat brain query pricing --all      # include superseded and archived material",
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain query")
			all := set.Bool("all", false, "include history, archives, and terminal records")
			limit := set.Int("limit", 15, "maximum results")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() == 0 {
				return usageErrorf("expected at least one search term")
			}
			_, store, err := openBrain(env)
			if err != nil {
				return err
			}
			hits := brain.Query(store, set.Args(), brain.QueryOptions{
				IncludeHistory: *all, IncludeTerminal: *all, Limit: *limit,
			})
			if env.JSON {
				return emitJSON(env, hits)
			}
			if len(hits) == 0 {
				env.Printer.Println("No matches on the current surface.")
				if !*all {
					env.Printer.Hint("Add --all to search history and superseded records.")
				}
				return nil
			}
			for _, hit := range hits {
				label := hit.Path
				if hit.ID != "" {
					label = fmt.Sprintf("%s  %s", hit.ID, hit.Title)
				}
				level := ui.LevelInfo
				if hit.Status == brain.StatusStale || hit.Status == brain.StatusQuarantined {
					level = ui.LevelWarn
				}
				env.Printer.Status(level, label, string(hit.Status))
				env.Printer.Hint("      %s", hit.Path)
				for _, line := range hit.Excerpt {
					env.Printer.Hint("      │ %s", truncate(line, 96))
				}
			}
			env.Printer.Hint("\n%s. Open the records themselves; this is an index, not an answer.",
				pluralise(len(hits), "result", "results"))
			return nil
		},
	}
}

func brainReviewCommand() *Command {
	return &Command{
		Name:    "review",
		Summary: "The re-check queue, ordered by what it costs to leave unresolved",
		Usage:   "vat brain review [--overdue] [--limit n]",
		Long: `List everything awaiting human judgement.

Priority weights how many other records depend on a claim against how long it
has gone unverified. A stale claim nothing cites can wait; a stale claim the
roadmap rests on cannot. Without that ordering the queue is a flat list that
grows until it is ignored wholesale — which is the exact state this layer exists
to prevent.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain review")
			overdueOnly := set.Bool("overdue", false, "only items past the review window")
			limit := set.Int("limit", 20, "maximum items")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, store, err := openBrain(env)
			if err != nil {
				return err
			}
			items := brain.ReviewQueue(store, brainPolicy(ws), env.Now)
			if *overdueOnly {
				filtered := items[:0]
				for _, item := range items {
					if item.Overdue {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			if env.JSON {
				return emitJSON(env, items)
			}
			if len(items) == 0 {
				env.Printer.Status(ui.LevelOK, "review queue", "empty")
				return nil
			}
			shown := items
			if len(shown) > *limit {
				shown = shown[:*limit]
			}
			rows := make([][]string, 0, len(shown))
			for _, item := range shown {
				age := fmt.Sprintf("%d", item.AgeDays)
				if item.Overdue {
					age += " ⚠"
				}
				rows = append(rows, []string{
					item.ID, string(item.Status), age,
					fmt.Sprintf("%d", item.References), truncate(item.Title, 48), item.Why,
				})
			}
			env.Printer.Table([]string{"ID", "STATUS", "AGE", "CITED", "TITLE", "WHY"}, rows)
			overdue := 0
			for _, item := range items {
				if item.Overdue {
					overdue++
				}
			}
			env.Printer.Hint("\n%d awaiting review, %d past the %d-day window.",
				len(items), overdue, ws.Manifest.Policy.Brain.ReviewSLADays)
			env.Printer.Hint("Re-verify against the owning repository, then: vat brain promote <id>")
			return nil
		},
	}
}

func brainSweepCommand() *Command {
	return &Command{
		Name:    "sweep",
		Summary: "Demote claims whose evidence has aged out",
		Usage:   "vat brain sweep [--apply]",
		Long: `Move active current-state claims past the policy window to stale.

Without --apply nothing is written; the transitions are only listed.

Demotion is not deletion. The record and its reasoning survive intact — it
simply stops being citable until someone re-checks it. This is the mechanism
that keeps the layer honest across years rather than months.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain sweep")
			apply := set.Bool("apply", false, "write the transitions")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, store, err := openBrain(env)
			if err != nil {
				return err
			}
			transitions, err := brain.Sweep(store, brainPolicy(ws), env.Now, *apply)
			if err != nil {
				return err
			}
			if env.JSON {
				return emitJSON(env, transitions)
			}
			if len(transitions) == 0 {
				env.Printer.Status(ui.LevelOK, "sweep", "every claim is within its window")
				return nil
			}
			for _, transition := range transitions {
				level := ui.LevelWarn
				if transition.Applied {
					level = ui.LevelOK
				}
				env.Printer.Status(level, transition.ID,
					fmt.Sprintf("%s → %s (%s)", transition.From, transition.To, transition.Reason))
			}
			if !*apply {
				env.Printer.Hint("\nNothing written. Re-run with --apply to record these.")
			} else {
				env.Printer.Hint("\n%s demoted. Run `vat brain build` to refresh the index.",
					pluralise(len(transitions), "claim", "claims"))
			}
			return nil
		},
	}
}
