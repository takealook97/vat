package cli

import (
	"context"
	"strings"

	"github.com/takealook97/vat/internal/brain"
)

// Listing records by status, kind, and extension value, read from the files as
// they are now rather than from a committed projection.

func brainListCommand() *Command {
	return &Command{
		Name:    "list",
		Summary: "List records by status, kind, and extension value",
		Usage:   "vat brain list [--status <s>] [--kind <k>] [--where key=value]... [--json]",
		Long: `List every record, archived ones included, filtered by what it is.

query answers a question and needs a word; graph.json is a committed projection
that can lag the files. list reads the records as they are now. Every filter
must hold: --status and --kind match exactly, and --where matches an extension
key to an exact string value, so a key no record carries lists nothing.
Citability is judged at read time, as query judges it.

A file that cannot be read as a record is listed under skipped, and the rest
are still listed; the command then exits 1. An empty listing exits 0.`,
		Examples: []string{
			"vat brain list --status active --kind decision",
			"vat brain list --where project=checkout --json",
		},
		Run: runBrainList,
	}
}

func runBrainList(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("brain list")
	status := set.String("status", "", "only records with this status")
	kind := set.String("kind", "", "only records of this kind: goal, gap, decision, or memory")
	where := &extensionFlag{}
	set.Var(where, "where", "only records whose extension key has this value, as key=value (repeatable)")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return usageErrorf("list takes no arguments; filter with --status, --kind, or --where")
	}
	if err := where.validate(); err != nil {
		return usageErrorf("%s", strings.ReplaceAll(err.Error(), "--set", "--where"))
	}
	opts := brain.ListOptions{Status: brain.Status(*status), Where: where.values}
	if *status != "" && !opts.Status.Valid() {
		return usageErrorf("unknown status %q", *status)
	}
	if *kind != "" {
		parsed, err := brain.ParseKind(*kind)
		if err != nil {
			return usageErrorf("%v", err)
		}
		opts.Kind = parsed
	}

	bc, err := resolveBrain(env)
	if err != nil {
		return err
	}
	result := brain.List(bc.Store, opts, bc.Policy, env.Now)
	if env.JSON {
		if err := emitJSON(env, result); err != nil {
			return err
		}
	} else {
		printBrainList(env, result)
	}
	// The listing is printed whole first: one unreadable file must not hide
	// every record that is fine. The exit code is what says it is incomplete.
	if len(result.Skipped) > 0 {
		for _, skipped := range result.Skipped {
			env.Printer.Warnf("%s skipped: %s", skipped.Path, skipped.Reason)
		}
		return findingsErrorf("")
	}
	return nil
}

func printBrainList(env *Env, result brain.ListResult) {
	if len(result.Records) == 0 {
		env.Printer.Println("No records match.")
		return
	}
	rows := make([][]string, 0, len(result.Records))
	for _, entry := range result.Records {
		citable := "no"
		if entry.Citable {
			citable = "yes"
			if entry.CitableUntil != "" {
				citable = "until " + entry.CitableUntil
			}
		}
		rows = append(rows, []string{
			entry.ID, string(entry.Kind), string(entry.Status), citable, truncate(entry.Title, 48), entry.Path,
		})
	}
	env.Printer.Table([]string{"ID", "KIND", "STATUS", "CITABLE", "TITLE", "PATH"}, rows)
	env.Printer.Hint("\n%s.", pluralise(len(result.Records), "record", "records"))
}
