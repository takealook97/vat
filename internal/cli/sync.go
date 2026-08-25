package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/syncx"
	"github.com/takealook97/vat/internal/ui"
)

func syncCommand() *Command {
	return &Command{
		Name:    "sync",
		Summary: "Fetch every repository and fast-forward only what is safe",
		Usage:   "vat sync [--dry-run] [--offline] [--group <g>] [--only <names>]",
		Long: `Update the workspace without ever discarding local work.

Missing repositories are cloned. Everything else is fetched, and only a clean
default branch that is strictly behind its upstream is advanced, with
--ff-only. Every other state is reported and left exactly as it is:

  dirty tree        reported, nothing advanced — your changes are not stashed
  other branch      reported, nothing advanced — you are not checked out elsewhere
  local ahead       reported, nothing pushed — pushing is your decision
  diverged          failure — an automatic merge here guesses at intent
  remote mismatch   failure — origin pointing elsewhere is a supply-chain signal

A failure in one repository is never hidden by success in another; the exit code
reflects the worst outcome in the run.`,
		Examples: []string{
			"vat sync",
			"vat sync --dry-run     # show what would happen, contacting nothing",
			"vat sync --offline     # check local structure with no network at all",
		},
		Run: runSync,
	}
}

func runSync(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("sync")
	dryRun := set.Bool("dry-run", false, "print the plan without changing anything")
	offline := set.Bool("offline", false, "skip all network operations")
	group := set.String("group", "", "only repositories in these groups")
	role := set.String("role", "", "only repositories with these roles")
	only := set.String("only", "", "only these repositories by name")
	jobs := set.Int("jobs", 0, "concurrent git operations (default: policy.sync.parallelism)")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	repos, err := ws.Select(manifest.Selector{
		Names:  append(splitList(*only), set.Args()...),
		Groups: splitList(*group), Roles: splitList(*role),
	})
	if err != nil {
		return usageErrorf("%v", err)
	}

	report := syncx.Run(ctx, ws, repos, syncx.Options{
		DryRun: *dryRun, Offline: *offline, Parallelism: *jobs,
	})
	report.Results = syncx.SortResults(report.Results, repos)

	if env.JSON {
		encoder := json.NewEncoder(env.Printer.Out())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		renderSyncReport(env, report)
	}

	if report.Failures > 0 {
		return findingsErrorf("%d repository/repositories need attention before the workspace is consistent.",
			report.Failures)
	}
	return nil
}

func renderSyncReport(env *Env, report syncx.Report) {
	rows := make([][]string, 0, len(report.Results))
	for _, result := range report.Results {
		rows = append(rows, []string{
			result.Repo, string(result.State), result.Branch, result.Revision, result.Detail,
		})
	}
	env.Printer.Table([]string{"REPOSITORY", "STATE", "BRANCH", "REV", "DETAIL"}, rows)

	updated, skipped := 0, 0
	for _, result := range report.Results {
		switch result.State {
		case syncx.StateUpdated, syncx.StateCloned:
			updated++
		case syncx.StateDirty, syncx.StateBranch, syncx.StateAhead, syncx.StateDetached:
			skipped++
		}
	}
	env.Printer.Hint("\n%d advanced · %d left alone on purpose · %d need attention",
		updated, skipped, report.Failures)
	if report.Failures > 0 {
		env.Printer.Status(ui.LevelFail, "result",
			fmt.Sprintf("%d repositories could not be brought to a known-good state", report.Failures))
	}
}
