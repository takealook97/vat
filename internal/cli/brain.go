package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

func brainCommand() *Command {
	return &Command{
		Name:    "brain",
		Summary: "The reviewed-knowledge layer: facts with provenance and an expiry",
		Usage:   "vat brain <init|new|build|check|query|review|sweep|promote|supersede|adopt>",
		Long: `Hold what the organisation believes is true, and know when it stopped
being verified.

Code says what a system does. It does not say what the organisation is trying to
achieve, what was already tried, or why a decision was made — and those are
exactly what is lost first, and most expensively.

The brain stores one fact per file. A statement about the present carries the
repository that owns it, the exact revision it was read from, and the date it
was observed. When nobody re-checks it within the policy window, it is demoted
to stale automatically: it is not deleted, and it is not still true, it is
unverified. That single mechanism is what keeps a knowledge repository from
becoming a confident liar over a few years.`,
		Subcommands: []*Command{
			brainInitCommand(),
			brainNewCommand(),
			brainBuildCommand(),
			brainCheckCommand(),
			brainQueryCommand(),
			brainReviewCommand(),
			brainSweepCommand(),
			brainPromoteCommand(),
			brainSupersedeCommand(),
			brainAdoptCommand(),
		},
	}
}

// openBrain resolves the brain repository the workspace has adopted.
func openBrain(env *Env) (*workspace.Workspace, *brain.Store, error) {
	ws, err := env.Workspace()
	if err != nil {
		return nil, nil, err
	}
	root, ok := ws.BrainPath()
	if !ok {
		return ws, nil, usageErrorf(
			"this workspace has no brain repository.\n" +
				"  Create one:  vat repo new brain --role brain\n" +
				"  Adopt one:   vat brain adopt <directory>")
	}
	if !fsx.IsDir(root) {
		return ws, nil, usageErrorf("the brain repository is not cloned; run `vat sync`")
	}
	store, err := brain.Load(root)
	if err != nil {
		return ws, nil, err
	}
	return ws, store, nil
}

func brainPolicy(ws *workspace.Workspace) brain.CheckPolicy {
	return brain.CheckPolicy{
		StaleAfterDays: ws.Manifest.Policy.Brain.StaleAfterDays,
		ReviewSLADays:  ws.Manifest.Policy.Brain.ReviewSLADays,
	}
}

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

func brainNewCommand() *Command {
	return &Command{
		Name:    "new",
		Summary: "Create an atomic record",
		Usage:   `vat brain new <goal|gap|decision|memory> --title "..." [--claim current-state --owner <repo>]`,
		Long: `Create one record holding one fact.

A record enters as provisional, never as truth. Promoting it is a separate,
deliberate step — otherwise anything an agent wrote becomes canonical simply by
having been written.

For a claim about the present, pass --claim current-state and name the
repository that owns the fact. vat records the current revision of that
repository as the evidence, so the claim can later be checked against what has
changed since.`,
		Examples: []string{
			`vat brain new decision --title "Orders own their own idempotency keys"`,
			`vat brain new gap --title "Retries can double-submit" --claim current-state --owner payments`,
		},
		Run: runBrainNew,
	}
}

func runBrainNew(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("brain new")
	title := set.String("title", "", "the record's heading (required)")
	claim := set.String("claim", "", "current-state|historical|intent")
	owner := set.String("owner", "", "repository that is canonical for this fact")
	axis := set.String("axis", "", "grouping axis, for goals")
	refs := set.String("refs", "", "identifiers of related records")
	id := set.String("id", "", "explicit identifier (default: the next free one)")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() != 1 {
		return usageErrorf("expected exactly one kind: goal, gap, decision, or memory")
	}
	kind, err := brain.ParseKind(set.Arg(0))
	if err != nil {
		return usageErrorf("%v", err)
	}
	if strings.TrimSpace(*title) == "" {
		return usageErrorf("--title is required")
	}

	ws, store, err := openBrain(env)
	if err != nil {
		return err
	}

	input := brain.NewRecordInput{
		Kind: kind, ID: *id, Title: *title, OwnedBy: *owner,
		Axis: *axis, Refs: splitList(*refs), Now: env.Now,
	}
	if input.ID == "" {
		input.ID = store.NextID(kind)
	}
	if *claim != "" {
		input.ClaimKind = brain.ClaimKind(*claim)
	}
	if input.ClaimKind == brain.ClaimCurrentState {
		if *owner == "" {
			return usageErrorf("a current-state claim needs --owner: which repository is canonical for this fact?")
		}
		reference, err := sourceReferenceFor(ctx, ws, *owner)
		if err != nil {
			return err
		}
		input.SourceRef = reference
	}

	path, err := brain.Create(store.Root, input)
	if err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, input.ID, path)
	if input.SourceRef != "" {
		env.Printer.Status(ui.LevelInfo, "evidence", input.SourceRef)
	}
	env.Printer.Hint("\nWrite the record, then: vat brain build && vat brain check")
	env.Printer.Hint("It stays provisional until: vat brain promote %s", input.ID)
	return nil
}

// sourceReferenceFor pins a claim to the owning repository's exact revision.
// A branch name would keep moving and silently change what the claim was
// evidence for.
func sourceReferenceFor(ctx context.Context, ws *workspace.Workspace, owner string) (string, error) {
	repo, ok := ws.Manifest.Find(owner)
	if !ok {
		return "", usageErrorf("%q is not a repository in %s", owner, manifest.FileName)
	}
	dir := ws.RepoPath(repo)
	if !fsx.IsDir(dir) {
		return "", usageErrorf("%s is not cloned, so its revision cannot be recorded", owner)
	}
	revision, err := headRevision(ctx, dir)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s@%s", owner, revision), nil
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
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(findings); err != nil {
					return err
				}
			} else if len(findings) == 0 {
				env.Printer.Status(ui.LevelOK, "brain",
					fmt.Sprintf("%d records, nothing to report", len(store.Records)))
			} else {
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
				env.Printer.Status(ui.LevelInfo, "brain",
					fmt.Sprintf("%d errors, %d warnings",
						brain.Errors(findings), len(findings)-brain.Errors(findings)))
			}
			if brain.Errors(findings) > 0 {
				return findingsErrorf("")
			}
			return nil
		},
	}
}

func brainQueryCommand() *Command {
	return &Command{
		Name:    "query",
		Summary: "Search the bounded surface for the records that matter",
		Usage:   "vat brain query <terms...> [--all] [--limit n]",
		Long: `Find the few records relevant to a question.

The default surface is deliberately narrow: the generated index, the root
projections, and the non-terminal records. Reading everything makes answers
worse, not better — superseded reasoning and current fact stop being
distinguishable. --all widens the search to history and archives, for when you
are auditing why something was decided rather than asking what is true now.`,
		Examples: []string{
			"vat brain query idempotency retries",
			"vat brain query pricing --all      # include superseded and archived material",
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain query")
			all := set.Bool("all", false, "include history, archives, and terminal records")
			limit := set.Int("limit", 15, "maximum results")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() == 0 {
				return usageErrorf("expected at least one search term")
			}
			_, store, err := openBrain(env)
			if err != nil {
				return err
			}
			hits := brain.Query(store, set.Args(), brain.QueryOptions{
				IncludeHistory: *all, IncludeTerminal: *all, Limit: *limit,
			})
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				return encoder.Encode(hits)
			}
			if len(hits) == 0 {
				env.Printer.Println("No matches on the current surface.")
				if !*all {
					env.Printer.Hint("Add --all to search history and superseded records.")
				}
				return nil
			}
			for _, hit := range hits {
				label := hit.Path
				if hit.ID != "" {
					label = fmt.Sprintf("%s  %s", hit.ID, hit.Title)
				}
				level := ui.LevelInfo
				if hit.Status == brain.StatusStale || hit.Status == brain.StatusQuarantined {
					level = ui.LevelWarn
				}
				env.Printer.Status(level, label, string(hit.Status))
				env.Printer.Hint("      %s", hit.Path)
				for _, line := range hit.Excerpt {
					env.Printer.Hint("      │ %s", truncate(line, 96))
				}
			}
			env.Printer.Hint("\n%d results. Open the records themselves; this is an index, not an answer.",
				len(hits))
			return nil
		},
	}
}

func brainReviewCommand() *Command {
	return &Command{
		Name:    "review",
		Summary: "The re-check queue, ordered by what it costs to leave unresolved",
		Usage:   "vat brain review [--overdue] [--limit n]",
		Long: `List everything awaiting human judgement.

Priority weights how many other records depend on a claim against how long it
has gone unverified. A stale claim nothing cites can wait; a stale claim the
roadmap rests on cannot. Without that ordering the queue is a flat list that
grows until it is ignored wholesale — which is the exact state this layer exists
to prevent.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain review")
			overdueOnly := set.Bool("overdue", false, "only items past the review window")
			limit := set.Int("limit", 20, "maximum items")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, store, err := openBrain(env)
			if err != nil {
				return err
			}
			items := brain.ReviewQueue(store, brainPolicy(ws), env.Now)
			if *overdueOnly {
				filtered := items[:0]
				for _, item := range items {
					if item.Overdue {
						filtered = append(filtered, item)
					}
				}
				items = filtered
			}
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				return encoder.Encode(items)
			}
			if len(items) == 0 {
				env.Printer.Status(ui.LevelOK, "review queue", "empty")
				return nil
			}
			shown := items
			if len(shown) > *limit {
				shown = shown[:*limit]
			}
			rows := make([][]string, 0, len(shown))
			for _, item := range shown {
				age := fmt.Sprintf("%d", item.AgeDays)
				if item.Overdue {
					age += " ⚠"
				}
				rows = append(rows, []string{
					item.ID, string(item.Status), age,
					fmt.Sprintf("%d", item.References), truncate(item.Title, 48), item.Why,
				})
			}
			env.Printer.Table([]string{"ID", "STATUS", "AGE", "CITED", "TITLE", "WHY"}, rows)
			overdue := 0
			for _, item := range items {
				if item.Overdue {
					overdue++
				}
			}
			env.Printer.Hint("\n%d awaiting review, %d past the %d-day window.",
				len(items), overdue, ws.Manifest.Policy.Brain.ReviewSLADays)
			env.Printer.Hint("Re-verify against the owning repository, then: vat brain promote <id>")
			return nil
		},
	}
}

func brainSweepCommand() *Command {
	return &Command{
		Name:    "sweep",
		Summary: "Demote claims whose evidence has aged out",
		Usage:   "vat brain sweep [--apply]",
		Long: `Move active current-state claims past the policy window to stale.

Without --apply nothing is written; the transitions are only listed.

Demotion is not deletion. The record and its reasoning survive intact — it
simply stops being citable until someone re-checks it. This is the mechanism
that keeps the layer honest across years rather than months.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain sweep")
			apply := set.Bool("apply", false, "write the transitions")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, store, err := openBrain(env)
			if err != nil {
				return err
			}
			transitions, err := brain.Sweep(store, brainPolicy(ws), env.Now, *apply)
			if err != nil {
				return err
			}
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				return encoder.Encode(transitions)
			}
			if len(transitions) == 0 {
				env.Printer.Status(ui.LevelOK, "sweep", "every claim is within its window")
				return nil
			}
			for _, transition := range transitions {
				level := ui.LevelWarn
				if transition.Applied {
					level = ui.LevelOK
				}
				env.Printer.Status(level, transition.ID,
					fmt.Sprintf("%s → %s (%s)", transition.From, transition.To, transition.Reason))
			}
			if !*apply {
				env.Printer.Hint("\nNothing written. Re-run with --apply to record these.")
			} else {
				env.Printer.Hint("\n%d claims demoted. Run `vat brain build` to refresh the index.",
					len(transitions))
			}
			return nil
		},
	}
}

func brainPromoteCommand() *Command {
	return &Command{
		Name:    "promote",
		Summary: "Mark a reviewed record as citable",
		Usage:   "vat brain promote <id> [--reviewer <name>]",
		Long: `Move a record to active after a human has checked it.

A current-state claim with no owner and no source revision cannot be promoted at
all. That refusal is what makes the promotion gate real rather than an honour
system: analysis does not become organisational truth because it was useful.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain promote")
			reviewer := set.String("reviewer", "", "who reviewed it")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one record identifier")
			}
			_, store, err := openBrain(env)
			if err != nil {
				return err
			}
			record, ok := store.ByID()[set.Arg(0)]
			if !ok {
				return usageErrorf("no record with id %q", set.Arg(0))
			}
			if err := brain.Promote(store.Root, record, *reviewer, env.Now); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, record.ID,
				fmt.Sprintf("active, observed %s", env.Now.Format("2006-01-02")))
			env.Printer.Hint("Run `vat brain build` to refresh the index.")
			return nil
		},
	}
}

func brainSupersedeCommand() *Command {
	return &Command{
		Name:    "supersede",
		Summary: "Replace a decision with a later one, linking both ends",
		Usage:   "vat brain supersede <old-id> <new-id>",
		Long: `Record that one decision replaces another.

The old record is never edited to say something new. Both files are updated so
the chain reads correctly from either end: a reader who finds the current
decision can see what it replaced, and a reader who finds the old one is sent
forward. Rewriting the original instead destroys the only account of why it once
made sense.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain supersede")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 2 {
				return usageErrorf("expected an old identifier and a new one")
			}
			_, store, err := openBrain(env)
			if err != nil {
				return err
			}
			index := store.ByID()
			previous, ok := index[set.Arg(0)]
			if !ok {
				return usageErrorf("no record with id %q", set.Arg(0))
			}
			replacement, ok := index[set.Arg(1)]
			if !ok {
				return usageErrorf("no record with id %q", set.Arg(1))
			}
			if err := brain.Supersede(store.Root, previous, replacement); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, previous.ID, "superseded by "+replacement.ID)
			env.Printer.Status(ui.LevelOK, replacement.ID, "active, supersedes "+previous.ID)
			env.Printer.Hint("Run `vat brain build` to refresh the index.")
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

func truncate(text string, width int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= width {
		return string(runes)
	}
	return string(runes[:width-1]) + "…"
}
