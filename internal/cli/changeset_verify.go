package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/runner"
	"github.com/takealook97/vat/internal/ui"
)

// Running the canonical checks and recording each outcome against the exact
// revision it ran on. A worker reporting success is not evidence; this is.

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
	unverifiable := 0
	for _, participant := range current.Repositories {
		repo, ok := ws.Manifest.Find(participant.Name)
		if !ok {
			env.Printer.Status(ui.LevelFail, participant.Name, "no longer in the manifest")
			unverifiable++
			continue
		}
		dir := ws.RepoPath(repo)
		if !gitx.IsRepository(dir) {
			env.Printer.Status(ui.LevelFail, participant.Name, "not cloned")
			unverifiable++
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
			unverifiable++
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
			unverifiable++
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
			// Counted apart from a failed check. Both block the changeset, and
			// they are not the same fact: one is evidence that something broke,
			// the other is the absence of any evidence at all.
			unverifiable++
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

	if failures+unverifiable == 0 {
		current.Status = changeset.StatusVerified
	} else {
		current.Status = changeset.StatusOpen
	}
	if err := changeset.Save(ws.Root, current); err != nil {
		return err
	}

	env.Printer.Heading("Result")
	if summary := verifySummary(failures, unverifiable); summary != "" {
		env.Printer.Status(ui.LevelFail, current.ID, summary)
		return findingsErrorf("")
	}
	env.Printer.Status(ui.LevelOK, current.ID, "every repository verified")
	// Naming `close` here sent the reader at a command that now refuses, because
	// verified is not landed. A hint that walks into a refusal is the same
	// defect wherever it appears: the tool advertising a step it will reject.
	env.Printer.Hint("\nStill needed before closing:")
	env.Printer.Hint("  vat ship %s", current.ID)
	env.Printer.Hint("      — these revisions have to reach the branches they ship from")
	env.Printer.Hint("  vat changeset close %s --acceptance \"...\"", current.ID)
	env.Printer.Hint("      — the one end-to-end outcome that proves the pieces work together")
	return nil
}

// verifySummary states what stopped the changeset, keeping a check that ran and
// failed apart from a repository nothing could be run in at all.
//
// Four conditions stop a repository being entered — no canonical checks, a dirty
// tree, a clone that is not there, a participant no longer in the manifest — and
// every one of them was counted as a failed check. The summary then said those
// checks were "recorded against the revisions they ran on" when nothing had run,
// which makes the record's evidentiary claim about checks that never executed.
// That is the one sentence a reader is entitled to trust.
//
// The specific reason stays beside each repository. This counts.
func verifySummary(failures, unverifiable int) string {
	switch {
	case failures > 0 && unverifiable > 0:
		return fmt.Sprintf("%s failed, recorded against the revisions they ran on; %s could not be verified",
			pluralise(failures, "check", "checks"),
			pluralise(unverifiable, "repository", "repositories"))
	case failures > 0:
		return fmt.Sprintf("%s failed; recorded against the revisions they ran on",
			pluralise(failures, "check", "checks"))
	case unverifiable > 0:
		return fmt.Sprintf("%s could not be verified; the reason is beside each one above",
			pluralise(unverifiable, "repository", "repositories"))
	}
	return ""
}
