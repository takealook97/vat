package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/evidence"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
)

func evidenceCommand() *Command {
	return &Command{
		Name:    "evidence",
		Summary: "Write the contract a worker is given before it starts",
		Usage:   "vat evidence <new|show|list|check>",
		Long: `Define what a delegated task is, before anyone starts it.

Delegation without a written contract produces work that is plausible and
wrong: the worker infers the goal, invents an acceptance criterion, and reports
success against its own invention. An evidence packet names the objective, the
repositories it may write to, what is explicitly out of scope, the observable
outcome that settles it, and the checks that prove it, so that "done" is
checkable by someone other than the worker.

Release authority is false unless a human sets it. Being trusted to decide
something is not the same as being able to act on it.`,
		Subcommands: []*Command{
			evidenceNewCommand(),
			evidenceShowCommand(),
			evidenceListCommand(),
			evidenceCheckCommand(),
		},
	}
}

func evidenceNewCommand() *Command {
	return &Command{
		Name:    "new",
		Summary: "Create an evidence packet",
		Usage:   `vat evidence new <id> "<objective>" --repos a,b --acceptance "..."`,
		Examples: []string{
			`vat evidence new EP-001 "Add idempotency keys" --repos payments --acceptance "a repeated request creates one charge"`,
		},
		Run: runEvidenceNew,
	}
}

func runEvidenceNew(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("evidence new")
	repos := set.String("repos", "", "repositories the worker may write to (required)")
	acceptance := set.String("acceptance", "", "observable outcomes that settle it, comma-separated (strongly recommended)")
	nonGoals := set.String("non-goal", "", "things explicitly out of scope")
	contracts := set.String("contract", "", "interfaces to honour or change deliberately")
	refs := set.String("refs", "", "brain record identifiers that authorised this")
	changesetID := set.String("changeset", "", "the changeset this packet belongs to")
	release := set.Bool("release-authority", false, "grant permission to deploy or write externally")
	markdown := set.Bool("markdown", false, "also print the briefing to paste into a session")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() < 2 {
		return usageErrorf("expected an identifier and an objective")
	}
	id := set.Arg(0)
	objective := strings.Join(set.Args()[1:], " ")

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	if err := fsx.EnsureDir(ws.EvidenceDir()); err != nil {
		return err
	}

	scope := splitList(*repos)
	if len(scope) == 0 {
		return usageErrorf("--repos is required: which repositories may the worker write to?")
	}
	// Overwriting silently replaced the acceptance criterion — the one thing
	// the packet exists to fix before work starts — with whatever the second
	// invocation happened to pass.
	if fsx.Exists(filepath.Join(ws.Root, evidence.Path(id))) {
		return usageErrorf("%s already exists at %s; choose another identifier",
			id, evidence.Path(id))
	}

	packet := evidence.New(id, objective, scope, env.Now)
	packet.Acceptance = splitList(*acceptance)
	packet.NonGoals = splitList(*nonGoals)
	packet.Contracts = splitList(*contracts)
	packet.EvidenceRefs = splitList(*refs)
	packet.Changeset = *changesetID
	packet.ReleaseAuthority = *release
	packet.RollbackPoints = map[string]string{}

	for _, name := range scope {
		repo, ok := ws.Manifest.Find(name)
		if !ok {
			return usageErrorf("%q is not in %s", name, manifest.FileName)
		}
		packet.CanonicalChecks = append(packet.CanonicalChecks, repo.Checks...)
		dir := ws.RepoPath(repo)
		if gitx.IsRepository(dir) {
			if revision, err := gitx.HeadRevision(ctx, dir); err == nil {
				packet.RollbackPoints[name] = revision
			}
		}
	}
	if err := evidence.Save(ws.Root, packet); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, id, evidence.Path(id))
	for _, problem := range evidence.Validate(packet) {
		env.Printer.Status(ui.LevelWarn, id, problem)
	}
	if *markdown {
		env.Printer.Println()
		env.Printer.Println(evidence.Markdown(packet))
	} else {
		env.Printer.Hint("\nPrint the briefing:  vat evidence show %s --markdown", id)
	}
	return nil
}

func evidenceShowCommand() *Command {
	return &Command{
		Name:    "show",
		Summary: "Print a packet, optionally as a briefing to paste into a session",
		Usage:   "vat evidence show <id> [--markdown]",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("evidence show")
			markdown := set.Bool("markdown", false, "render as a briefing")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if set.NArg() != 1 {
				return usageErrorf("expected exactly one packet identifier")
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			packet, err := evidence.Load(ws.Root, set.Arg(0))
			if err != nil {
				return usageErrorf("%v", err)
			}
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				return encoder.Encode(packet)
			}
			if *markdown {
				env.Printer.Println(evidence.Markdown(packet))
				return nil
			}
			env.Printer.Printf("%s  %s\n", packet.ID, packet.Objective)
			env.Printer.Printf("scope: %s\n", strings.Join(packet.Repositories, ", "))
			env.Printer.Printf("release authority: %v\n", packet.ReleaseAuthority)
			env.Printer.Heading("Acceptance")
			for _, item := range packet.Acceptance {
				env.Printer.Println("  - " + item)
			}
			env.Printer.Heading("Checks")
			for _, item := range packet.CanonicalChecks {
				env.Printer.Println("  " + item)
			}
			return nil
		},
	}
}

func evidenceListCommand() *Command {
	return &Command{
		Name:    "list",
		Summary: "List evidence packets",
		Usage:   "vat evidence list",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("evidence list")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			packets, err := evidence.List(ws.Root)
			if err != nil {
				return err
			}
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				return encoder.Encode(packets)
			}
			if len(packets) == 0 {
				env.Printer.Println("No evidence packets.")
				return nil
			}
			rows := make([][]string, 0, len(packets))
			for _, packet := range packets {
				authority := "none"
				if packet.ReleaseAuthority {
					authority = "release"
				}
				rows = append(rows, []string{
					packet.ID, packet.CreatedAt, strings.Join(packet.Repositories, ","),
					authority, truncate(packet.Objective, 44),
				})
			}
			env.Printer.Table([]string{"ID", "CREATED", "SCOPE", "AUTHORITY", "OBJECTIVE"}, rows)
			return nil
		},
	}
}

func evidenceCheckCommand() *Command {
	return &Command{
		Name:    "check",
		Summary: "Report what a packet is missing to be usable",
		Usage:   "vat evidence check [<id>]",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("evidence check")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			packets, err := evidence.List(ws.Root)
			if err != nil {
				return err
			}
			type packetReport struct {
				ID       string   `json:"id"`
				Complete bool     `json:"complete"`
				Problems []string `json:"problems"`
			}
			reports := []packetReport{}
			problems := 0
			for _, packet := range packets {
				if set.NArg() == 1 && packet.ID != set.Arg(0) {
					continue
				}
				found := evidence.Validate(packet)
				if found == nil {
					found = []string{}
				}
				problems += len(found)
				reports = append(reports, packetReport{
					ID: packet.ID, Complete: len(found) == 0, Problems: found,
				})
			}
			if env.JSON {
				encoder := json.NewEncoder(env.Printer.Out())
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(reports); err != nil {
					return err
				}
			} else {
				for _, report := range reports {
					if report.Complete {
						env.Printer.Status(ui.LevelOK, report.ID, "complete")
						continue
					}
					for _, problem := range report.Problems {
						env.Printer.Status(ui.LevelWarn, report.ID, problem)
					}
				}
			}
			// An incomplete packet is a warning, like every other warning-only
			// finding in vat: it exits 0 and says what is missing.
			return nil
		},
	}
}
