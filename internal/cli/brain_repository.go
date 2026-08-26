package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
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
				if !strictlyBelow(ws.Root, root) {
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
			env.Printer.Hint("\nNext: vat brain new decision --title \"...\"")
			return nil
		},
	}
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
		Usage:   "vat brain check",
		Long: `Fail closed on everything that would make the knowledge layer untrustworthy.

Every finding is reported at once rather than one per run, because these rules
are run in a loop while cleaning up a repository.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain check")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, store, err := openBrain(env)
			if err != nil {
				return err
			}
			findings := brain.Check(store, brainPolicy(ws), env.Now)
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
