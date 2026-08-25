package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/ui"
)

func lintCommand() *Command {
	return &Command{
		Name:    "lint",
		Summary: "Check the workspace against its own declared rules",
		Usage:   "vat lint [--fix] [--offline] [--only <rule>] [--list]",
		Long: `Enforce mechanically what the workspace only states in prose.

A rule that is written down and not checked is a hope. These are the failures
that actually happen as a multi-repository workspace ages:

  a repository added to the manifest but forgotten in .gitignore, so the next
  root commit absorbs an entire nested clone

  a role body copied into two runtime adapters that then diverge, so the same
  agent behaves differently depending on which tool started it

  a claim whose evidence moved months ago, still being cited as current

  a workspace contract that grew until the per-repository contracts below it
  stopped fitting in an agent's context

--fix repairs only what can be repaired without judgement: it regenerates what
is generated. It never edits a fact, a decision, or a working tree.`,
		Examples: []string{
			"vat lint",
			"vat lint --fix",
			"vat lint --only harness",
			"vat lint --list      # every rule this command can report",
		},
		Run: runLint,
	}
}

func runLint(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("lint")
	fix := set.Bool("fix", false, "regenerate what is generated, then re-check")
	offline := set.Bool("offline", false, "skip rules that resolve git revisions")
	only := set.String("only", "", "only rules whose name contains this")
	list := set.Bool("list", false, "list every rule and exit")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	if *list {
		for _, rule := range lint.RuleNames() {
			env.Printer.Println(rule)
		}
		return nil
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}

	if *fix {
		result, err := lint.Fix(ws, env.Now)
		if err != nil {
			return err
		}
		if len(result.Changed) == 0 {
			env.Printer.Status(ui.LevelOK, "fix", "nothing to regenerate")
		}
		for _, file := range result.Changed {
			env.Printer.Status(ui.LevelOK, file, "regenerated")
		}
		// Reload: the repair changed the files the rules are about to read.
		if ws, err = env.Workspace(); err != nil {
			return err
		}
	}

	report, err := lint.Run(ctx, ws, lint.Options{Offline: *offline, Now: env.Now, Only: splitList(*only)})
	if err != nil {
		return err
	}

	if env.JSON {
		encoder := json.NewEncoder(env.Printer.Out())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		renderLintReport(env, report)
	}
	if report.Errors() > 0 {
		return findingsErrorf("")
	}
	return nil
}

func renderLintReport(env *Env, report lint.Report) {
	printer := env.Printer
	if len(report.Findings) == 0 {
		printer.Status(ui.LevelOK, "lint",
			fmt.Sprintf("%d rules checked, nothing to report", report.Checked))
		return
	}
	for _, finding := range report.Findings {
		level := ui.LevelWarn
		if finding.Severity == lint.SeverityError {
			level = ui.LevelFail
		}
		subject := finding.Rule
		if finding.Subject != "" {
			subject = finding.Rule + " · " + finding.Subject
		}
		printer.Status(level, subject, finding.Message)
		if finding.Fix != "" {
			printer.Hint("      → %s", finding.Fix)
		}
	}
	printer.Heading("Result")
	errors := report.Errors()
	warnings := len(report.Findings) - errors
	level := ui.LevelOK
	if errors > 0 {
		level = ui.LevelFail
	} else if warnings > 0 {
		level = ui.LevelWarn
	}
	printer.Status(level, "lint", fmt.Sprintf("%d errors, %d warnings across %d rules",
		errors, warnings, report.Checked))
	if fixable := report.Fixable(); fixable > 0 {
		printer.Hint("%d of these can be repaired with `vat lint --fix`.", fixable)
	}
}
