package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fsx"
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
			reportUnmanagedProjections(env, "", result.Skipped)
			if len(result.Changed) == 0 {
				if len(result.Skipped) == 0 {
					env.Printer.Status(ui.LevelOK, "build", "already current")
				}
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
			// The same failure `vat lint --only` had: a selector matching no
			// rule reported a clean run. The match is a substring so a family
			// can be selected; matching nothing at all cannot be intentional.
			selectors := splitList(*only)
			if unmatched := unmatchedSelectors(selectors, brain.RuleNames()); len(unmatched) > 0 {
				return usageErrorf("no rule matches %s.\n  List them: vat brain check --list",
					strings.Join(quoteAll(unmatched), ", "))
			}
			policy.Only = selectors
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
				reportBrainFixes(env, findings)
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
		Usage:   "vat brain adopt <repository-name> [--plan]",
		Long: `Declare which governed repository holds the knowledge layer.

Nothing is moved or rewritten. vat reads what is there and reports which records
do not yet meet the schema, so an existing repository can be brought under the
rules gradually rather than converted in one pass.

--plan writes nothing at all — not the marker, not the projections, not the
manifest. It groups what adoption would find: how many records cannot be read,
how many carry a lifecycle status this schema does not have, how many relations
are one-sided, and which groups are mechanical. It proposes no mapping, because
a knowledge repository is the one thing in a workspace whose content no tool
should reinterpret. It makes the work countable, which is what deciding how to
migrate needs and what a list of two hundred findings does not give.`,
		Examples: []string{
			"vat brain adopt cortex --plan          # what adoption would find",
			"vat brain adopt cortex --plan --json   # the same, for a script",
			"vat brain adopt cortex",
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain adopt")
			plan := set.Bool("plan", false, "group what adoption would find, and write nothing")
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
			if *plan {
				return reportAdoptionPlan(env, ws, repo)
			}
			repo.Role = manifest.RoleBrain
			next := manifest.WithRepo(ws.Manifest, repo)
			next.Policy.Brain.Repo = repo.Name
			if err := commitManifest(env, ws, next); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, name, "adopted as the brain repository")

			// Declaring a repository the brain and leaving it without the marker
			// had this command report success and lint say "run vat brain init"
			// about the same directory in the same breath. Only the marker is
			// written: the command promises the repository is brought under the
			// rules gradually, not scaffolded in one pass.
			marked, err := brain.Mark(ws.RepoPath(repo))
			if err != nil {
				return err
			}
			if marked {
				env.Printer.Status(ui.LevelOK,
					filepath.Join(repo.Name, brain.MarkerFile), "written")
			}

			store, err := brain.Load(ws.RepoPath(repo))
			if err != nil {
				return err
			}
			// Marking the repository makes projections it does not have into
			// drift, and adoption that hands back a workspace failing its own
			// lint has not finished. Only generated files are written; the
			// records are read and left exactly as they are.
			built, err := brain.Build(store, env.Now)
			if err != nil {
				return err
			}
			for _, file := range built.Changed {
				env.Printer.Status(ui.LevelOK, filepath.Join(repo.Name, file), "generated")
			}
			reportUnmanagedProjections(env, repo.Name, built.Skipped)
			findings := brain.Check(store, brainPolicy(ws), env.Now)
			env.Printer.Status(ui.LevelInfo, "records", fmt.Sprintf("%d found", len(store.Records)))
			if len(findings) == 0 {
				env.Printer.Status(ui.LevelOK, "schema", "every record already conforms")
			} else {
				env.Printer.Status(ui.LevelWarn, "schema",
					fmt.Sprintf("%s attention; run `vat brain check` for the list",
						pluralise(brain.Errors(findings), "record needs", "records need")))
				env.Printer.Hint("      → Nothing was rewritten. Bring them up one at a time.")
			}
			return renderAfterChange(env, ws.Root)
		},
	}
}

// reportBrainFixes names the commands that clear the findings nobody has to
// judge.
//
// The rules that report a stale claim and a terminal record exist because those
// states accumulate unseen — the workspace measured for the second held
// forty-nine terminal records and one archived one. A remedy named only inside
// each finding's own line accumulates the same way: the reader has to notice
// for themselves that one command clears thirty of them. `vat lint` has said
// this since it gained `--fix`, and a reader of this report is owed no less.
//
// The commands are listed rather than assumed, because brain has no single
// `--fix`: a stale claim is swept and a terminal record is archived.
func reportBrainFixes(env *Env, findings []brain.Finding) {
	commands := brain.Fixes(findings)
	if len(commands) == 0 {
		return
	}
	count := 0
	for _, finding := range findings {
		if finding.Fixable {
			count++
		}
	}
	env.Printer.Hint("%s can be repaired without judgement: run %s.",
		pluralise(count, "finding", "findings"), strings.Join(commands, ", then "))
}

// reportUnmanagedProjections says which projections were left exactly as they
// were found.
//
// This is the first thing somebody adopting an existing knowledge repository
// needs to know, and the reason it is a status line rather than a failure: the
// files are intact, the rest of the build ran, and the choice of what to do
// with them belongs to whoever wrote them. `vat lint` reports the same state as
// an error, which is where CI reads it.
func reportUnmanagedProjections(env *Env, prefix string, skipped []string) {
	for _, file := range skipped {
		subject := file
		if prefix != "" {
			subject = filepath.Join(prefix, file)
		}
		env.Printer.Status(ui.LevelWarn, subject, "left alone; vat did not write it")
	}
	if len(skipped) > 0 {
		env.Printer.Hint("      → Move or delete it to let vat own the name, then `vat brain build`.")
	}
}

// reportAdoptionPlan groups what adopting a repository would find, and writes
// nothing — including the marker and the manifest, which the applying path
// writes before it reads anything.
func reportAdoptionPlan(env *Env, ws *workspace.Workspace, repo manifest.Repo) error {
	root := ws.RepoPath(repo)
	if !fsx.IsDir(root) {
		return usageErrorf("%s is not cloned, so there is nothing to read", repo.Name)
	}
	store, err := brain.Load(root)
	if err != nil {
		return err
	}
	plan := brain.BuildPlan(store, brainPolicy(ws), env.Now)
	if env.JSON {
		return emitJSON(env, plan)
	}

	env.Printer.Status(ui.LevelInfo, repo.Name, fmt.Sprintf("%d records read", plan.Records))
	if len(plan.Groups) == 0 {
		env.Printer.Status(ui.LevelOK, "schema", "every record already conforms")
		env.Printer.Hint("\nNothing was written. Run again without --plan to adopt.")
		return nil
	}
	rows := make([][]string, 0, len(plan.Groups))
	for _, group := range plan.Groups {
		effort := "decide"
		if group.Mechanical {
			effort = "mechanical"
		}
		rows = append(rows, []string{
			group.Kind, fmt.Sprintf("%d", group.Count), effort,
			strings.Join(group.Examples, ", "), group.Summary,
		})
	}
	env.Printer.Table([]string{"GROUP", "COUNT", "EFFORT", "EXAMPLES", "MEANING"}, rows)
	env.Printer.Hint("\n%d items across %d groups. `mechanical` means the shape can be converted\nwithout deciding what a record means; it is not permission to do it.",
		plan.Total(), len(plan.Groups))
	env.Printer.Hint("\nNothing was written. Adoption rewrites no record either — it marks the\nrepository and reports the same findings one at a time through `vat brain check`.")
	return nil
}
