package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/takealook97/vat/internal/metrics"
	"github.com/takealook97/vat/internal/ui"
)

func metricsCommand() *Command {
	return &Command{
		Name:    "metrics",
		Summary: "Measure whether the workspace's discipline is actually holding",
		Usage:   "vat metrics [--record] [--history]",
		Long: `Answer the question a methodology cannot answer about itself.

A checklist measures whether people performed rituals. These numbers measure
whether the rituals produced the effect they were for.

The one to watch is the review queue. If it grows every month, knowledge is
being written faster than it is verified, and the knowledge layer is decaying
however diligently records are added. Rising median claim age says the same
thing more slowly.

--record appends a snapshot to the local ledger, so the direction of travel
becomes visible. A single reading says little.`,
		Examples: []string{
			"vat metrics",
			"vat metrics --record     # append a snapshot, then show the change",
			"vat metrics --history",
		},
		Run: runMetrics,
	}
}

func runMetrics(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("metrics")
	record := set.Bool("record", false, "append this snapshot to the local ledger")
	history := set.Bool("history", false, "print previous snapshots instead")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	ws, err := env.Workspace()
	if err != nil {
		return err
	}

	if *history {
		snapshots, err := metrics.History(ws)
		if err != nil {
			return err
		}
		if env.JSON {
			encoder := json.NewEncoder(env.Printer.Out())
			encoder.SetIndent("", "  ")
			return encoder.Encode(snapshots)
		}
		if len(snapshots) == 0 {
			env.Printer.Println("No snapshots recorded yet. Run `vat metrics --record`.")
			return nil
		}
		rows := make([][]string, 0, len(snapshots))
		for _, snapshot := range snapshots {
			rows = append(rows, []string{
				snapshot.At[:10],
				fmt.Sprintf("%d", snapshot.LintErrors),
				fmt.Sprintf("%d", snapshot.ReviewQueue),
				fmt.Sprintf("%d", snapshot.MedianClaimAgeDays),
				fmt.Sprintf("%d", snapshot.BrainCitable),
				fmt.Sprintf("%d", snapshot.ChangesetsOpen),
			})
		}
		env.Printer.Table([]string{"DATE", "LINT ERR", "QUEUE", "CLAIM AGE", "CITABLE", "OPEN CS"}, rows)
		return nil
	}

	snapshot, err := metrics.Collect(ctx, ws, env.Now)
	if err != nil {
		return err
	}
	previous, err := metrics.History(ws)
	if err != nil {
		return err
	}

	// Recorded first, so the trend line and the "no earlier snapshot" hint
	// cannot contradict each other in the same output.
	if *record {
		if err := metrics.Append(ws, snapshot); err != nil {
			return err
		}
	}

	if env.JSON {
		encoder := json.NewEncoder(env.Printer.Out())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(snapshot); err != nil {
			return err
		}
	} else {
		trends := metrics.Compare(snapshot, previous)
		rows := make([][]string, 0, len(trends))
		for _, trend := range trends {
			marker := ""
			switch {
			case trend.Improved:
				marker = "better"
			case trend.Worsened:
				marker = "worse"
			}
			rows = append(rows, []string{trend.Name, trend.Current, trend.Delta, marker, trend.Reading})
		}
		env.Printer.Table([]string{"MEASURE", "NOW", "CHANGE", "", "WHAT IT MEANS"}, rows)
		if len(previous) == 0 {
			env.Printer.Hint("\nNo earlier snapshot to compare against. Record one with --record.")
		}
	}

	if *record {
		env.Printer.Status(ui.LevelOK, "ledger", "snapshot recorded")
	}
	return nil
}
