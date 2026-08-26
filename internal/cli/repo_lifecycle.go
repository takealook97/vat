package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// Archiving, restoring, and renaming: the operations that change a
// repository's standing in the workspace without adding or removing one.

func repoArchiveCommand() *Command {
	return &Command{
		Name:    "archive",
		Summary: "Keep a repository in the record but exclude it from daily commands",
		Usage:   "vat repo archive <name>",
		Long: `Mark a repository archived.

It stays in the manifest, so the record of what this workspace once governed
survives, but sync, status, and exec skip it. This is what to reach for when a
repository is finished rather than gone.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			return setArchived(env, args, true)
		},
	}
}

func repoUnarchiveCommand() *Command {
	return &Command{
		Name:    "unarchive",
		Summary: "Return an archived repository to active use",
		Usage:   "vat repo unarchive <name>",
		Run: func(ctx context.Context, env *Env, args []string) error {
			return setArchived(env, args, false)
		},
	}
}

func setArchived(env *Env, args []string, archived bool) error {
	set := newFlagSet("repo archive")
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
	repo.Archived = archived
	if err := commitManifest(env, ws, manifest.WithRepo(ws.Manifest, repo)); err != nil {
		return err
	}
	state := "archived"
	if !archived {
		state = "active again"
	}
	env.Printer.Status(ui.LevelOK, name, state)
	return renderAfterChange(env, ws.Root)
}

func repoRenameCommand() *Command {
	return &Command{
		Name:    "rename",
		Summary: "Rename a repository in the manifest and on disk",
		Usage:   "vat repo rename <old> <new> [--keep-path]",
		Long: `Rename a governed repository.

The manifest entry, the directory, the .gitignore exclusion, and the generated
harness move together. --keep-path renames only the manifest entry and records
the existing directory as an explicit path, which is what you want when other
tooling depends on the folder name.`,
		Run: runRepoRename,
	}
}

func runRepoRename(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("repo rename")
	keepPath := set.Bool("keep-path", false, "rename the manifest entry only, leaving the directory alone")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() != 2 {
		return usageErrorf("expected an old name and a new name")
	}
	oldName, newName := set.Arg(0), set.Arg(1)

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	repo, exists := ws.Manifest.Find(oldName)
	if !exists {
		return usageErrorf("%s is not in %s", oldName, manifest.FileName)
	}
	if _, taken := ws.Manifest.Find(newName); taken {
		return usageErrorf("%s is already in %s", newName, manifest.FileName)
	}

	oldDir := ws.RepoPath(repo)
	updated := repo
	updated.Name = newName
	if *keepPath {
		updated.Path = repo.Dir()
	} else {
		updated.Path = ""
	}

	// The manifest is validated before anything moves. Renaming first and
	// validating after would relocate a working tree for a name the manifest
	// then rejects, leaving it outside the workspace with nothing pointing at
	// it — `vat repo rename payments ../../payments` did exactly that.
	without, _ := manifest.WithoutRepo(ws.Manifest, oldName)
	next := manifest.WithRepo(without, updated)
	if err := manifest.Validate(next); err != nil {
		return usageErrorf("%v", err)
	}

	moved := false
	newDir := ws.RepoPath(updated)
	if !*keepPath {
		if !workspace.Contains(ws.Root, newDir) {
			return usageErrorf("%s would sit outside the workspace", newName)
		}
		if fsx.Exists(oldDir) {
			if fsx.Exists(newDir) {
				return usageErrorf("%s already exists on disk", ws.Rel(newDir))
			}
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("rename %s: %w", ws.Rel(oldDir), err)
			}
			moved = true
		}
	}

	// The directory has to move before the manifest is written, or a crash
	// between the two leaves a manifest naming a directory that is not there.
	// Moving first has the opposite risk, so a failure puts everything back.
	//
	// commitManifest writes two files, and the second can fail after the first
	// succeeded -- so the manifest is restored as well as the directory. Two
	// files cannot be written atomically without a journal, and a journal is
	// more machinery than this earns; what it can do is leave nothing
	// half-applied, and say exactly what to repair when even that fails.
	previous := ws.Manifest
	if err := commitManifest(env, ws, next); err != nil {
		var unrepaired []string
		if restore := commitManifest(env, ws, previous); restore != nil {
			unrepaired = append(unrepaired,
				fmt.Sprintf("%s still names %s: %v", manifest.FileName, newName, restore))
		}
		if moved {
			if back := os.Rename(newDir, oldDir); back != nil {
				unrepaired = append(unrepaired,
					fmt.Sprintf("the directory is still %s: %v", ws.Rel(newDir), back))
			}
		}
		if len(unrepaired) > 0 {
			return fmt.Errorf("%w\n  the workspace could not be put back:\n    %s\n  repair this by hand before running vat again",
				err, strings.Join(unrepaired, "\n    "))
		}
		env.Printer.Status(ui.LevelInfo, oldName, "put back; nothing was changed")
		return err
	}
	env.Printer.Status(ui.LevelOK, newName, "renamed from "+oldName)
	return renderAfterChange(env, ws.Root)
}

// commitManifest writes the manifest and keeps .gitignore in step, so the two
// can never disagree about what the workspace governs.

func commitManifest(env *Env, ws *workspace.Workspace, next manifest.Manifest) error {
	if err := ws.SaveManifest(next); err != nil {
		return err
	}
	changed, err := ws.SyncGitignore(next)
	if err != nil {
		return err
	}
	if changed {
		env.Printer.Status(ui.LevelOK, ".gitignore", "updated")
	}
	return nil
}

// renderAfterChange regenerates the harness from the new manifest.

func renderAfterChange(env *Env, root string) error {
	ws, err := workspace.OpenAt(root)
	if err != nil {
		return err
	}
	changed, err := lint.RenderHarness(ws)
	if err != nil {
		return err
	}
	for _, file := range changed {
		env.Printer.Status(ui.LevelOK, file, "regenerated")
	}
	return nil
}
