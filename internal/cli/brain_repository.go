package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// Standing up a knowledge repository and keeping its generated projections
// honest, including adopting one that already exists without rewriting it.

func brainInitCommand() *Command {
	return &Command{
		Name:    "init",
		Summary: "Scaffold the brain layout in a repository",
		Usage:   "vat brain init [directory]",
		Long: `Create the directory structure and starter documents of a brain repository.

Existing files are never overwritten, so this is safe to run on a repository
that already holds records.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain init")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			root := ""
			switch {
			case set.NArg() == 1:
				root = filepath.Join(ws.Root, set.Arg(0))
				// The argument is a user-supplied path and this scaffolds a
				// directory of documents at it. Without this, `brain init
				// ../../outside` built a brain repository outside the
				// workspace, which is the boundary the whole tool rests on.
				if !workspace.Contains(ws.Root, root) {
					return usageErrorf("%s is outside the workspace", set.Arg(0))
				}
			default:
				resolved, ok := ws.BrainPath()
				if !ok {
					return usageErrorf("no brain repository in the manifest; pass a directory or create one with `vat repo new brain --role brain`")
				}
				root = resolved
			}
			created, err := brain.Init(root, env.Now)
			if err != nil {
				return err
			}
			for _, file := range created {
				env.Printer.Status(ui.LevelOK, file, "created")
			}
			if len(created) == 0 {
				env.Printer.Status(ui.LevelOK, "brain", "already initialised")
			}
			env.Printer.Hint("%s", brainNextStep(ws, root))
			return nil
		},
	}
}

// brainNextStep returns the hint to print once a brain layout exists at root.
//
// `brain init <directory>` scaffolds wherever it is pointed, but every other
// `vat brain` command resolves the brain through the manifest. Printing "Next:
// vat brain new" unconditionally sent the reader straight into "this workspace
// has no brain repository": the tool created a state its own commands could not
// reach, and named a command that could only fail.
func brainNextStep(ws *workspace.Workspace, root string) string {
	if brainRoot, ok := ws.BrainPath(); ok && sameDir(brainRoot, root) {
		return "\nNext: vat brain new decision --title \"...\""
	}
	rel := ws.Rel(root)
	if repo, ok := ws.Manifest.Find(rel); ok {
		return fmt.Sprintf(
			"\n%s is governed but is not the workspace's brain, so no vat brain\ncommand reaches it yet. Register it:\n  vat brain adopt %s",
			rel, repo.Name)
	}
	return fmt.Sprintf(
		"\n%s is not in %s, so no vat brain command reaches it yet. Enrol it:\n  vat repo adopt %s\n  vat brain adopt %s",
		rel, manifest.FileName, rel, rel)
}

// sameDir compares two paths that are already absolute and inside the
// workspace. Symlinks are resolved before the comparison because the workspace
// root itself may be reached through one, and a false mismatch here would send
// the reader to adopt a repository that is already the brain.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return resolvedA == resolvedB
}

func brainBuildCommand() *Command {
	return &Command{
		Name:    "build",
		Summary: "Regenerate the index and the relation graph from the records",
		Usage:   "vat brain build",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain build")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			_, store, err := openBrain(env)
			if err != nil {
				return err
			}
			result, err := brain.Build(store, env.Now)
			if err != nil {
				return err
			}
			if len(result.Changed) == 0 {
				env.Printer.Status(ui.LevelOK, "build", "already current")
				return nil
			}
			for _, file := range result.Changed {
				env.Printer.Status(ui.LevelOK, file, "regenerated")
			}
			return nil
		},
	}
}

func brainCheckCommand() *Command {
	return &Command{
		Name:    "check",
		Summary: "Validate identifiers, provenance, supersession, and links",
		Usage:   "vat brain check [--only <rule>] [--list]",
		Long: `Fail closed on everything that would make the knowledge layer untrustworthy.

Every finding is reported at once rather than one per run, because these rules
are run in a loop while cleaning up a repository. --only narrows that to one
class of problem while you work through it; --list names every rule.`,
		Examples: []string{
			"vat brain check",
			"vat brain check --only claim    # provenance rules alone",
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain check")
			only := set.String("only", "", "only rules whose name contains this")
			list := set.Bool("list", false, "list every rule and exit")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if *list {
				for _, rule := range brain.RuleNames() {
					env.Printer.Println(rule)
				}
				return nil
			}
			ws, store, err := openBrain(env)
			if err != nil {
				return err
			}
			policy := brainPolicy(ws)
			policy.Only = splitList(*only)
			findings := brain.Check(store, policy, env.Now)
			switch {
			case env.JSON:
				if err := emitJSON(env, findings); err != nil {
					return err
				}
			case len(findings) == 0:
				env.Printer.Status(ui.LevelOK, "brain",
					fmt.Sprintf("%s, nothing to report",
						pluralise(len(store.Records), "record", "records")))
			default:
				for _, finding := range findings {
					level := ui.LevelWarn
					if finding.Severity == brain.SeverityError {
						level = ui.LevelFail
					}
					subject := finding.Rule
					if finding.ID != "" {
						subject = finding.Rule + " · " + finding.ID
					}
					env.Printer.Status(level, subject, finding.Message)
					if finding.Path != "" {
						env.Printer.Hint("      → %s", finding.Path)
					}
				}
				env.Printer.Heading("Result")
				level := ui.LevelWarn
				if brain.Errors(findings) > 0 {
					level = ui.LevelFail
				}
				env.Printer.Status(level, "brain",
					fmt.Sprintf("%s, %s",
						pluralise(brain.Errors(findings), "error", "errors"),
						pluralise(len(findings)-brain.Errors(findings), "warning", "warnings")))
			}
			if brain.Errors(findings) > 0 {
				return findingsErrorf("")
			}
			return nil
		},
	}
}

func brainAdoptCommand() *Command {
	return &Command{
		Name:    "adopt",
		Summary: "Point the workspace at an existing knowledge repository",
		Usage:   "vat brain adopt <repository-name>",
		Long: `Declare which governed repository holds the knowledge layer.

Nothing is moved or rewritten. vat reads what is there and reports which records
do not yet meet the schema, so an existing repository can be brought under the
rules gradually rather than converted in one pass.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain adopt")
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
			repo, ok := ws.Manifest.Find(name)
			if !ok {
				return usageErrorf("%q is not in %s; add it with `vat repo add` or `vat repo adopt`",
					name, manifest.FileName)
			}
			repo.Role = manifest.RoleBrain
			next := manifest.WithRepo(ws.Manifest, repo)
			next.Policy.Brain.Repo = repo.Name
			if err := commitManifest(env, ws, next); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, name, "adopted as the brain repository")

			store, err := brain.Load(ws.RepoPath(repo))
			if err != nil {
				return err
			}
			findings := brain.Check(store, brainPolicy(ws), env.Now)
			env.Printer.Status(ui.LevelInfo, "records", fmt.Sprintf("%d found", len(store.Records)))
			if len(findings) == 0 {
				env.Printer.Status(ui.LevelOK, "schema", "every record already conforms")
			} else {
				env.Printer.Status(ui.LevelWarn, "schema",
					fmt.Sprintf("%d records need attention; run `vat brain check` for the list",
						brain.Errors(findings)))
				env.Printer.Hint("      → Nothing was rewritten. Bring them up one at a time.")
			}
			return renderAfterChange(env, ws.Root)
		},
	}
}
