package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// The last question a changeset has to answer is not "did the checks pass" but
// "did the combination that passed them reach everybody else". Those are
// different questions, and a workspace that only ever asks the first accumulates
// changesets closed on work still sitting on a branch.
//
// The gate is `merge-base --is-ancestor`, and deliberately nothing more. A pull
// request is a forge's own idea — GitHub, GitLab, and Gerrit each name and model
// it differently — so gating on one would buy a dependency, tie vat to a single
// vendor, and still not answer the question, because an open pull request means
// precisely that the change has not landed. "Is an ancestor of the branch this
// repository ships from" is the same sentence everywhere git runs.

func shipCommand() *Command {
	return &Command{
		Name:    "ship",
		Summary: "Judge whether a changeset's verified revisions have landed",
		Usage:   "vat ship <id> [--remote <name>] [--offline]",
		Long: `Report, for every repository in a changeset, whether the revision its checks
passed on has reached the branch that repository ships from.

vat pushes nothing and merges nothing. This judges; landing the work is yours.

The test is whether the verified revision is an ancestor of the tracking ref for
the repository's default branch, which is a plain git question with the same
answer on every forge. A pull request is recorded as evidence when you supply
one, and is never the gate: an open pull request is the state of not having
landed.

Every repository is reported in one pass, because this is run in a loop while a
change is being landed and one finding per run makes that loop unusable.`,
		Examples: []string{
			"vat ship CS-0007",
			"vat ship CS-0007 --offline    # judge against refs already fetched",
		},
		Run: runShip,
	}
}

// landing is one repository's answer, in the order the reader needs it: which
// repository, what it shipped, and where that was looked for.
type landing struct {
	Repo     string `json:"repo"`
	Revision string `json:"revision,omitempty"`
	Ref      string `json:"ref"`
	Landed   bool   `json:"landed"`
	// Observed distinguishes "we looked and it is not there" from "we could not
	// look". Both fail the gate, and telling somebody to land work that is
	// already landed — because a ref was missing — sends them at the wrong
	// problem entirely.
	Observed bool   `json:"observed"`
	Detail   string `json:"detail,omitempty"`
}

type shipReport struct {
	Changeset string    `json:"changeset"`
	Landed    bool      `json:"landed"`
	Repos     []landing `json:"repos"`
}

func runShip(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("ship")
	remote := set.String("remote", "origin", "remote whose branches count as landed")
	offline := set.Bool("offline", false, "judge against refs already fetched, without contacting the remote")
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
	// Re-judging a finished changeset would overwrite its landing evidence with
	// today's answer — and a branch rewound since it closed would erase the
	// record of a change that did ship. `verify` refuses a closed changeset for
	// the same reason: a completion record is not a live view.
	if !current.Status.Open() {
		return usageErrorf(
			"%s is %s; re-judging it would overwrite the landing evidence it closed with.\n"+
				"  Open a new changeset for further work.", current.ID, current.Status)
	}
	// Landing an unverified changeset would record that something reached a
	// default branch without any record of what proved it worked — which is the
	// state the whole layer exists to stop being invisible.
	if !current.FullyVerified() {
		for _, participant := range current.Repositories {
			if !participant.Verified() {
				env.Printer.Status(ui.LevelFail, participant.Name,
					"no passing checks recorded at a known revision")
			}
		}
		return findingsErrorf("Nothing is proven yet. Run `vat changeset verify %s` first.", current.ID)
	}

	report := shipReport{Changeset: current.ID, Landed: true}
	for _, participant := range current.Repositories {
		result, updated := judgeLanding(ctx, env, ws, participant, *remote, *offline)
		current = changeset.WithParticipant(current, updated)
		report.Repos = append(report.Repos, result)
		if !result.Landed {
			report.Landed = false
		}
	}
	// A cancelled run has judged nothing it can stand behind: every git call
	// after the interrupt fails instantly on the dead context, so writing now
	// would file those failures as findings.
	if err := ctx.Err(); err != nil {
		return err
	}
	// The observations are worth keeping even when the answer is no: a partial
	// landing is exactly the state somebody needs to see the next morning.
	if err := changeset.Save(ws.Root, current); err != nil {
		return err
	}
	if env.JSON {
		if err := emitJSON(env, report); err != nil {
			return err
		}
		if !report.Landed {
			return findingsErrorf("")
		}
		return nil
	}

	for _, result := range report.Repos {
		level, detail := ui.LevelOK, "landed on "+result.Ref
		if !result.Landed {
			level, detail = ui.LevelFail, result.Detail
		}
		env.Printer.Status(level, result.Repo, detail)
	}
	env.Printer.Heading("Result")
	if unlanded := countUnlanded(report); unlanded > 0 {
		env.Printer.Status(ui.LevelFail, "ship", fmt.Sprintf("%s of %d not confirmed landed",
			pluralise(unlanded, "repository", "repositories"), len(report.Repos)))
		// Only tell somebody to land work when something was actually observed
		// to be unlanded. When nothing could be judged, the work may well have
		// shipped and the problem is the ref.
		for _, result := range report.Repos {
			if !result.Landed && result.Observed {
				return findingsErrorf("Land the outstanding work, then run this again.")
			}
		}
		return findingsErrorf("Nothing could be judged. Check the remote and the branch each repository ships from.")
	}
	env.Printer.Status(ui.LevelOK, "ship", fmt.Sprintf("every repository landed on %s", *remote))
	env.Printer.Hint("\nNext: vat changeset close %s --acceptance \"...\"", current.ID)
	return nil
}

// judgeLanding answers for one repository and returns the participant with what
// was observed recorded on it.
func judgeLanding(
	ctx context.Context, env *Env, ws *workspace.Workspace,
	participant changeset.Participant, remote string, offline bool,
) (landing, changeset.Participant) {
	// The previous answer is kept until something is actually observed. Landing
	// is a claim about now, so an observation that contradicts it must clear it
	// — but *failing to look* is not an observation. Clearing up front meant a
	// run with no network, or one where a repository was briefly not cloned,
	// erased the evidence of a change that really had shipped, and no later run
	// could put it back.
	updated := participant
	result := landing{Repo: participant.Name, Revision: participant.Revision}
	repo, ok := ws.Manifest.Find(participant.Name)
	if !ok {
		result.Detail = "no longer in the manifest, so there is no branch to look on"
		return result, updated
	}
	branch := repo.Branch(ws.Manifest.Workspace.DefaultBranch)
	result.Ref = remote + "/" + branch

	dir := ws.RepoPath(repo)
	if !gitx.IsRepository(dir) {
		result.Detail = "not cloned, so nothing can be observed"
		return result, updated
	}
	if !offline {
		if err := gitx.Fetch(ctx, dir, remote); err != nil {
			// Reporting the repository as unlanded here would be a lie about
			// the branch rather than about the network. Say which it was.
			result.Detail = "could not reach " + remote + ": " + gitx.Redact(err.Error())
			return result, updated
		}
	}
	landed, err := gitx.IsAncestor(ctx, dir, participant.Revision, result.Ref)
	switch {
	case errors.Is(err, gitx.ErrRevisionNotFound):
		// Saying "not landed" here would be a claim about the branch. The truth
		// is about the ref: it is not in this clone, so nothing was observed.
		result.Detail = result.Ref + " is not present in this clone, so landing could not be judged"
		return result, updated
	case err != nil:
		result.Detail = "git could not compare against " + result.Ref + ": " + gitx.Redact(err.Error())
		return result, updated
	case !landed:
		result.Observed = true
		result.Detail = shortRev(participant.Revision) + " is not on " + result.Ref +
			"; verified but not landed"
		updated.LandedOn, updated.LandedAt = "", ""
		return result, updated
	}
	result.Landed, result.Observed = true, true
	updated.LandedOn = result.Ref
	updated.LandedAt = env.Now.Format("2006-01-02")
	return result, updated
}

func countUnlanded(report shipReport) int {
	count := 0
	for _, result := range report.Repos {
		if !result.Landed {
			count++
		}
	}
	return count
}
