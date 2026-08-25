package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

func statusCommand() *Command {
	return &Command{
		Name:    "status",
		Summary: "Show branch, cleanliness, and revision for every repository",
		Usage:   "vat status [--only <names>] [--group <g>] [--role <r>] [--dirty] [--fetch] [--archived]",
		Long: `Print the current state of every governed repository on one screen.

By default nothing touches the network, so this is safe to run constantly. With
--fetch it first updates remote-tracking refs, which makes the ahead/behind
counts current at the cost of a round trip per repository.`,
		Examples: []string{
			"vat status",
			"vat status --dirty          # only repositories with uncommitted work",
			"vat status --group backend",
		},
		Run: runStatus,
	}
}

// repoStatus is one repository's observable state.
type repoStatus struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Group    string `json:"group,omitempty"`
	Present  bool   `json:"present"`
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
	Dirty    bool   `json:"dirty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Stashes  int    `json:"stashes,omitempty"`
	// Unreadable marks a repository git could not be questioned about. It is
	// reported as its own state rather than folded into "clean".
	Unreadable bool   `json:"unreadable,omitempty"`
	Note       string `json:"note,omitempty"`
}

func runStatus(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("status")
	group := set.String("group", "", "only repositories in these groups (comma-separated)")
	role := set.String("role", "", "only repositories with these roles")
	only := set.String("only", "", "only these repositories by name")
	dirtyOnly := set.Bool("dirty", false, "only repositories with uncommitted work")
	fetch := set.Bool("fetch", false, "update remote-tracking refs first")
	archived := set.Bool("archived", false, "include archived repositories")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	repos, err := ws.Select(manifest.Selector{
		Names:  append(splitList(*only), set.Args()...),
		Groups: splitList(*group), Roles: splitList(*role),
		IncludeArchive: *archived,
	})
	if err != nil {
		return usageErrorf("%v", err)
	}

	statuses := collectStatuses(ctx, ws, repos, *fetch)
	if *dirtyOnly {
		filtered := statuses[:0]
		for _, status := range statuses {
			if status.Dirty || status.Ahead > 0 || status.Stashes > 0 {
				filtered = append(filtered, status)
			}
		}
		statuses = filtered
	}

	if env.JSON {
		encoder := json.NewEncoder(env.Printer.Out())
		encoder.SetIndent("", "  ")
		return encoder.Encode(statuses)
	}
	renderStatusTable(env, ws, statuses)
	return nil
}

func collectStatuses(ctx context.Context, ws *workspace.Workspace, repos []manifest.Repo, fetch bool) []repoStatus {
	statuses := make([]repoStatus, len(repos))
	parallelism := ws.Manifest.Policy.Sync.Parallelism
	if parallelism <= 0 {
		parallelism = 8
	}
	semaphore := make(chan struct{}, parallelism)
	var wg sync.WaitGroup

	for i, repo := range repos {
		wg.Add(1)
		go func(index int, repo manifest.Repo) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			statuses[index] = describeStatus(ctx, ws, repo, fetch)
		}(i, repo)
	}
	wg.Wait()
	return statuses
}

func describeStatus(ctx context.Context, ws *workspace.Workspace, repo manifest.Repo, fetch bool) repoStatus {
	status := repoStatus{Name: repo.Name, Role: string(repo.Role), Group: repo.Group}
	dir := ws.RepoPath(repo)
	if repo.Archived {
		status.Note = "archived"
	}
	if !gitx.IsRepository(dir) {
		status.Note = "not cloned"
		return status
	}
	status.Present = true

	if fetch {
		if err := gitx.Fetch(ctx, dir, "origin"); err != nil {
			status.Note = "fetch failed"
		}
	}
	// A git failure here is not the same as a clean repository. Discarding it
	// would report an unreadable or permission-denied repository as clean and
	// detached, which is the one answer a status command must never give.
	branch, err := gitx.CurrentBranch(ctx, dir)
	if err != nil {
		status.Unreadable = true
		status.Note = "unreadable: " + firstLine(err.Error())
		return status
	}
	status.Branch = branch
	if branch == "" {
		status.Branch = "(detached)"
	}
	if status.Revision, err = gitx.ShortRevision(ctx, dir, "HEAD"); err != nil {
		status.Unreadable = true
		status.Note = "unreadable: " + firstLine(err.Error())
		return status
	}
	if status.Dirty, err = gitx.IsDirty(ctx, dir); err != nil {
		status.Unreadable = true
		status.Note = "unreadable: " + firstLine(err.Error())
		return status
	}
	if status.Stashes, err = gitx.StashCount(ctx, dir); err != nil {
		status.Note = "stash list unavailable"
	}

	if branch != "" {
		upstream := "origin/" + branch
		if gitx.HasRef(ctx, dir, "refs/remotes/"+upstream) {
			if divergence, err := gitx.AheadBehind(ctx, dir, "HEAD", upstream); err == nil {
				status.Ahead, status.Behind = divergence.Ahead, divergence.Behind
			}
		} else if status.Note == "" {
			status.Note = "no upstream"
		}
	}
	expected := repo.Branch(ws.Manifest.Workspace.DefaultBranch)
	if branch != "" && branch != expected && status.Note == "" {
		status.Note = "not on " + expected
	}
	return status
}

func renderStatusTable(env *Env, ws *workspace.Workspace, statuses []repoStatus) {
	if len(statuses) == 0 {
		env.Printer.Println("No repositories match.")
		return
	}
	rows := make([][]string, 0, len(statuses))
	dirty, ahead, behind, missing, unreadable := 0, 0, 0, 0, 0
	for _, status := range statuses {
		state := "clean"
		switch {
		case !status.Present:
			state = "missing"
			missing++
		case status.Unreadable:
			state = "unreadable"
			unreadable++
		case status.Dirty:
			state = "dirty"
			dirty++
		}
		if status.Ahead > 0 {
			ahead++
		}
		if status.Behind > 0 {
			behind++
		}
		rows = append(rows, []string{
			status.Name,
			status.Branch,
			status.Revision,
			state,
			formatDivergence(status),
			status.Note,
		})
	}
	env.Printer.Table([]string{"REPOSITORY", "BRANCH", "REV", "TREE", "VS ORIGIN", "NOTE"}, rows)

	var summary []string
	summary = append(summary, fmt.Sprintf("%d repositories", len(statuses)))
	if missing > 0 {
		summary = append(summary, fmt.Sprintf("%d not cloned", missing))
	}
	if unreadable > 0 {
		summary = append(summary, fmt.Sprintf("%d unreadable", unreadable))
	}
	if dirty > 0 {
		summary = append(summary, fmt.Sprintf("%d dirty", dirty))
	}
	if ahead > 0 {
		summary = append(summary, fmt.Sprintf("%d ahead", ahead))
	}
	if behind > 0 {
		summary = append(summary, fmt.Sprintf("%d behind", behind))
	}
	env.Printer.Hint("\n%s · workspace %s", strings.Join(summary, " · "), ws.Manifest.Workspace.Name)
	// Suggested only when there is something for it to advance; recommending it
	// while everything is ahead or diverged proposes a no-op.
	if behind > 0 || missing > 0 {
		env.Printer.Hint("Run `vat sync` to fast-forward what can be advanced safely.")
	}
}

// firstLine returns the first non-empty line of a message, so a multi-line git
// error stays inside one table cell.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return text
}

func formatDivergence(status repoStatus) string {
	switch {
	case !status.Present:
		return ""
	case status.Ahead > 0 && status.Behind > 0:
		return fmt.Sprintf("+%d/-%d diverged", status.Ahead, status.Behind)
	case status.Ahead > 0:
		return fmt.Sprintf("+%d", status.Ahead)
	case status.Behind > 0:
		return fmt.Sprintf("-%d", status.Behind)
	default:
		return "="
	}
}
