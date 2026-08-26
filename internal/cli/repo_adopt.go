package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// Taking a repository already sitting in the workspace under the manifest,
// reading what is on disk rather than changing it.

func repoAdoptCommand() *Command {
	return &Command{
		Name:    "adopt",
		Summary: "Enrol a repository that is already on disk",
		Usage:   "vat repo adopt <directory> [--role <r>] [--group <g>] [--branch <b>] [--checks <cmds>] [--access <a>] [--description <text>] [--required=false]",
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
	// strictlyBelow resolves symlinks, which textual containment does not: a
	// link inside the workspace pointing at a repository outside it passed
	// every check and then had contracts written into it, outside the one
	// directory vat is allowed to write to.
	if !workspace.Contains(ws.Root, filepath.Join(ws.Root, name)) {
		return usageErrorf("%s resolves outside the workspace; adopt the directory where it actually is", name)
	}
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
			return usageErrorf("unknown role %q (valid: %s)", *fields.role, manifest.RoleNames())
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
	// These were bound and then silently dropped, so `--required=false` still
	// wrote required: true.
	if isFlagSet(set, "branch") {
		repo.DefaultBranch = *fields.branch
	}
	if isFlagSet(set, "access") {
		repo.Access = *fields.access
	}
	if isFlagSet(set, "required") {
		repo.Required = *fields.required
	}

	next := manifest.WithRepo(ws.Manifest, repo)
	if err := commitManifest(env, ws, next); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, name, fmt.Sprintf("adopted as %s · %s", repo.Role, gitx.Redact(repo.Origin)))
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
	// The manifest is committed, so it records identity and never access. A
	// remote configured with a token still works after this: git reads the
	// credential from its helper, not from vat.yaml.
	origin = gitx.WithoutCredentials(origin)
	// A branch that cannot be read leaves default_branch unset, and an
	// undeclared default is what makes sync skip a repository forever with a
	// note nobody reads. `vat lint` reports that case as repo/default-branch-missing.
	branch, branchErr := gitx.CurrentBranch(ctx, dir)
	repo := manifest.Repo{
		Name:     name,
		Origin:   origin,
		Role:     inferRole(name),
		Required: true,
	}
	// Recording the branch a repository is actually on is what stops sync from
	// skipping it forever with a "not on main" note nobody reads.
	if branchErr == nil && branch != "" && branch != "main" {
		repo.DefaultBranch = branch
	}
	if brain.IsBrain(dir) {
		repo.Role = manifest.RoleBrain
	}
	return repo, true
}
