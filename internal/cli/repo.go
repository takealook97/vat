package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

func repoCommand() *Command {
	return &Command{
		Name:    "repo",
		Summary: "Add, create, adopt, archive, rename, or remove a governed repository",
		Usage:   "vat repo <list|add|new|adopt|remove|archive|unarchive|rename>",
		Long: `Manage which repositories the workspace governs.

Every subcommand here changes three things together — the manifest, the
workspace .gitignore, and the generated harness — because changing one without
the others is the failure mode. A repository added to the manifest but missing
from .gitignore gets absorbed into the workspace's own history on the next
commit; one added without a harness gives an agent no contract to read.`,
		Subcommands: []*Command{
			repoListCommand(),
			repoAddCommand(),
			repoNewCommand(),
			repoAdoptCommand(),
			repoRemoveCommand(),
			repoArchiveCommand(),
			repoUnarchiveCommand(),
			repoRenameCommand(),
		},
	}
}

func repoListCommand() *Command {
	return &Command{
		Name:    "list",
		Summary: "List every governed repository",
		Usage:   "vat repo list [--group <g>] [--role <r>] [--archived]",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("repo list")
			group := set.String("group", "", "only these groups")
			role := set.String("role", "", "only these roles")
			archived := set.Bool("archived", false, "include archived repositories")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			repos, err := ws.Select(manifest.Selector{
				Groups: splitList(*group), Roles: splitList(*role), IncludeArchive: *archived,
			})
			if err != nil {
				return usageErrorf("%v", err)
			}
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				return encoder.Encode(repos)
			}
			rows := make([][]string, 0, len(repos))
			for _, repo := range repos {
				state := "cloned"
				if !ws.Exists(repo) {
					state = "missing"
				}
				if repo.Archived {
					state = "archived"
				}
				rows = append(rows, []string{
					repo.Name, string(repo.Role), repo.Group,
					repo.Branch(ws.Manifest.Workspace.DefaultBranch), state, repo.Origin,
				})
			}
			env.Printer.Table([]string{"NAME", "ROLE", "GROUP", "BRANCH", "STATE", "ORIGIN"}, rows)
			return nil
		},
	}
}

// repoFlags are the manifest fields every mutating repo subcommand accepts.
type repoFlags struct {
	role        *string
	group       *string
	branch      *string
	checks      *string
	access      *string
	description *string
	required    *bool
}

func bindRepoFlags(set *flag.FlagSet) repoFlags {
	return repoFlags{
		role:        set.String("role", string(manifest.RoleProduct), "product|brain|credential|docs|agent|infra"),
		group:       set.String("group", "", "group name, for selecting a slice of the workspace"),
		branch:      set.String("branch", "", "default branch (default: the workspace default)"),
		checks:      set.String("checks", "", "canonical check commands, comma-separated"),
		access:      set.String("access", "", "public|private"),
		description: set.String("description", "", "one line describing what this repository owns"),
		required:    set.Bool("required", true, "treat a missing clone as a failure"),
	}
}

func (f repoFlags) apply(repo manifest.Repo) (manifest.Repo, error) {
	out := repo
	role := manifest.Role(*f.role)
	if !role.Valid() {
		return out, usageErrorf("unknown role %q", *f.role)
	}
	out.Role = role
	out.Group = *f.group
	out.DefaultBranch = *f.branch
	out.Checks = splitList(*f.checks)
	out.Access = *f.access
	out.Description = *f.description
	out.Required = *f.required
	return out, nil
}

func repoAddCommand() *Command {
	return &Command{
		Name:    "add",
		Summary: "Enrol an existing remote repository and clone it",
		Usage:   "vat repo add <name> --origin <url> [--role <r>] [--group <g>] [--no-clone]",
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

func repoNewCommand() *Command {
	return &Command{
		Name:    "new",
		Summary: "Create a brand-new repository, scaffold it, and enrol it",
		Usage:   "vat repo new <name> [--role <r>] [--private] [--remote <url>] [--no-remote]",
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
	dir := filepath.Join(ws.Root, name)
	if fsx.Exists(dir) {
		return usageErrorf("%s already exists; use `vat repo adopt %s` instead", ws.Rel(dir), name)
	}

	originURL := *remote
	if originURL == "" && !*noRemote {
		originURL = expandRemoteTemplate(ws.Manifest.Workspace.RemoteTemplate, name)
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
			env.Printer.Status(ui.LevelOK, name, "pushed to "+created)
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
			return "", fmt.Errorf("push failed; does %s exist? %w", originURL, err)
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

func repoAdoptCommand() *Command {
	return &Command{
		Name:    "adopt",
		Summary: "Enrol a repository that is already on disk",
		Usage:   "vat repo adopt <directory> [--role <r>] [--group <g>]",
		Long: `Bring a repository already sitting in the workspace under the manifest.

Its origin and current branch are read and recorded as they are. Nothing is
moved, re-pointed, or checked out.`,
		Examples: []string{
			"vat repo adopt ./payments",
			"vat repo adopt legacy-api --role product --group backend",
		},
		Run: runRepoAdopt,
	}
}

func runRepoAdopt(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("repo adopt")
	fields := bindRepoFlags(set)
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return usageErrorf("expected exactly one directory")
	}
	target := strings.TrimSuffix(filepath.Clean(set.Arg(0)), string(filepath.Separator))

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	name := filepath.Base(target)
	discovered, ok := describeRepo(ctx, ws.Root, name)
	if !ok {
		return usageErrorf("%s is not a git repository inside the workspace", name)
	}
	if _, exists := ws.Manifest.Find(name); exists {
		return usageErrorf("%s is already in %s", name, manifest.FileName)
	}

	// Explicit flags win; anything not given keeps what was discovered on disk.
	repo := discovered
	if isFlagSet(set, "role") {
		role := manifest.Role(*fields.role)
		if !role.Valid() {
			return usageErrorf("unknown role %q", *fields.role)
		}
		repo.Role = role
	}
	if isFlagSet(set, "group") {
		repo.Group = *fields.group
	}
	if isFlagSet(set, "checks") {
		repo.Checks = splitList(*fields.checks)
	}
	if isFlagSet(set, "description") {
		repo.Description = *fields.description
	}

	next := manifest.WithRepo(ws.Manifest, repo)
	if err := commitManifest(env, ws, next); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, name, fmt.Sprintf("adopted as %s · %s", repo.Role, repo.Origin))
	return renderAfterChange(env, ws.Root)
}

// describeRepo reads a directory's git identity into a manifest entry.
func describeRepo(ctx context.Context, root, name string) (manifest.Repo, bool) {
	dir := filepath.Join(root, name)
	if !gitx.IsRepository(dir) {
		return manifest.Repo{}, false
	}
	origin, err := gitx.RemoteURL(ctx, dir, "origin")
	if err != nil {
		origin = "local:" + name
	}
	branch, _ := gitx.CurrentBranch(ctx, dir)
	repo := manifest.Repo{
		Name:     name,
		Origin:   origin,
		Role:     inferRole(name),
		Required: true,
	}
	// Recording the branch a repository is actually on is what stops sync from
	// skipping it forever with a "not on main" note nobody reads.
	if branch != "" && branch != "main" {
		repo.DefaultBranch = branch
	}
	if brain.IsBrain(dir) {
		repo.Role = manifest.RoleBrain
	}
	return repo, true
}

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

	next, removed := manifest.WithoutRepo(ws.Manifest, name)
	if !removed {
		return usageErrorf("%s is not in %s", name, manifest.FileName)
	}
	if err := commitManifest(env, ws, next); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, name, "removed from the manifest")

	if *deleteFiles && fsx.Exists(dir) {
		// Deleting a working tree is irreversible, so --yes deliberately does
		// not cover it. This prompt is the last thing between a typo and lost
		// work.
		if !confirm(env, fmt.Sprintf("Permanently delete %s and everything in it?", ws.Rel(dir))) {
			env.Printer.Status(ui.LevelSkip, name, "directory left on disk")
			return renderAfterChange(env, ws.Root)
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("delete %s: %w", dir, err)
		}
		env.Printer.Status(ui.LevelOK, name, "directory deleted")
	} else if fsx.Exists(dir) {
		env.Printer.Hint("      → %s is still on disk; delete it yourself when you are sure.", ws.Rel(dir))
	}
	return renderAfterChange(env, ws.Root)
}

// unsavedWork reports everything in a repository that exists nowhere else.
func unsavedWork(ctx context.Context, dir string) []string {
	var risks []string
	if dirty, err := gitx.IsDirty(ctx, dir); err == nil && dirty {
		risks = append(risks, "uncommitted changes in the working tree")
	}
	if unpushed, err := gitx.UnpushedCommits(ctx, dir); err == nil && unpushed > 0 {
		risks = append(risks, fmt.Sprintf("%d commit(s) not on any remote", unpushed))
	}
	// Stashes are invisible to `git status`, which is exactly why they are the
	// work most often destroyed by a cleanup.
	if stashes, err := gitx.StashCount(ctx, dir); err == nil && stashes > 0 {
		risks = append(risks, fmt.Sprintf("%d stash entry/entries", stashes))
	}
	return risks
}

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
		newDir := filepath.Join(ws.Root, newName)
		if fsx.Exists(oldDir) {
			if fsx.Exists(newDir) {
				return usageErrorf("%s already exists on disk", ws.Rel(newDir))
			}
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("rename %s: %w", ws.Rel(oldDir), err)
			}
		}
	}

	next, _ := manifest.WithoutRepo(ws.Manifest, oldName)
	if err := commitManifest(env, ws, manifest.WithRepo(next, updated)); err != nil {
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
