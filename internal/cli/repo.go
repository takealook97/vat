package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
)

// The repository command tree and the flags its subcommands share. Each
// subcommand lives beside the operation it performs, because that is what a
// reader is looking for when they open one of these files.

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
		Usage:   "vat repo list [--only <names>] [--group <g>] [--role <r>] [--archived] [--format <tsv>]",
		Long: `List the repositories the manifest governs.

--format tsv writes name, role, group, branch, state, and origin separated by
tabs, with no header and no alignment. It exists because the aligned table is
for people and changes shape with its content, and JSON needs a parser: a shell
script replacing a hand-maintained roster file should not have to reach for one
to read the roster that replaced it.`,
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("repo list")
			selection := bindSelector(set, true)
			format := set.String("format", "", "`tsv` for tab-separated rows with no header")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			if *format != "" && *format != "tsv" {
				return usageErrorf("unknown --format %q; the formats are tsv, or --json", *format)
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			repos, err := selection.resolve(ws, set)
			if err != nil {
				return err
			}
			if env.JSON {
				// An empty slice rather than nil, so a consumer can iterate the
				// result without a null check.
				if repos == nil {
					repos = []manifest.Repo{}
				}
				return emitJSON(env, repos)
			}
			// A header with nothing under it and a silent success look the same,
			// which is what the workspace somebody adopts the harness alone into
			// was answered with.
			if len(repos) == 0 {
				env.Printer.Println("No repositories are enrolled.")
				env.Printer.Hint("Enrol one with `vat repo add <name> --origin <url>`.")
				return nil
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
					repo.Branch(ws.Manifest.Workspace.DefaultBranch), state, gitx.Redact(repo.Origin),
				})
			}
			if *format == "tsv" {
				// Written to the stream directly rather than through Println,
				// which tames control characters because it also prints record
				// excerpts. Taming here would eat the separator and hand a
				// script six fields it cannot tell apart. Every field is a
				// manifest value the schema already constrains, and a name may
				// hold no tab or newline.
				for _, row := range rows {
					if _, err := fmt.Fprintln(env.Printer.Out(), strings.Join(row, "\t")); err != nil {
						return err
					}
				}
				return nil
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
		role:        set.String("role", string(manifest.RoleProduct), "one of: "+manifest.RoleNames()),
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
		return out, usageErrorf("unknown role %q (valid: %s)", *f.role, manifest.RoleNames())
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
