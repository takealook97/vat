package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
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
		Usage:   "vat repo rename <old> <new> [--keep-path] [--origin <url>] [--plan]",
		Long: `Rename a governed repository.

The manifest entry, the directory, the .gitignore exclusion, the brain policy if
this repository is the knowledge layer, and the generated harness move together.
--keep-path renames only the manifest entry and records the existing directory
as an explicit path, which is what you want when other tooling depends on the
folder name.

A rename on the forge changes the URL the repository answers to, so --origin
records the new identity. The clone's remote is never rewritten: a remote that
does not match the manifest is a supply-chain signal, and vat reports it rather
than smoothing it over. Rename the remote yourself, or keep the old one — a
forge redirect means both routes work, and vat doctor names any extra remote it
finds.

--plan reports every effect and writes nothing.

A repository enrolled in an open changeset is refused. That record claims which
revisions were proven together, and renaming a participant out from under it
either breaks the record or rewrites a claim about the past.`,
		Examples: []string{
			"vat repo rename cortex brain --plan",
			"vat repo rename cortex brain --origin https://github.com/acme/brain.git",
		},
		Run: runRepoRename,
	}
}

func runRepoRename(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("repo rename")
	keepPath := set.Bool("keep-path", false, "rename the manifest entry only, leaving the directory alone")
	origin := set.String("origin", "", "record a new origin URL, as after a rename on the forge")
	plan := set.Bool("plan", false, "report every effect and write nothing")
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
	if *origin != "" {
		if err := manifest.ValidateRepoOrigin(*origin); err != nil {
			return usageErrorf("--origin: %v", err)
		}
		if manifest.HasEmbeddedCredential(*origin) {
			// Neither branch quotes the value: an origin is the field most
			// likely to hold a token, and an error message is not where that
			// surfaces.
			return usageErrorf("--origin embeds a credential; store it in your git credential helper and pass the plain URL")
		}
	}
	// Asked before anything is validated or moved. A changeset names its
	// participants by the manifest name, so a rename either leaves the record
	// pointing at a repository that is gone or rewrites what it claims was
	// verified — and the second is worse.
	if blocking := openChangesetsNaming(ws, oldName); len(blocking) > 0 {
		return usageErrorf(
			"%s is enrolled in %s, which is still open.\n"+
				"  A completion record names which revisions were proven together, so it is not\n"+
				"  rewritten by a rename. Close or abandon it first.",
			oldName, strings.Join(blocking, ", "))
	}

	oldDir := ws.RepoPath(repo)
	updated := repo
	updated.Name = newName
	if *origin != "" {
		updated.Origin = *origin
	}
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
	// The knowledge layer is named twice: once as a repository and once as the
	// policy that points at it. Moving only the first made the manifest invalid,
	// so renaming an adopted brain was impossible without editing vat.yaml by
	// hand — in the one command that exists to avoid exactly that.
	if next.Policy.Brain.Repo == oldName {
		next.Policy.Brain.Repo = newName
	}
	if err := manifest.Validate(next); err != nil {
		return usageErrorf("%v", err)
	}

	if *plan {
		reportRenamePlan(env, ws, repo, updated, *keepPath)
		return nil
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
	reportUncommittedContracts(env, ws)
	return nil
}

// reportUncommittedContracts names the repository contracts that are not yet in
// their own repository's history.
//
// The per-repository AGENTS.md is the working permit for a session opened
// inside that repository alone, so it does its job only once it is committed
// and travels with the clone. Nothing said so — and `vat changeset verify` then
// refuses to verify anything, on the file vat itself had written.
//
// It stops firing the moment the file is committed, so it cannot become the
// hint people learn to read past.
func reportUncommittedContracts(env *Env, ws *workspace.Workspace) {
	ctx := context.Background()
	var pending []string
	// Every repository, not only the ones this run rendered. Reporting what
	// changed here would name one contract and go quiet about the three
	// rendered by earlier commands and still uncommitted.
	for _, repo := range ws.Manifest.Repos {
		dir := ws.RepoPath(repo)
		if repo.Archived || !gitx.IsRepository(dir) || !fsx.Exists(filepath.Join(dir, "AGENTS.md")) {
			continue
		}
		// Tracked means the repository already carries it; whether it has
		// uncommitted edits is the working tree's business, not this hint's.
		tracked, err := gitx.IsTracked(ctx, dir, "AGENTS.md")
		if err != nil || tracked {
			continue
		}
		// A repository that ignores the path has decided, and a hint that
		// cannot be satisfied is one people learn to read past.
		if gitx.Ignores(ctx, dir, "AGENTS.md") {
			continue
		}
		pending = append(pending, path.Join(repo.Dir(), "AGENTS.md"))
	}
	if len(pending) == 0 {
		return
	}
	sentence := "Commit %s in its repository. It is the contract a session opened there reads."
	if len(pending) > 1 {
		sentence = "Commit %s in their repositories. They are the contract a session opened in one reads."
	}
	env.Printer.Hint(sentence, strings.Join(pending, ", "))
}

// openChangesetsNaming returns the identifiers of open changesets that enrol a
// repository. A record that is closed, rolled back, or abandoned describes the
// past under the name it used then, and is left exactly as it is.
func openChangesetsNaming(ws *workspace.Workspace, name string) []string {
	sets, err := changeset.LoadAll(ws.Root)
	if err != nil {
		// Unreadable records are `vat lint`'s finding. Refusing a rename over
		// one would make an unrelated defect look like a rule about renaming.
		return nil
	}
	var blocking []string
	for _, set := range sets {
		if !set.Status.Open() {
			continue
		}
		if _, enrolled := set.Participant(name); enrolled {
			blocking = append(blocking, set.ID)
		}
	}
	return blocking
}

// reportRenamePlan says what a rename would touch, in the order it would touch
// it. Nothing is written.
func reportRenamePlan(
	env *Env, ws *workspace.Workspace, from, to manifest.Repo, keepPath bool,
) {
	env.Printer.Status(ui.LevelInfo, manifest.FileName,
		fmt.Sprintf("repos entry %s → %s", from.Name, to.Name))
	if from.Origin != to.Origin {
		env.Printer.Status(ui.LevelInfo, manifest.FileName,
			fmt.Sprintf("origin %s → %s", gitx.Redact(from.Origin), gitx.Redact(to.Origin)))
	}
	if ws.Manifest.Policy.Brain.Repo == from.Name {
		env.Printer.Status(ui.LevelInfo, manifest.FileName,
			fmt.Sprintf("policy.brain.repo %s → %s", from.Name, to.Name))
	}
	switch {
	case keepPath:
		env.Printer.Status(ui.LevelSkip, ws.Rel(ws.RepoPath(from)),
			"left where it is; recorded as an explicit path")
	case fsx.Exists(ws.RepoPath(from)):
		env.Printer.Status(ui.LevelInfo, ws.Rel(ws.RepoPath(from)),
			"directory moves to "+ws.Rel(ws.RepoPath(to)))
	default:
		env.Printer.Status(ui.LevelSkip, from.Name, "not cloned, so no directory moves")
	}
	env.Printer.Status(ui.LevelInfo, ".gitignore", "exclusion follows the directory")
	env.Printer.Status(ui.LevelInfo, "AGENTS.md", "generated regions are rewritten from the manifest")
	// Said even when there is no remote to speak of: the reader is deciding
	// what to do on the forge, and the answer is the same either way.
	env.Printer.Hint("\nvat never rewrites a remote. Rename it yourself, or keep the old one:\n"+
		"  git -C %s remote set-url origin <new-url>", ws.Rel(ws.RepoPath(to)))
	env.Printer.Hint("\nNothing was written. Run again without --plan to apply.")
}
