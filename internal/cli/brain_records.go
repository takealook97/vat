package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

// Creating a record and moving it through the lifecycle. Everything here
// writes, which is why each one refuses rather than guesses: a claim with
// no provenance cannot be promoted, and a record cannot supersede itself.

func brainNewCommand() *Command {
	return &Command{
		Name:    "new",
		Summary: "Create an atomic record",
		Usage:   `vat brain new <goal|gap|decision|memory> --title "..." [--claim <kind>] [--owner <repo>] [--axis <a>] [--refs <ids>] [--id <id>]`,
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
	} else if err := brain.ValidateID(input.ID); err != nil {
		return usageErrorf("%v", err)
	}
	if *claim != "" {
		kind := brain.ClaimKind(*claim)
		if !kind.Valid() {
			return usageErrorf("unknown claim kind %q (want %s)", *claim, brain.ClaimKinds())
		}
		input.ClaimKind = kind
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

// currentRevisionOf reports where the owning repository stands right now, or
// "" when vat cannot tell. Not being able to see the source is a reason to ask
// the reviewer, never a reason to assume the evidence held.
func currentRevisionOf(ctx context.Context, ws *workspace.Workspace, owner string) string {
	if strings.TrimSpace(owner) == "" {
		return ""
	}
	repo, ok := ws.Manifest.Find(owner)
	if !ok {
		return ""
	}
	dir := ws.RepoPath(repo)
	if !fsx.IsDir(dir) {
		return ""
	}
	revision, err := headRevision(ctx, dir)
	if err != nil {
		return ""
	}
	return revision
}

func brainPromoteCommand() *Command {
	return &Command{
		Name:    "promote",
		Summary: "Mark a reviewed record as citable",
		Usage:   "vat brain promote <id> [--reviewer <name>] [--reverified]",
		Long: `Move a record to active after a human has checked it.

A current-state claim with no owner and no source revision cannot be promoted at
all. That refusal is what makes the promotion gate real rather than an honour
system: analysis does not become organisational truth because it was useful.

vat reads the owning repository to see whether the evidence is still the
revision the claim was read from. When it is, the observation date moves freely.
When it has moved — or vat cannot see the repository — the date only moves if
you pass --reverified, which is you stating that you re-read the source
yourself. Otherwise one keystroke would re-date a year-old claim as verified
today.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("brain promote")
			reviewer := set.String("reviewer", "", "who reviewed it")
			reverified := set.Bool("reverified", false, "you re-read the source yourself")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one record identifier")
			}
			ws, store, err := openBrain(env)
			if err != nil {
				return err
			}
			record, ok := store.ByID()[set.Arg(0)]
			if !ok {
				return usageErrorf("no record with id %q", set.Arg(0))
			}
			request := brain.PromoteRequest{
				Reviewer: *reviewer, Now: env.Now, Reverified: *reverified,
				RequireReviewer: ws.Manifest.Policy.Gates.BrainPromote == manifest.GateManual,
				SourceRevision:  currentRevisionOf(ctx, ws, record.OwnedBy),
			}
			if err := brain.Promote(store.Root, record, request); err != nil {
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
			ws, store, err := openBrain(env)
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
			// Superseding a record with itself wrote a chain that points
			// nowhere, and `brain check` then failed permanently with no vat
			// command able to clear it.
			if previous.ID == replacement.ID {
				return usageErrorf("%s cannot supersede itself", previous.ID)
			}
			gated := ws.Manifest.Policy.Brain.RequirePromotionGate
			if err := brain.Supersede(store.Root, previous, replacement,
				brain.SupersedeOptions{PromotionGated: gated}); err != nil {
				return err
			}
			env.Printer.Status(ui.LevelOK, previous.ID, "superseded by "+replacement.ID)
			if gated {
				env.Printer.Status(ui.LevelOK, replacement.ID, "provisional, supersedes "+previous.ID)
				env.Printer.Hint("The replacement is not citable until: vat brain promote %s", replacement.ID)
			} else {
				env.Printer.Status(ui.LevelOK, replacement.ID, "active, supersedes "+previous.ID)
			}
			env.Printer.Hint("Run `vat brain build` to refresh the index.")
			return nil
		},
	}
}
