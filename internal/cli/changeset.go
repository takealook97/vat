package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/runner"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

func changesetCommand() *Command {
	return &Command{
		Name:    "changeset",
		Summary: "Record the evidence for a change that spans several repositories",
		Usage:   "vat changeset <new|add|verify|show|list|close|abandon|undo-plan>",
		Long: `Pay back the cost of choosing many repositories over one.

Separate repositories mean a cross-cutting change is several commits with no
relationship recorded anywhere. Weeks later nobody can say which revisions were
ever verified together, or what to revert if it turns out wrong. That is the
real price of a multi-repo layout, and it is usually never paid.

A changeset is the record that pays it: the repositories involved, the point
each one can return to, the exact revision its checks passed on, and the single
end-to-end outcome that proves the pieces work together.`,
		Examples: []string{
			`vat changeset new "Move order cancellation to v2"`,
			"vat changeset add CS-0001 payments console",
			"vat changeset verify CS-0001",
			`vat changeset close CS-0001 --acceptance "cancel-then-refund passes end to end"`,
		},
		Subcommands: []*Command{
			changesetNewCommand(),
			changesetAddCommand(),
			changesetVerifyCommand(),
			changesetShowCommand(),
			changesetListCommand(),
			changesetCloseCommand(),
			changesetAbandonCommand(),
			changesetUndoPlanCommand(),
		},
	}
}

func changesetNewCommand() *Command {
	return &Command{
		Name:    "new",
		Summary: "Open a changeset",
		Usage:   `vat changeset new "<objective>" [--repos a,b] [--non-goal "..."] [--contract "..."] [--decision D-0001]`,
		Run:     runChangesetNew,
	}
}

func runChangesetNew(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("changeset new")
	repos := set.String("repos", "", "repositories to enrol immediately")
	nonGoals := set.String("non-goal", "", "things explicitly out of scope, comma-separated")
	contracts := set.String("contract", "", "interfaces this change touches")
	decisions := set.String("decision", "", "brain decision identifiers that authorised this")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() == 0 {
		return usageErrorf("expected an objective")
	}
	objective := strings.Join(set.Args(), " ")

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	if err := fsx.EnsureDir(ws.ChangesetsDir()); err != nil {
		return err
	}
	id, err := changeset.NextID(ws.Root)
	if err != nil {
		return err
	}
	current := changeset.New(id, objective, env.Now)
	current.NonGoals = splitList(*nonGoals)
	current.Contracts = splitList(*contracts)
	current.Decisions = splitList(*decisions)

	for _, name := range splitList(*repos) {
		participant, err := enrol(ctx, ws, name)
		if err != nil {
			return err
		}
		current = changeset.WithParticipant(current, participant)
	}
	if err := changeset.Save(ws.Root, current); err != nil {
		return err
	}

	env.Printer.Status(ui.LevelOK, id, changeset.Path(id))
	for _, participant := range current.Repositories {
		env.Printer.Status(ui.LevelInfo, participant.Name,
			"return point "+shortRev(participant.RollbackPoint))
	}
	env.Printer.Hint("\nEnrol more:  vat changeset add %s <repo>...", id)
	env.Printer.Hint("When ready:  vat changeset verify %s", id)
	return nil
}

// enrol records where a repository stands before the change begins. Captured
// now because after the change lands it can no longer be observed.
func enrol(ctx context.Context, ws *workspace.Workspace, name string) (changeset.Participant, error) {
	repo, ok := ws.Manifest.Find(name)
	if !ok {
		return changeset.Participant{}, usageErrorf("%q is not in %s", name, manifest.FileName)
	}
	dir := ws.RepoPath(repo)
	if !gitx.IsRepository(dir) {
		return changeset.Participant{}, usageErrorf("%s is not cloned", name)
	}
	revision, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		return changeset.Participant{}, err
	}
	branch, _ := gitx.CurrentBranch(ctx, dir)
	return changeset.Participant{Name: name, RollbackPoint: revision, Branch: branch}, nil
}

func changesetAddCommand() *Command {
	return &Command{
		Name:    "add",
		Summary: "Enrol more repositories in an open changeset",
		Usage:   "vat changeset add <id> <repo>...",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("changeset add")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() < 2 {
				return usageErrorf("expected a changeset id and at least one repository")
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			current, err := changeset.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			for _, name := range set.Args()[1:] {
				if _, exists := current.Participant(name); exists {
					env.Printer.Status(ui.LevelSkip, name, "already enrolled")
					continue
				}
				participant, err := enrol(ctx, ws, name)
				if err != nil {
					return err
				}
				current = changeset.WithParticipant(current, participant)
				env.Printer.Status(ui.LevelOK, name,
					"return point "+shortRev(participant.RollbackPoint))
			}
			return changeset.Save(ws.Root, current)
		},
	}
}

func changesetVerifyCommand() *Command {
	return &Command{
		Name:    "verify",
		Summary: "Run each repository's canonical checks and record the result",
		Usage:   "vat changeset verify <id> [--timeout <duration>]",
		Long: `Run the canonical checks the manifest declares for every enrolled repository,
and record each outcome against the exact revision it ran on.

This is the closest a multi-repo workspace gets to cross-repository CI: it does
not stop a bad combination from being committed, but it makes the combination
that was actually verified a matter of record rather than of memory.

A worker reporting success is not evidence. This is.`,
		Run: runChangesetVerify,
	}
}

func runChangesetVerify(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("changeset verify")
	timeout := set.Duration("timeout", 20*time.Minute, "per-command timeout")
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
	if len(current.Repositories) == 0 {
		return usageErrorf("%s has no repositories enrolled", current.ID)
	}
	// Verifying a finished changeset rewrote its status back to "verified"
	// while its closing evidence stayed in the file, leaving a record that
	// claimed both at once.
	if !current.Status.Open() {
		return usageErrorf(
			"%s is %s; verifying it would contradict its own closing record.\n"+
				"  Open a new changeset for further work.", current.ID, current.Status)
	}

	failures := 0
	for _, participant := range current.Repositories {
		repo, ok := ws.Manifest.Find(participant.Name)
		if !ok {
			env.Printer.Status(ui.LevelFail, participant.Name, "no longer in the manifest")
			failures++
			continue
		}
		dir := ws.RepoPath(repo)
		if !gitx.IsRepository(dir) {
			env.Printer.Status(ui.LevelFail, participant.Name, "not cloned")
			failures++
			continue
		}
		revision, err := gitx.HeadRevision(ctx, dir)
		if err != nil {
			return err
		}
		dirty, err := gitx.IsDirty(ctx, dir)
		if err != nil {
			// Discarding this would record checks against a revision while
			// silently treating an unreadable repository as clean, which is
			// the one thing a verification record must never do.
			env.Printer.Status(ui.LevelFail, participant.Name,
				"git cannot read the working tree state; not verified")
			failures++
			continue
		}
		if dirty {
			// Recording a pass here would file results against a revision that
			// does not describe what was tested — the exact claim a changeset
			// exists to make. Reporting it and continuing was worse than
			// refusing, because the record looked verified either way.
			env.Printer.Status(ui.LevelFail, participant.Name,
				"working tree is dirty; results would not describe "+shortRev(revision)+
					". Commit or stash, then verify")
			failures++
			updated := participant
			updated.Revision = ""
			updated.Checks = nil
			current = changeset.WithParticipant(current, updated)
			continue
		}
		updated := participant
		updated.Revision = revision
		if updated.Branch, err = gitx.CurrentBranch(ctx, dir); err != nil {
			env.Printer.Status(ui.LevelWarn, participant.Name,
				"branch could not be read; recording the revision alone")
		}
		updated.Checks = nil

		if len(repo.Checks) == 0 {
			env.Printer.Status(ui.LevelWarn, participant.Name,
				"declares no canonical checks; nothing can be verified")
			failures++
			current = changeset.WithParticipant(current, updated)
			continue
		}
		for _, command := range repo.Checks {
			result := runner.RunOne(ctx, runner.Job{
				Repo: participant.Name, Dir: dir, Command: command,
			}, *timeout)
			run := changeset.CheckRun{
				Command:  command,
				Status:   "pass",
				RanAt:    env.Now.Format(time.RFC3339),
				Revision: revision,
			}
			if !result.OK() {
				run.Status = "fail"
				run.Detail = result.FirstLine()
				failures++
				env.Printer.Status(ui.LevelFail, participant.Name+" · "+command, run.Detail)
			} else {
				env.Printer.Status(ui.LevelOK, participant.Name+" · "+command,
					fmt.Sprintf("%s at %s", result.Duration.Round(time.Millisecond), shortRev(revision)))
			}
			updated.Checks = append(updated.Checks, run)
		}
		current = changeset.WithParticipant(current, updated)
	}

	if failures == 0 {
		current.Status = changeset.StatusVerified
	} else {
		current.Status = changeset.StatusOpen
	}
	if err := changeset.Save(ws.Root, current); err != nil {
		return err
	}

	env.Printer.Heading("Result")
	if failures > 0 {
		env.Printer.Status(ui.LevelFail, current.ID,
			fmt.Sprintf("%d check(s) failed; recorded against the revisions they ran on", failures))
		return findingsErrorf("")
	}
	env.Printer.Status(ui.LevelOK, current.ID, "every repository verified")
	env.Printer.Hint("\nStill needed before closing: the one end-to-end outcome that proves")
	env.Printer.Hint("the pieces work together.")
	env.Printer.Hint("  vat changeset close %s --acceptance \"...\"", current.ID)
	return nil
}

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
			if env.JSON {
				return emitJSON(env, sets)
			}
			rows := make([][]string, 0, len(sets))
			for _, current := range sets {
				if *openOnly && !current.Status.Open() {
					continue
				}
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

func shortRev(revision string) string {
	if revision == "" {
		return "-"
	}
	if len(revision) > 8 {
		return revision[:8]
	}
	return revision
}
