package cli

import (
	"context"
	"fmt"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// The preflight view. Verification refuses a dirty tree, on purpose: results
// filed against a revision that does not describe what was tested are the one
// thing a completion record must never hold. Adopting the harness dirties every
// governed repository at once, so a first changeset meets that refusal in eleven
// places and the only way to learn the order was to run verify and read the
// failures. This says it before anything runs, and commits nothing.

// participantState is where one participant stands before verification.
type participantState struct {
	Name string `json:"name"`
	// State is one of: uncommitted, unverified, moved, unverifiable, verified,
	// landed.
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	// Blocking marks a state that stops the changeset being verified as it
	// stands.
	Blocking bool `json:"blocking"`
}

type statusReport struct {
	ID           string             `json:"id"`
	Status       string             `json:"status"`
	Participants []participantState `json:"participants"`
	// CommitFirst names the participants that must be committed before
	// verification can describe anything, in enrolment order.
	CommitFirst []string `json:"commit_first,omitempty"`
}

func changesetStatusCommand() *Command {
	return &Command{
		Name:    "status",
		Summary: "Report what stands between a changeset and verification",
		Usage:   "vat changeset status <id>",
		Long: `Say where every participant stands, and commit nothing.

Verification refuses a dirty working tree, because results filed against a
revision that does not describe what was tested are the claim this record exists
to prevent. Adopting the harness dirties every governed repository at once, so a
first changeset meets that refusal everywhere and the order to fix it in was
only discoverable by running verify.

Six states are distinguished:

  uncommitted    the tree has changes, so no result would describe a revision
  unverified     committed, no checks recorded yet
  moved          verified, and the repository has moved on since
  unverifiable   declares no canonical checks, so nothing can be proven here
  verified       every recorded check passed at the recorded revision
  landed         that revision was observed on the branch it ships from

Nothing is committed for you. A command that committed to satisfy its own gate
would be deciding what a revision means.`,
		Run: runChangesetStatus,
	}
}

func runChangesetStatus(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("changeset status")
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

	report := statusReport{ID: current.ID, Status: string(current.Status)}
	for _, participant := range current.Repositories {
		state := describeParticipant(ctx, ws, participant)
		report.Participants = append(report.Participants, state)
		if state.State == "uncommitted" {
			report.CommitFirst = append(report.CommitFirst, participant.Name)
		}
	}
	if report.Participants == nil {
		report.Participants = []participantState{}
	}

	if env.JSON {
		return emitJSON(env, report)
	}
	for _, state := range report.Participants {
		level := ui.LevelOK
		if state.Blocking {
			level = ui.LevelWarn
		}
		detail := state.State
		if state.Detail != "" {
			detail += " · " + state.Detail
		}
		env.Printer.Status(level, state.Name, detail)
	}
	if len(report.CommitFirst) > 0 {
		env.Printer.Hint("\nCommit these before verifying, in this order:")
		for _, name := range report.CommitFirst {
			env.Printer.Hint("  git -C %s commit -am \"...\"", ws.Rel(participantDir(ws, name)))
		}
		env.Printer.Hint("\nNothing was committed. A command that committed to satisfy its own gate\nwould be deciding what a revision means.")
	}
	return nil
}

// participantDir is the path to print for a participant, falling back to the
// name when it resolves to nothing — the printed line is guidance, and guidance
// that disappears because a repository left the manifest is worse than a name.
func participantDir(ws *workspace.Workspace, name string) string {
	if target, ok := resolveParticipant(ws, name); ok {
		return target.Dir
	}
	return name
}

func describeParticipant(
	ctx context.Context, ws *workspace.Workspace, participant changeset.Participant,
) participantState {
	state := participantState{Name: participant.Name}
	target, ok := resolveParticipant(ws, participant.Name)
	if !ok {
		state.State, state.Blocking = "unresolved", true
		state.Detail = "no longer in the manifest"
		return state
	}
	if !gitx.IsRepository(target.Dir) {
		state.State, state.Blocking = "unresolved", true
		state.Detail = "not cloned"
		return state
	}
	dirty, err := gitx.IsDirty(ctx, target.Dir)
	if err != nil {
		state.State, state.Blocking = "unresolved", true
		state.Detail = "git cannot read the working tree"
		return state
	}
	if dirty {
		state.State, state.Blocking = "uncommitted", true
		state.Detail = dirtyPaths(ctx, target.Dir)
		return state
	}
	if participant.Unverifiable != "" {
		// Not blocking on its own: a workspace may accept that a repository has
		// nothing to prove. The record has to show it did, which is the point.
		state.State = "unverifiable"
		state.Detail = participant.Unverifiable
		return state
	}
	if !participant.Verified() {
		state.State, state.Blocking = "unverified", true
		if len(target.Checks) == 0 {
			state.Detail = "declares no canonical checks"
		}
		return state
	}
	head, err := gitx.HeadRevision(ctx, target.Dir)
	if err == nil && head != participant.Revision {
		// Verified, then moved. The checks still describe the revision they ran
		// on; what has stopped being true is that the revision is the one here.
		state.State, state.Blocking = "moved", true
		state.Detail = fmt.Sprintf("verified %s, now at %s",
			shortRev(participant.Revision), shortRev(head))
		return state
	}
	if participant.Landed() {
		state.State = "landed"
		state.Detail = participant.LandedOn + " on " + participant.LandedAt
		return state
	}
	state.State = "verified"
	state.Detail = shortRev(participant.Revision)
	return state
}
