package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/ui"
)

// Reading a changeset, closing it with the outcome that proves the pieces
// work together, abandoning it, and printing the way back.

func changesetShowCommand() *Command {
	return &Command{
		Name:    "show",
		Summary: "Print a changeset in full",
		Usage:   "vat changeset show <id>",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset show")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one changeset identifier")
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			current, err := changeset.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			if env.JSON {
				return emitJSON(env, current)
			}
			env.Printer.Printf("%s  %s\n", current.ID, current.Objective)
			env.Printer.Printf("status: %s · opened %s · %d days\n",
				current.Status, current.OpenedAt, current.AgeDays(env.Now))
			if current.Acceptance != "" {
				env.Printer.Printf("acceptance: %s\n", current.Acceptance)
			}
			// Why work stopped is the whole value of an abandoned record, and
			// it was being written to the file and never shown again.
			if current.Notes != "" {
				env.Printer.Printf("notes: %s\n", current.Notes)
			}
			env.Printer.Heading("Repositories")
			rows := make([][]string, 0, len(current.Repositories))
			for _, participant := range current.Repositories {
				verdict := "not verified"
				if participant.Verified() {
					verdict = fmt.Sprintf("%d checks passed", len(participant.Checks))
				}
				rows = append(rows, []string{
					participant.Name, shortRev(participant.RollbackPoint),
					shortRev(participant.Revision), verdict,
				})
			}
			env.Printer.Table([]string{"REPOSITORY", "RETURN TO", "VERIFIED AT", "CHECKS"}, rows)
			problems := changeset.Validate(current, ws.Manifest.Policy.Changeset.RequireRollbackPoint)
			if len(problems) > 0 {
				env.Printer.Heading("Problems")
				for _, problem := range problems {
					env.Printer.Status(ui.LevelWarn, current.ID, problem)
				}
			}
			return nil
		},
	}
}

func changesetListCommand() *Command {
	return &Command{
		Name:    "list",
		Summary: "List changesets, newest first",
		Usage:   "vat changeset list [--open]",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset list")
			openOnly := set.Bool("open", false, "only unfinished changesets")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			sets, err := changeset.LoadAll(ws.Root)
			if err != nil {
				return err
			}
			// Filtered before the JSON branch, not inside the table loop.
			// Applying it in only one of the two renderings meant a CI job
			// asking for open work in JSON was handed every changeset ever
			// closed and had no way to tell.
			if *openOnly {
				open := make([]changeset.Changeset, 0, len(sets))
				for _, current := range sets {
					if current.Status.Open() {
						open = append(open, current)
					}
				}
				sets = open
			}
			if env.JSON {
				return emitJSON(env, sets)
			}
			rows := make([][]string, 0, len(sets))
			for _, current := range sets {
				age := fmt.Sprintf("%dd", current.AgeDays(env.Now))
				limit := ws.Manifest.Policy.Changeset.MaxOpenDays
				if current.Status.Open() && limit > 0 && current.AgeDays(env.Now) > limit {
					age += " !"
				}
				names := make([]string, 0, len(current.Repositories))
				for _, participant := range current.Repositories {
					names = append(names, participant.Name)
				}
				rows = append(rows, []string{
					current.ID, string(current.Status), age,
					strings.Join(names, ","), truncate(current.Objective, 48),
				})
			}
			if len(rows) == 0 {
				env.Printer.Println("No changesets.")
				return nil
			}
			env.Printer.Table([]string{"ID", "STATUS", "AGE", "REPOSITORIES", "OBJECTIVE"}, rows)
			return nil
		},
	}
}

func changesetCloseCommand() *Command {
	return &Command{
		Name:    "close",
		Summary: "Close a verified changeset with its integration outcome",
		Usage:   `vat changeset close <id> --acceptance "..." [--approved-by <name>] [--force]`,
		Long: `Close a changeset.

An acceptance statement is required, and it must describe something end to end.
Per-repository checks passing is not the same as the pieces working together —
that gap is exactly where multi-repository changes break, and closing without
naming the outcome loses the only record of whether anyone checked.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset close")
			acceptance := set.String("acceptance", "", "the end-to-end outcome that proves this worked (required)")
			approvedBy := set.String("approved-by", "", "who approved the release")
			force := set.Bool("force", false, "close despite unverified repositories")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one changeset identifier")
			}
			if strings.TrimSpace(*acceptance) == "" {
				return usageErrorf("--acceptance is required: what single end-to-end outcome proves this worked?")
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			current, err := changeset.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			if !current.FullyVerified() && !*force {
				for _, participant := range current.Repositories {
					if !participant.Verified() {
						env.Printer.Status(ui.LevelFail, participant.Name,
							"no passing checks recorded at a known revision")
					}
				}
				return findingsErrorf("Run `vat changeset verify %s` first, or pass --force.", current.ID)
			}
			current.Status = changeset.StatusClosed
			current.Acceptance = *acceptance
			current.ApprovedBy = *approvedBy
			current.ClosedAt = env.Now.Format("2006-01-02")
			if err := changeset.Save(ws.Root, current); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, current.ID, "closed")
			env.Printer.Hint("The revision bundle and return points stay on record in %s.",
				changeset.Path(current.ID))
			return nil
		},
	}
}

func changesetAbandonCommand() *Command {
	return &Command{
		Name:    "abandon",
		Summary: "Mark a changeset as stopped without shipping",
		Usage:   "vat changeset abandon <id> [--reason <text>]",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset abandon")
			reason := set.String("reason", "", "why the work stopped")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one changeset identifier")
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			current, err := changeset.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			if !current.Status.Open() {
				return usageErrorf("%s is already %s", current.ID, current.Status)
			}
			current.Status = changeset.StatusAbandoned
			current.ClosedAt = env.Now.Format("2006-01-02")
			if *reason != "" {
				current.Notes = strings.TrimSpace(current.Notes + "\nabandoned: " + *reason)
			}
			if err := changeset.Save(ws.Root, current); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, current.ID, "abandoned")
			return nil
		},
	}
}

func changesetUndoPlanCommand() *Command {
	return &Command{
		Name:    "undo-plan",
		Summary: "Print the ordered plan to return every repository to its start point",
		Usage:   "vat changeset undo-plan <id>",
		Long: `Print the commands that would return every repository to where it started.

vat prints the plan and never carries it out. Returning several repositories at
once is irreversible and depends on facts vat cannot see: what has been
deployed, and what others have already pulled. The plan is the part that is hard
to reconstruct; acting on it is your decision.

Repositories are listed in reverse order of enrolment, so no window exists where
a consumer still expects an interface that is already gone.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset undo-plan")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one changeset identifier")
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			current, err := changeset.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			steps, err := current.RollbackPlan()
			if err != nil {
				return findingsErrorf("%v", err)
			}
			env.Printer.Printf("# Return plan for %s - %s\n", current.ID, current.Objective)
			env.Printer.Println("# Reverse enrolment order. Review every line before acting on it.")
			for _, step := range steps {
				env.Printer.Println(step)
			}
			return nil
		},
	}
}
