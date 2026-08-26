package cli

import (
	"encoding/json"
	"flag"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// The two things every command was repeating: choosing which repositories to
// act on, and emitting a payload as JSON. Both are extracted here and nothing
// more — a CLI framework of our own would cost more than the repetition it
// removed, so a command is still a struct with a Run function.

// selectorFlags are the flags that narrow a command to part of the workspace.
// Four commands bound the same four flags and assembled the same selector, and
// the wording of the help text had already drifted between them.
type selectorFlags struct {
	only     *string
	group    *string
	role     *string
	archived *bool
}

// bindSelector registers the selection flags on a flag set.
//
// includeArchived is false for commands where operating on an archived
// repository makes no sense, so the flag is not offered at all rather than
// offered and quietly ignored.
func bindSelector(set *flag.FlagSet, includeArchived bool) selectorFlags {
	flags := selectorFlags{
		only:  set.String("only", "", "only these repositories, by name (comma-separated)"),
		group: set.String("group", "", "only repositories in these groups"),
		role:  set.String("role", "", "only repositories with these roles"),
	}
	if includeArchived {
		flags.archived = set.Bool("archived", false, "include archived repositories")
	}
	return flags
}

// resolve returns the repositories the flags select, treating bare arguments as
// repository names so `vat status payments` works alongside `--only payments`.
//
// A group or role matching nothing comes back as an error from the manifest
// rather than an empty list: an empty run in CI is a green build that did
// nothing.
func (f selectorFlags) resolve(ws *workspace.Workspace, set *flag.FlagSet) ([]manifest.Repo, error) {
	names := splitList(*f.only)
	if set != nil {
		names = append(names, set.Args()...)
	}
	selector := manifest.Selector{
		Names:  names,
		Groups: splitList(*f.group),
		Roles:  splitList(*f.role),
	}
	if f.archived != nil {
		selector.IncludeArchive = *f.archived
	}
	repos, err := ws.Select(selector)
	if err != nil {
		return nil, usageErrorf("%v", err)
	}
	return repos, nil
}

// emitJSON writes a payload as indented JSON on the command's output stream.
//
// Callers hand it a value that marshals to an array rather than nil wherever a
// list is meant, so a consumer iterating the result never has to special-case a
// null.
func emitJSON(env *Env, payload any) error {
	encoder := json.NewEncoder(env.Printer.Out())
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
