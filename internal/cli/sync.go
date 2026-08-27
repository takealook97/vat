package cli

import (
	"context"
	"fmt"

	"github.com/takealook97/vat/internal/syncx"
	"github.com/takealook97/vat/internal/ui"
)

func syncCommand() *Command {
	return &Command{
		Name:    "sync",
		Summary: "Fetch every repository and fast-forward only what is safe",
		Usage:   "vat sync [--dry-run] [--offline] [--only <names>] [--group <g>] [--role <r>] [--jobs <n>]",
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
	selection := bindSelector(set, false)
	jobs := set.Int("jobs", 0, "concurrent git operations (default: policy.sync.parallelism)")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	repos, err := selection.resolve(ws, set)
	if err != nil {
		return err
	}

	report := syncx.Run(ctx, ws, repos, syncx.Options{
		DryRun: *dryRun, Offline: *offline, Parallelism: *jobs,
	})
	report.Results = syncx.SortResults(report.Results, repos)

	if env.JSON {
		if err := emitJSON(env, report); err != nil {
			return err
		}
	} else {
		renderSyncReport(env, report)
	}

	if report.Failures > 0 {
		if env.JSON {
			// The trailer goes to stdout, which would leave the JSON payload
			// followed by prose and unparseable by the caller that asked for it.
			return findingsErrorf("")
		}
		verb := "need"
		if report.Failures == 1 {
			verb = "needs"
		}
		return findingsErrorf("%s %s attention before the workspace is consistent.",
			pluralise(report.Failures, "repository", "repositories"), verb)
	}
	return nil
}

func renderSyncReport(env *Env, report syncx.Report) {
	if len(report.Results) == 0 {
		env.Printer.Println("No repositories are enrolled.")
		env.Printer.Hint("Enrol one with `vat repo add <name> --origin <url>`.")
		return
	}
	rows := make([][]string, 0, len(report.Results))
	for _, result := range report.Results {
		rows = append(rows, []string{
			result.Repo, string(result.State), result.Branch, result.Revision, result.Detail,
		})
	}
	env.Printer.Table([]string{"REPOSITORY", "STATE", "BRANCH", "REV", "DETAIL"}, rows)

	updated, skipped, current, planned := 0, 0, 0, 0
	for _, result := range report.Results {
		switch result.State {
		case syncx.StateUpdated, syncx.StateCloned:
			updated++
		case syncx.StateCurrent, syncx.StateArchived:
			current++
		case syncx.StatePlanned:
			planned++
		case syncx.StateDirty, syncx.StateBranch, syncx.StateAhead, syncx.StateDetached,
			syncx.StateNoRemote:
			skipped++
		}
	}
	needs := "need attention"
	if report.Failures == 1 {
		needs = "needs attention"
	}
	// A dry run reports a plan, not an outcome. Counting a repository it would
	// clone as "already current" says the opposite of what the row means, in the
	// line somebody reads to decide whether to run it for real.
	if planned > 0 {
		env.Printer.Hint("\n%s would change · %d left alone on purpose · %d %s. Nothing was written.",
			pluralise(planned, "repository", "repositories"), skipped, report.Failures, needs)
		return
	}
	// Every row above is accounted for. A summary whose numbers do not add up to
	// the table it sits under is the state this tool reports in other people's
	// workspaces, and it read as three zeros exactly when the run went well.
	env.Printer.Hint("\n%d advanced · %d already current · %d left alone on purpose · %d %s",
		updated, current, skipped, report.Failures, needs)
	if report.Failures > 0 {
		env.Printer.Status(ui.LevelFail, "result",
			fmt.Sprintf("%s could not be brought to a known-good state",
				pluralise(report.Failures, "repository", "repositories")))
	}
}
