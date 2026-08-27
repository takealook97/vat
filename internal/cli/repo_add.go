package cli

import (
	"context"
	"strings"

	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
)

// Enrolling a repository that already exists somewhere else.

func repoAddCommand() *Command {
	return &Command{
		Name:    "add",
		Summary: "Enrol an existing remote repository and clone it",
		Usage:   "vat repo add <name> --origin <url> [--role <r>] [--group <g>] [--branch <b>] [--checks <cmds>] [--access <a>] [--description <text>] [--required=false] [--path <dir>] [--no-clone]",
		Long: `Bring an existing remote repository under the workspace.

The manifest entry, the .gitignore exclusion, and the generated harness are
written together, then the repository is cloned.`,
		Examples: []string{
			"vat repo add payments --origin https://github.com/acme/payments.git --group backend",
			`vat repo add brain --origin git@github.com:acme/brain.git --role brain`,
		},
		Run: runRepoAdd,
	}
}

func runRepoAdd(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("repo add")
	origin := set.String("origin", "", "the repository's canonical remote URL (required)")
	path := set.String("path", "", "directory name under the workspace (default: the repository name)")
	noClone := set.Bool("no-clone", false, "register without cloning")
	fields := bindRepoFlags(set)
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return usageErrorf("expected exactly one repository name")
	}
	name := set.Arg(0)
	if strings.TrimSpace(*origin) == "" {
		return usageErrorf("--origin is required")
	}
	if manifest.HasEmbeddedCredential(*origin) {
		return usageErrorf(
			"--origin embeds a credential; %s is committed, so it records identity and never access.\n"+
				"  Keep the token in your git credential helper and pass the plain URL.", manifest.FileName)
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	if _, exists := ws.Manifest.Find(name); exists {
		return usageErrorf("%s is already in %s", name, manifest.FileName)
	}

	repo, err := fields.apply(manifest.Repo{Name: name, Origin: *origin, Path: *path})
	if err != nil {
		return err
	}
	next := manifest.WithRepo(ws.Manifest, repo)
	if err := commitManifest(env, ws, next); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, name, "registered as "+string(repo.Role))

	if *noClone {
		// The generated contract is rendered from the manifest, not from the
		// working trees, so it went stale the moment the manifest changed —
		// whether or not anything was cloned. Returning here left `vat repo
		// add --no-clone` reporting success and `vat lint` reporting drift
		// immediately afterwards, from the one command documented to render.
		if err := renderAfterChange(env, ws.Root); err != nil {
			return err
		}
		env.Printer.Hint("Not cloned. Run `vat sync` when you want it on disk.")
		return nil
	}
	dir := ws.RepoPath(repo)
	if gitx.IsRepository(dir) {
		env.Printer.Status(ui.LevelInfo, name, "already present, not cloned again")
	} else if err := gitx.Clone(ctx, repo.Origin, dir); err != nil {
		// The manifest entry is kept: the registration is correct even though
		// the clone failed, and removing it would lose the user's input.
		env.Printer.Status(ui.LevelFail, name, "clone failed: "+err.Error())
		return findingsErrorf("Registered, but not cloned. Fix access and run `vat sync`.")
	} else {
		env.Printer.Status(ui.LevelOK, name, "cloned")
	}
	return renderAfterChange(env, ws.Root)
}
