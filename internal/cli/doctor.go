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
		Usage:   "vat doctor [--network] [--offline] [--secret-max-age <days>]",
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
			"vat doctor --offline                # explicit: the default, and refuses --network",
			"vat doctor --secret-max-age 90      # report credentials unrotated for 90 days",
		},
		Run: runDoctor,
	}
}

func runDoctor(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("doctor")
	network := set.Bool("network", false, "include read-only network and auth checks")
	// `vat sync` and `vat lint` both take --offline and doctor did not, so a
	// script that passed it everywhere failed on the one command that was
	// already offline. Accepted as what it means here rather than rejected: the
	// caller is asking for the default and is right to be able to say so.
	offline := set.Bool("offline", false, "state explicitly that no network is to be used; doctor is offline unless --network")
	secretAge := set.Int("secret-max-age", 180, "report credential material older than this many days (0 disables)")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if *offline && *network {
		return usageErrorf("--offline and --network ask for opposite things")
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
			fmt.Sprintf("%s, %s", pluralise(report.Failures, "failing", "failing"), pluralise(report.Warnings, "warning", "warnings")))
	case report.Warnings > 0:
		printer.Status(ui.LevelWarn, "environment",
			fmt.Sprintf("usable, %s", pluralise(report.Warnings, "warning", "warnings")))
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
