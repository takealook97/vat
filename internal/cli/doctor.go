package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/takealook97/vat/internal/doctor"
	"github.com/takealook97/vat/internal/ui"
)

func doctorCommand() *Command {
	return &Command{
		Name:    "doctor",
		Summary: "Judge the workspace and the machine, without changing either",
		Usage:   "vat doctor [--network] [--secret-max-age <days>]",
		Long: `Diagnose the environment and stop there.

doctor never repairs anything. A tool that silently fixes what it finds teaches
you nothing about why it broke, and on a machine holding credentials and
unpushed commits, "fixing" is how work disappears.

No secret value is ever printed. Findings about credentials are limited to
whether a file exists, whether it looks encrypted, and how long it has gone
unchanged.`,
		Examples: []string{
			"vat doctor",
			"vat doctor --network                # add read-only reachability checks",
			"vat doctor --secret-max-age 90      # report credentials unrotated for 90 days",
		},
		Run: runDoctor,
	}
}

func runDoctor(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("doctor")
	network := set.Bool("network", false, "include read-only network and auth checks")
	secretAge := set.Int("secret-max-age", 180, "report credential material older than this many days (0 disables)")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	report := doctor.Run(ctx, ws, doctor.Options{
		Network: *network, Now: env.Now, SecretMaxAgeDays: *secretAge,
	})

	if env.JSON {
		if err := emitJSON(env, report); err != nil {
			return err
		}
	} else {
		renderDoctorReport(env, report)
	}
	if report.Failures > 0 {
		return findingsErrorf("")
	}
	return nil
}

func renderDoctorReport(env *Env, report doctor.Report) {
	printer := env.Printer
	section := ""
	for _, finding := range report.Findings {
		if finding.Section != section {
			section = finding.Section
			printer.Heading(strings.ToUpper(section[:1]) + section[1:])
		}
		printer.Status(levelOf(finding.Status), finding.Subject, finding.Detail)
		if finding.Fix != "" && finding.Status != doctor.StatusOK {
			printer.Hint("      → %s", finding.Fix)
		}
	}
	printer.Heading("Result")
	switch {
	case report.Failures > 0:
		printer.Status(ui.LevelFail, "environment",
			fmt.Sprintf("%d failing, %d warnings", report.Failures, report.Warnings))
	case report.Warnings > 0:
		printer.Status(ui.LevelWarn, "environment",
			fmt.Sprintf("usable, %d warnings", report.Warnings))
	default:
		printer.Status(ui.LevelOK, "environment", "everything checked is in order")
	}
}

func levelOf(status doctor.Status) ui.Level {
	switch status {
	case doctor.StatusFail:
		return ui.LevelFail
	case doctor.StatusWarn:
		return ui.LevelWarn
	default:
		return ui.LevelOK
	}
}
