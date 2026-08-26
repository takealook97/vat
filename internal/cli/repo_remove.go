package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// Removing a repository, and the guards standing between that and losing
// work that exists nowhere else.

func repoRemoveCommand() *Command {
	return &Command{
		Name:    "remove",
		Summary: "Stop governing a repository, with checks against losing work",
		Usage:   "vat repo remove <name> [--delete] [--force]",
		Long: `Remove a repository from the manifest.

Before doing anything, vat looks for work that exists only on this machine:
uncommitted changes, commits no remote has, and stashes. If it finds any, the
removal is refused. --force overrides that check and is the only way past it.

By default the directory is left on disk — unregistering and deleting are
different decisions. --delete removes it too, and always asks for confirmation
even with --yes, because it is the one action here that cannot be undone.`,
		Examples: []string{
			"vat repo remove legacy-api",
			"vat repo remove scratch --delete",
		},
		Run: runRepoRemove,
	}
}

func runRepoRemove(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("repo remove")
	deleteFiles := set.Bool("delete", false, "also delete the directory from disk")
	force := set.Bool("force", false, "proceed even if unsaved or unpushed work is found")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return usageErrorf("expected exactly one repository name")
	}
	name := set.Arg(0)

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	repo, exists := ws.Manifest.Find(name)
	if !exists {
		return usageErrorf("%s is not in %s", name, manifest.FileName)
	}
	dir := ws.RepoPath(repo)

	if gitx.IsRepository(dir) {
		risks := unsavedWork(ctx, dir)
		if len(risks) > 0 {
			for _, risk := range risks {
				env.Printer.Status(ui.LevelFail, name, risk)
			}
			if !*force {
				return findingsErrorf("Refusing to remove %s. Push or discard the work above, or pass --force.", name)
			}
			env.Printer.Status(ui.LevelWarn, name, "proceeding anyway because --force was given")
		}
	}

	// The prompt comes before the manifest changes. Asking afterwards meant
	// declining left the repository de-registered and dropped from the managed
	// .gitignore region, so the still-present directory became
	// untracked-but-not-ignored and the workspace's next `git add .` absorbed it.
	deleteApproved := false
	if *deleteFiles && fsx.Exists(dir) {
		// This is the one call in vat that deletes a tree, so it checks
		// containment itself rather than trusting validation upstream.
		if !workspace.Contains(ws.Root, dir) {
			return findingsErrorf(
				"Refusing to delete %s: it is the workspace root, or outside it.", dir)
		}
		// Deleting a working tree is irreversible, so --yes deliberately does
		// not cover it. This prompt is the last thing between a typo and lost
		// work.
		deleteApproved = confirm(env,
			fmt.Sprintf("Permanently delete %s and everything in it?", ws.Rel(dir)))
		if !deleteApproved {
			env.Printer.Status(ui.LevelSkip, name,
				"deletion declined; nothing was changed")
			return nil
		}
	}

	next, removed := manifest.WithoutRepo(ws.Manifest, name)
	if !removed {
		return usageErrorf("%s is not in %s", name, manifest.FileName)
	}
	if err := commitManifest(env, ws, next); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, name, "removed from the manifest")

	switch {
	case deleteApproved:
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("delete %s: %w", dir, err)
		}
		env.Printer.Status(ui.LevelOK, name, "directory deleted")
	case fsx.Exists(dir):
		env.Printer.Hint("      → %s is still on disk; delete it yourself when you are sure.", ws.Rel(dir))
	}
	return renderAfterChange(env, ws.Root)
}

// unsavedWork reports everything in a repository that exists nowhere else.
// unsavedWork reports everything in a repository that exists nowhere else.
//
// Every check fails closed. A git command that cannot answer — a locked index,
// a dubious-ownership refusal, a corrupt repository — is reported as a risk
// rather than as an absence of one, because the alternative is deleting a tree
// whose contents could not be inspected.
func unsavedWork(ctx context.Context, dir string) []string {
	var risks []string
	dirty, err := gitx.IsDirty(ctx, dir)
	switch {
	case err != nil:
		risks = append(risks, "git cannot read the working tree state")
	case dirty:
		risks = append(risks, "uncommitted changes in the working tree")
	}
	unpushed, err := gitx.UnpushedCommits(ctx, dir)
	switch {
	case err != nil:
		risks = append(risks, "git cannot tell which commits are on a remote")
	case unpushed > 0:
		risks = append(risks, pluralise(unpushed, "commit", "commits")+" not on any remote")
	}
	// Stashes are invisible to `git status`, which is exactly why they are the
	// work most often destroyed by a cleanup.
	stashes, err := gitx.StashCount(ctx, dir)
	switch {
	case err != nil:
		risks = append(risks, "git cannot read the stash list")
	case stashes > 0:
		risks = append(risks, pluralise(stashes, "stash entry", "stash entries"))
	}
	return risks
}

func confirm(env *Env, question string) bool {
	_, _ = fmt.Fprintf(env.Printer.Out(), "%s [y/N] ", question)
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
