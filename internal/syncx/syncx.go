// Package syncx implements the workspace update state machine.
//
// Updating a multi-repo workspace is not a loop of `git pull`. A pull that
// stashes, merges, or checks out on your behalf can destroy work that exists
// nowhere else, and a loop that ignores exit codes reports success while half
// the workspace failed. This package fetches, then advances only what can be
// advanced without losing anything, and reports every other state as itself.
package syncx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// State is the outcome for a single repository.
type State string

const (
	// StateCurrent means the default branch already matches its upstream.
	StateCurrent State = "CURRENT"
	// StateUpdated means a clean default branch was fast-forwarded.
	StateUpdated State = "UPDATED"
	// StateCloned means a missing repository was cloned from its origin.
	StateCloned State = "CLONED"
	// StateMissing means the clone is absent and was not created.
	StateMissing State = "MISSING"
	// StateNotGit means the directory exists but holds no repository. This is
	// never resolved automatically: the directory may hold unsaved work.
	StateNotGit State = "NOT_GIT"
	// StateRemoteMismatch means origin points somewhere the manifest does not
	// name, or is absent where the manifest names one. Treated as a
	// supply-chain signal, never silently rewritten.
	StateRemoteMismatch State = "REMOTE_MISMATCH"
	// StateNoRemote means neither the clone nor the manifest names a remote.
	// `vat repo new --no-remote` leaves exactly this, so it is a recorded fact
	// rather than a problem: there is nothing to fetch and nothing to compare.
	StateNoRemote State = "NO_REMOTE"
	// StateFetchFailed means the network step failed.
	StateFetchFailed State = "FETCH_FAILED"
	// StateDirty means uncommitted work is present, so nothing was advanced.
	StateDirty State = "DIRTY"
	// StateBranch means a non-default branch is checked out.
	StateBranch State = "BRANCH"
	// StateDetached means HEAD is not on a branch.
	StateDetached State = "DETACHED"
	// StateDiverged means both sides hold commits the other does not.
	StateDiverged State = "DIVERGED"
	// StateAhead means local commits are not on the remote. Never auto-pushed.
	StateAhead State = "AHEAD"
	// StateNoUpstream means the default branch has no remote-tracking ref.
	StateNoUpstream State = "NO_UPSTREAM"
	// StateArchived means the repository is retained in the manifest but
	// excluded from updates.
	StateArchived State = "ARCHIVED"
	// StatePlanned is what a dry run reports instead of acting.
	StatePlanned State = "PLANNED"
)

// StateNames lists every state a run can report, in the order the reference
// documents them. A state nobody has documented is one nobody can look up when
// it appears in their terminal, so a test compares this against that table.
func StateNames() []State {
	return []State{
		StateCurrent, StateUpdated, StateCloned, StateDirty, StateBranch,
		StateDetached, StateAhead, StateArchived, StatePlanned, StateNoRemote,
		StateMissing, StateNotGit, StateRemoteMismatch, StateFetchFailed,
		StateDiverged, StateNoUpstream,
	}
}

// Failure reports whether a state should make the command exit non-zero.
// Dirty trees, feature branches, and local-ahead branches are normal working
// states, not failures — reporting them as errors would train users to ignore
// the exit code.
func (s State) Failure() bool {
	switch s {
	case StateNotGit, StateRemoteMismatch, StateFetchFailed, StateDiverged,
		StateNoUpstream, StateMissing:
		return true
	default:
		return false
	}
}

// Result is one repository's outcome.
type Result struct {
	Repo     string `json:"repo"`
	State    State  `json:"state"`
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Detail   string `json:"detail,omitempty"`
	// Optional records that the manifest declared this repository
	// `required: false`, which downgrades a missing clone from a failure to a
	// note. lint has always drawn that distinction; sync did not, so an
	// optional repository broke every run it was absent from.
	Optional bool `json:"optional,omitempty"`
}

// Failed reports whether one result should count against the run.
//
// Missing is the only state whose severity depends on the manifest: a
// repository nobody promised would be here is not a broken workspace.
func (r Result) Failed() bool {
	if r.State == StateMissing && r.Optional {
		return false
	}
	return r.State.Failure()
}

// Options configure a sync run.
type Options struct {
	// DryRun reports the planned action without touching anything.
	DryRun bool
	// Offline skips every network operation, so the run reports local
	// structure only.
	Offline bool
	// Remote is the remote name to compare and fetch, normally "origin".
	Remote string
	// Parallelism caps concurrent git invocations.
	Parallelism int
}

// Report is a whole run.
type Report struct {
	Results  []Result `json:"results"`
	Failures int      `json:"failures"`
}

// Run executes the state machine across the selected repositories.
func Run(ctx context.Context, ws *workspace.Workspace, repos []manifest.Repo, opts Options) Report {
	remote := opts.Remote
	if remote == "" {
		remote = "origin"
	}
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = ws.Manifest.Policy.Sync.Parallelism
	}
	if parallelism <= 0 {
		parallelism = 1
	}

	results := make([]Result, len(repos))
	semaphore := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func(index int, repo manifest.Repo) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[index] = syncOne(ctx, ws, repo, remote, opts)
		}(i, repo)
	}
	wg.Wait()

	failures := 0
	for _, result := range results {
		if result.Failed() {
			failures++
		}
	}
	return Report{Results: results, Failures: failures}
}

func syncOne(ctx context.Context, ws *workspace.Workspace, repo manifest.Repo, remote string, opts Options) Result {
	name := repo.Name
	dir := ws.RepoPath(repo)
	branch := repo.Branch(ws.Manifest.Workspace.DefaultBranch)

	if repo.Archived {
		return Result{Repo: name, State: StateArchived, Detail: "excluded from updates"}
	}

	if !gitx.IsRepository(dir) {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return Result{Repo: name, State: StateNotGit,
				Detail: fmt.Sprintf("%s exists but is not a git repository", ws.Rel(dir))}
		}
		if opts.Offline {
			return Result{Repo: name, State: StateMissing, Optional: !repo.Required,
				Detail: "offline, clone skipped"}
		}
		if opts.DryRun {
			return Result{Repo: name, State: StatePlanned,
				Detail: fmt.Sprintf("clone %s", gitx.Redact(repo.Origin))}
		}
		if err := gitx.Clone(ctx, repo.Origin, dir); err != nil {
			return Result{Repo: name, State: StateMissing, Optional: !repo.Required,
				Detail: gitErrorDetail(err)}
		}
		// Read back rather than assumed: a clone lands on the remote's own HEAD,
		// which is not always the branch the manifest declares.
		current, _ := gitx.CurrentBranch(ctx, dir)
		revision, _ := gitx.ShortRevision(ctx, dir, "HEAD")
		return Result{Repo: name, State: StateCloned, Branch: displayBranch(current), Revision: revision}
	}

	actual, err := gitx.RemoteURL(ctx, dir, remote)
	if err != nil {
		// A repository the manifest itself records as having no remote is not a
		// supply-chain signal. Reporting it as one made every run fail forever
		// for a state `vat repo new --no-remote` deliberately produces.
		if manifest.IsLocalOrigin(repo.Origin) {
			// The branch the repository is on, never the one the manifest
			// declares: reporting the declaration makes this row and
			// `vat status` disagree about the same repository at the same
			// moment, and the wrong one is the one somebody would trust.
			current, _ := gitx.CurrentBranch(ctx, dir)
			revision, _ := gitx.ShortRevision(ctx, dir, "HEAD")
			return Result{Repo: name, State: StateNoRemote, Branch: displayBranch(current),
				Revision: revision, Detail: "no remote, as the manifest records"}
		}
		return Result{Repo: name, State: StateRemoteMismatch,
			Detail: fmt.Sprintf("no remote %q configured; the manifest names %s",
				remote, gitx.Redact(repo.Origin))}
	}
	if !gitx.SameRemote(actual, repo.Origin) {
		// Rewriting the remote here would turn a possible supply-chain problem
		// into a silent redirection of every future fetch.
		return Result{Repo: name, State: StateRemoteMismatch,
			Detail: fmt.Sprintf("origin is %s, manifest says %s",
				gitx.Redact(actual), gitx.Redact(repo.Origin))}
	}

	if !opts.Offline {
		if opts.DryRun {
			// Report the plan without contacting the network at all: a dry run
			// that fetches is not a dry run.
			return Result{Repo: name, State: StatePlanned,
				Detail: fmt.Sprintf("fetch %s, then fast-forward %s if clean and behind", remote, branch)}
		}
		if err := gitx.Fetch(ctx, dir, remote); err != nil {
			return Result{Repo: name, State: StateFetchFailed, Detail: gitErrorDetail(err)}
		}
	}

	revision, _ := gitx.ShortRevision(ctx, dir, "HEAD")
	current, err := gitx.CurrentBranch(ctx, dir)
	if err != nil {
		return Result{Repo: name, State: StateFetchFailed, Revision: revision, Detail: gitErrorDetail(err)}
	}

	dirty, err := gitx.IsDirty(ctx, dir)
	if err != nil {
		return Result{Repo: name, State: StateFetchFailed, Revision: revision, Detail: gitErrorDetail(err)}
	}
	if dirty {
		return Result{Repo: name, State: StateDirty, Branch: displayBranch(current),
			Revision: revision, Detail: dirtyDetail(dir)}
	}
	if current == "" {
		return Result{Repo: name, State: StateDetached, Revision: revision,
			Detail: "detached HEAD; nothing advanced"}
	}
	if current != branch {
		return Result{Repo: name, State: StateBranch, Branch: current, Revision: revision,
			Detail: fmt.Sprintf("on %s, not %s; nothing advanced", current, branch)}
	}

	upstream := remote + "/" + branch
	if !gitx.HasRef(ctx, dir, "refs/remotes/"+upstream) {
		return Result{Repo: name, State: StateNoUpstream, Branch: branch, Revision: revision,
			Detail: fmt.Sprintf("no %s", upstream)}
	}

	divergence, err := gitx.AheadBehind(ctx, dir, "HEAD", upstream)
	if err != nil {
		return Result{Repo: name, State: StateFetchFailed, Branch: branch, Revision: revision,
			Detail: gitErrorDetail(err)}
	}
	switch {
	case divergence.Diverged():
		return Result{Repo: name, State: StateDiverged, Branch: branch, Revision: revision,
			Ahead: divergence.Ahead, Behind: divergence.Behind,
			Detail: "history diverged; see git log --left-right HEAD..." + upstream}
	case divergence.Ahead > 0:
		return Result{Repo: name, State: StateAhead, Branch: branch, Revision: revision,
			Ahead: divergence.Ahead, Detail: "local commits not pushed"}
	case divergence.Behind == 0:
		return Result{Repo: name, State: StateCurrent, Branch: branch, Revision: revision}
	}

	if err := gitx.FastForward(ctx, dir, upstream); err != nil {
		return Result{Repo: name, State: StateFetchFailed, Branch: branch, Revision: revision,
			Detail: gitErrorDetail(err)}
	}
	advanced, _ := gitx.ShortRevision(ctx, dir, "HEAD")
	return Result{Repo: name, State: StateUpdated, Branch: branch, Revision: advanced,
		Behind: divergence.Behind}
}

func displayBranch(branch string) string {
	if branch == "" {
		return "(detached)"
	}
	return branch
}

// gitErrorDetail returns the first meaningful line of a git failure, redacted:
// git quotes the remote it could not reach, and that line is printed as-is.
func gitErrorDetail(err error) string {
	var cmdErr *gitx.CommandError
	if errors.As(err, &cmdErr) {
		for _, line := range strings.Split(cmdErr.Stderr, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				return gitx.Redact(trimmed)
			}
		}
	}
	return gitx.Redact(err.Error())
}

// SortResults orders results by manifest position for stable output, since the
// goroutines that produced them finish in arbitrary order.
func SortResults(results []Result, order []manifest.Repo) []Result {
	position := map[string]int{}
	for i, repo := range order {
		position[repo.Name] = i
	}
	sorted := make([]Result, len(results))
	copy(sorted, results)
	sort.SliceStable(sorted, func(i, j int) bool {
		return position[sorted[i].Repo] < position[sorted[j].Repo]
	})
	return sorted
}

// dirtyDetail says why the tree is dirty when git left an operation unfinished.
//
// "uncommitted changes" invites committing your way out of it, and when the
// tree is dirty because a merge or a rebase stopped part of the way through,
// what that commits is a file full of conflict markers.
func dirtyDetail(dir string) string {
	if operation := gitx.InterruptedOperation(dir); operation != "" {
		return "unfinished " + operation + "; resolve or abort it before committing"
	}
	return "uncommitted changes; nothing advanced"
}
