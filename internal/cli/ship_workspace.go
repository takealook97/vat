package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// Judging the workspace rather than one changeset.
//
// Every workspace running vat wrote this by hand. Three of three had a
// scripts/shipping-gate.sh, two of them identical but for a marker string, and
// each answered a question `vat ship <id>` cannot be asked: not whether one
// verified bundle reached its branches, but whether this round is closed at all.
// The distinction matters because a bundle is not the unit anyone ships in — a
// round is — and because the repository most often left behind is the one no
// changeset names.
//
// One of those scripts states the rule it was written for: a round where the
// product went up and the canonical record stayed on somebody's laptop is not
// shipped. So every governed repository is judged, the brain and the workspace
// root included, rather than only what a changeset happened to enrol.
//
// This judges and changes nothing. A gate that tidied up would be another way to
// deploy rather than a gate — the same sentence CONTRIBUTING states as rule
// five, arrived at independently by somebody filling this hole.

// closure is one repository's answer to "is this closed".
type closure struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
	Closed bool   `json:"closed"`
	Detail string `json:"detail"`
}

type workspaceShipReport struct {
	Closed       bool      `json:"closed"`
	Repositories []closure `json:"repositories"`
	OpenChanges  []string  `json:"open_changesets,omitempty"`
}

func shipWorkspace(
	ctx context.Context, env *Env, ws *workspace.Workspace, remote string, offline bool,
) error {
	report := workspaceShipReport{Closed: true}

	// The root carries the manifest and the generated contracts, so a workspace
	// whose repositories all shipped while its own tree sat uncommitted has
	// shipped a roster nobody else can read.
	report.Repositories = append(report.Repositories,
		judgeClosure(ctx, ws.Root, ".", ws.Manifest.Workspace.DefaultBranch, remote, offline))

	for _, repo := range ws.Manifest.Active() {
		dir := ws.RepoPath(repo)
		if !gitx.IsRepository(dir) {
			report.Repositories = append(report.Repositories, closure{
				Repo: repo.Name, Detail: "not cloned, so nothing here can be judged",
			})
			continue
		}
		report.Repositories = append(report.Repositories,
			judgeClosure(ctx, dir, repo.Name, defaultBranchOf(ws.Manifest, repo), remote, offline))
	}

	for _, result := range report.Repositories {
		if !result.Closed {
			report.Closed = false
		}
	}

	// Open work is reported and does not fail the gate. A round can close with a
	// changeset still running — the two are different questions, and failing
	// here would make the gate unpassable in any workspace that keeps one open.
	if sets, err := changeset.LoadAll(ws.Root); err == nil {
		for _, set := range sets {
			if set.Status.Open() {
				report.OpenChanges = append(report.OpenChanges, set.ID)
			}
		}
	}

	if env.JSON {
		if err := emitJSON(env, report); err != nil {
			return err
		}
		if !report.Closed {
			return findingsErrorf("")
		}
		return nil
	}

	for _, result := range report.Repositories {
		level := ui.LevelOK
		if !result.Closed {
			level = ui.LevelFail
		}
		env.Printer.Status(level, result.Repo, result.Detail)
	}
	if len(report.OpenChanges) > 0 {
		env.Printer.Status(ui.LevelInfo, "open work",
			fmt.Sprintf("%s still open: %s",
				pluralise(len(report.OpenChanges), "changeset", "changesets"),
				strings.Join(report.OpenChanges, ", ")))
	}

	env.Printer.Heading("Result")
	outstanding := 0
	for _, result := range report.Repositories {
		if !result.Closed {
			outstanding++
		}
	}
	if outstanding > 0 {
		env.Printer.Status(ui.LevelFail, "ship", fmt.Sprintf("%s of %d not closed",
			pluralise(outstanding, "repository", "repositories"), len(report.Repositories)))
		return findingsErrorf("Commit and push the outstanding work, then run this again.")
	}
	env.Printer.Status(ui.LevelOK, "ship",
		fmt.Sprintf("every repository is level with %s", remote))
	return nil
}

// judgeClosure answers for one working tree: committed, on the branch it ships
// from, and level with that branch on the remote.
func judgeClosure(
	ctx context.Context, dir, name, branch, remote string, offline bool,
) closure {
	if branch == "" {
		branch = "main"
	}
	result := closure{Repo: name, Branch: branch}
	if !gitx.IsRepository(dir) {
		result.Detail = "not a git repository, so nothing here can be judged"
		return result
	}
	if !offline {
		// Judging against refs nobody refreshed answers about yesterday. A fetch
		// that fails is reported through the comparison below rather than here:
		// an unreachable remote is a reason to say so, not to stop.
		_ = gitx.Fetch(ctx, dir, remote)
	}

	dirty, err := gitx.IsDirty(ctx, dir)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	if dirty {
		result.Detail = "uncommitted changes, so what would ship is not what is here"
		return result
	}

	current, err := gitx.CurrentBranch(ctx, dir)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	if current == "" {
		result.Detail = "detached HEAD, so it is not on the branch it ships from"
		return result
	}
	if current != branch {
		result.Detail = fmt.Sprintf("on %s, not the %s it ships from", current, branch)
		return result
	}

	head, err := gitx.HeadRevision(ctx, dir)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	result.Head = head

	tracking := remote + "/" + branch
	// show-ref verifies a full ref path, not the shorthand a person types.
	if !gitx.HasRef(ctx, dir, "refs/remotes/"+tracking) {
		result.Detail = fmt.Sprintf("%s does not resolve, so nothing can say whether this shipped", tracking)
		return result
	}
	divergence, err := gitx.AheadBehind(ctx, dir, branch, tracking)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	switch {
	case divergence.Ahead > 0 && divergence.Behind > 0:
		result.Detail = fmt.Sprintf("diverged from %s: %d ahead, %d behind",
			tracking, divergence.Ahead, divergence.Behind)
	case divergence.Ahead > 0:
		result.Detail = fmt.Sprintf("%s exist only here, so this has not shipped",
			pluralise(divergence.Ahead, "commit", "commits"))
	case divergence.Behind > 0:
		// Behind is not unshipped, but it is not closed either: the tree that
		// was judged is not the tree the remote holds.
		result.Detail = fmt.Sprintf("%s behind %s, so this is not the tree that shipped",
			pluralise(divergence.Behind, "commit", "commits"), tracking)
	default:
		result.Closed = true
		result.Detail = "level with " + tracking + " at " + shortRevision(head)
	}
	return result
}

// defaultBranchOf reports the branch a repository ships from, falling back to
// the workspace's own default when it declares none.
func defaultBranchOf(m manifest.Manifest, repo manifest.Repo) string {
	if repo.DefaultBranch != "" {
		return repo.DefaultBranch
	}
	return m.Workspace.DefaultBranch
}
