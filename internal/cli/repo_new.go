package cli

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// Creating a repository that does not exist yet, scaffolding it, and
// enrolling it in one step.

func repoNewCommand() *Command {
	return &Command{
		Name:    "new",
		Summary: "Create a brand-new repository, scaffold it, and enrol it",
		Usage:   "vat repo new <name> [--role <r>] [--group <g>] [--branch <b>] [--checks <cmds>] [--access <a>] [--description <text>] [--required=false] [--private] [--remote <url>] [--no-remote]",
		Long: `Create a repository that does not exist yet.

It is initialised locally with a starter harness, a README, and a .gitignore,
committed, and enrolled in the manifest. When the GitHub CLI is available and
--no-remote is not given, the remote is created and the first commit pushed.

A repository created this way arrives with a contract already in it, which is
the difference between a workspace an agent can work in and one it has to guess
at.`,
		Examples: []string{
			"vat repo new payments --group backend --private",
			"vat repo new brain --role brain --private",
			"vat repo new scratch --no-remote",
		},
		Run: runRepoNew,
	}
}

func runRepoNew(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("repo new")
	private := set.Bool("private", false, "create the remote as private")
	remote := set.String("remote", "", "explicit remote URL (default: workspace.remote_template)")
	noRemote := set.Bool("no-remote", false, "do not create or attach any remote")
	fields := bindRepoFlags(set)
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
	if _, exists := ws.Manifest.Find(name); exists {
		return usageErrorf("%s is already in %s", name, manifest.FileName)
	}
	// Asked before the path is built, not after the manifest is saved. The name
	// becomes a directory and a remote URL, so `vat repo new ../escaped` used to
	// initialise a repository outside the workspace, scaffold it, commit it, and
	// only then fail validation -- leaving everything it had written behind.
	if err := manifest.ValidateRepoName(name); err != nil {
		return usageErrorf("%v", err)
	}
	dir := filepath.Join(ws.Root, name)
	// Defence in depth: the name check above already rejects a separator, and
	// this refuses anything that still resolves outside the workspace.
	if !workspace.Contains(ws.Root, dir) {
		return usageErrorf("%s would sit outside the workspace", name)
	}
	if fsx.Exists(dir) {
		return usageErrorf("%s already exists; use `vat repo adopt %s` instead", ws.Rel(dir), name)
	}

	// Everything is validated before the first directory is created. Creating
	// first meant a typo in --role left an initialised git repository behind
	// that was in neither the manifest nor .gitignore, and that the retry then
	// refused to overwrite.
	if _, err := fields.apply(manifest.Repo{Name: name, Origin: "placeholder"}); err != nil {
		return err
	}

	originURL := *remote
	if originURL == "" && !*noRemote {
		originURL = expandRemoteTemplate(ws.Manifest.Workspace.RemoteTemplate, name)
	}
	// Checked here, before anything is created. The URL is about to be written
	// into .git/config and pushed to, and the refusal must never quote it back.
	if manifest.HasEmbeddedCredential(originURL) {
		return usageErrorf(
			"the remote embeds a credential; %s is committed, so it records identity and never access.\n"+
				"  Keep the token in your git credential helper and pass the plain URL.", manifest.FileName)
	}

	branch := *fields.branch
	if branch == "" {
		branch = ws.Manifest.Workspace.DefaultBranch
	}
	if err := gitx.Init(ctx, dir, branch); err != nil {
		return err
	}
	role := manifest.Role(*fields.role)
	if err := scaffoldNewRepo(dir, name, role, ws.Manifest.Workspace.Name); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, name, "initialised with a starter harness")

	if _, err := gitx.Run(ctx, dir, "add", "."); err != nil {
		return err
	}
	if _, err := gitx.Run(ctx, dir, "commit", "-m", "chore: initialise repository"); err != nil {
		return fmt.Errorf("initial commit failed: %w", err)
	}

	repo, err := fields.apply(manifest.Repo{Name: name, Origin: originURL})
	if err != nil {
		return err
	}
	if !*noRemote {
		created, err := createRemote(ctx, dir, name, originURL, *private)
		if err != nil {
			env.Printer.Status(ui.LevelWarn, name, "remote not created: "+err.Error())
			env.Printer.Hint("      → create it yourself, then: git -C %s remote add origin <url>", name)
		} else if created != "" {
			repo.Origin = created
			env.Printer.Status(ui.LevelOK, name, "pushed to "+gitx.Redact(created))
		}
	}
	if repo.Origin == "" {
		// The manifest requires an origin, and a placeholder is more honest
		// than an invented URL: lint will report it until it is set.
		repo.Origin = "local:" + name
		env.Printer.Status(ui.LevelWarn, name, "no remote; origin recorded as a local placeholder")
	}

	next := manifest.WithRepo(ws.Manifest, repo)
	if err := commitManifest(env, ws, next); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, name, "registered as "+string(repo.Role))
	return renderAfterChange(env, ws.Root)
}

func expandRemoteTemplate(template, name string) string {
	if template == "" {
		return ""
	}
	return strings.ReplaceAll(template, "{name}", name)
}

// createRemote uses the GitHub CLI when it is available. vat shells out rather
// than embedding an API client so the user's existing authentication, host
// configuration, and enterprise settings apply unchanged.

func createRemote(ctx context.Context, dir, name, originURL string, private bool) (string, error) {
	if originURL != "" {
		if err := gitx.AddRemote(ctx, dir, "origin", originURL); err != nil {
			return "", err
		}
		if _, err := gitx.Run(ctx, dir, "push", "-u", "origin", "HEAD"); err != nil {
			return "", fmt.Errorf("push failed; does %s exist? %w", gitx.Redact(originURL), err)
		}
		return originURL, nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("no --remote given and the GitHub CLI is not installed")
	}
	visibility := "--public"
	if private {
		visibility = "--private"
	}
	cmd := exec.CommandContext(ctx, "gh", "repo", "create", name, visibility, "--source", ".", "--push")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("gh repo create: %s", strings.TrimSpace(string(output)))
	}
	return gitx.RemoteURL(ctx, dir, "origin")
}

func scaffoldNewRepo(dir, name string, role manifest.Role, workspaceName string) error {
	files := map[string]string{
		"README.md": fmt.Sprintf("# %s\n\nPart of the `%s` workspace.\n\n## What this repository owns\n\n_Describe it in one paragraph._\n\n## What it does not own\n\n_List the things that belong elsewhere._\n",
			name, workspaceName),
		".gitignore": ".DS_Store\n",
	}
	if role == manifest.RoleCredential {
		// A credential repository must refuse plaintext by default, not rely on
		// everyone remembering. The ignore rules are the first line of that.
		files[".gitignore"] = ".DS_Store\n\n# Never commit plaintext. Only encrypted files belong here.\n*.env\n.env\n.env.*\n!*.sops.*\n!*.enc.*\nid_rsa\nid_ed25519\n*.pem\n*.key\n"
	}
	for path, content := range files {
		if err := fsx.WriteFileAtomic(filepath.Join(dir, path), []byte(content), fsx.DefaultFileMode); err != nil {
			return err
		}
	}
	return nil
}
