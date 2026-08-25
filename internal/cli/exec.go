package cli

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/runner"
	"github.com/takealook97/vat/internal/ui"
)

func execCommand() *Command {
	return &Command{
		Name:    "exec",
		Summary: "Run a command in every selected repository",
		Usage:   "vat exec [--group <g>] [--role <r>] [--checks] -- <command>",
		Long: `Run one command across the workspace, in parallel, with per-repository results.

Unlike a shell loop, a failure in one repository is never hidden by success in
another: each result is reported separately and the exit code reflects the whole
run.

--checks runs each repository's own canonical checks from the manifest instead
of a command you supply, which is how you ask "is everything still green?"
without knowing what each repository uses to answer that.`,
		Examples: []string{
			"vat exec -- git status --short",
			"vat exec --group backend -- make test",
			"vat exec --checks",
		},
		Run: runExec,
	}
}

func runExec(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("exec")
	group := set.String("group", "", "only repositories in these groups")
	role := set.String("role", "", "only repositories with these roles")
	only := set.String("only", "", "only these repositories by name")
	useChecks := set.Bool("checks", false, "run each repository's canonical checks from the manifest")
	jobs := set.Int("jobs", 0, "concurrent commands (default: policy.sync.parallelism)")
	timeout := set.Duration("timeout", 10*time.Minute, "per-command timeout")
	keepGoing := set.Bool("keep-going", true, "run everywhere even after a failure")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	command := strings.Join(set.Args(), " ")
	if command == "" && !*useChecks {
		return usageErrorf("expected a command after `--`, or --checks")
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	repos, err := ws.Select(manifest.Selector{
		Names: splitList(*only), Groups: splitList(*group), Roles: splitList(*role),
	})
	if err != nil {
		return usageErrorf("%v", err)
	}

	var jobList []runner.Job
	for _, repo := range repos {
		dir := ws.RepoPath(repo)
		if !gitx.IsRepository(dir) {
			env.Printer.Status(ui.LevelSkip, repo.Name, "not cloned")
			continue
		}
		if *useChecks {
			if len(repo.Checks) == 0 {
				env.Printer.Status(ui.LevelSkip, repo.Name, "declares no canonical checks")
				continue
			}
			for _, check := range repo.Checks {
				jobList = append(jobList, runner.Job{Repo: repo.Name, Dir: dir, Command: check})
			}
			continue
		}
		jobList = append(jobList, runner.Job{Repo: repo.Name, Dir: dir, Command: command})
	}
	if len(jobList) == 0 {
		env.Printer.Println("Nothing to run.")
		return nil
	}

	parallelism := *jobs
	if parallelism <= 0 {
		parallelism = ws.Manifest.Policy.Sync.Parallelism
	}

	results := runner.Run(ctx, jobList, runner.Options{
		Parallelism: parallelism, Timeout: *timeout,
		// Stopping means abandoning the rest, not merely running them one at a
		// time: the runner executes sequentially and skips what follows a
		// failure.
		StopOnFailure: !*keepGoing,
	})

	if env.JSON {
		encoder := json.NewEncoder(env.Printer.Out())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(results); err != nil {
			return err
		}
	} else {
		renderExecResults(env, results)
	}

	failures := 0
	for _, result := range results {
		if !result.OK() && !result.Skipped {
			failures++
		}
	}
	if failures > 0 {
		return findingsErrorf("%d of %d commands failed.", failures, len(results))
	}
	return nil
}

func renderExecResults(env *Env, results []runner.Result) {
	for _, result := range results {
		label := result.Repo
		if result.Command != "" {
			label = result.Repo + " · " + truncate(result.Command, 32)
		}
		if result.Skipped {
			env.Printer.Status(ui.LevelSkip, label, "not run; an earlier command failed")
			continue
		}
		if result.OK() {
			env.Printer.Status(ui.LevelOK, label, result.Duration.Round(time.Millisecond).String())
			for _, line := range firstLines(result.Stdout, 3) {
				env.Printer.Hint("      | %s", truncate(line, 100))
			}
			continue
		}
		env.Printer.Status(ui.LevelFail, label, result.FirstLine())
		for _, line := range firstLines(result.Output(), 6) {
			env.Printer.Hint("      | %s", truncate(line, 100))
		}
	}
	passed, skipped := 0, 0
	for _, result := range results {
		switch {
		case result.Skipped:
			skipped++
		case result.OK():
			passed++
		}
	}
	if skipped > 0 {
		env.Printer.Hint("\n%d of %d succeeded, %d not run after the first failure.",
			passed, len(results), skipped)
		return
	}
	env.Printer.Hint("\n%d of %d succeeded.", passed, len(results))
}

func firstLines(text string, limit int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
		if len(lines) >= limit {
			break
		}
	}
	return lines
}
