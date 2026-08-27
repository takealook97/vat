package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/harness"
	"github.com/takealook97/vat/internal/ui"
)

// Skills are a separate file from roles for the reason the two are separate
// kinds: a role says who is running and renders an adapter per runtime, while a
// skill is a procedure loaded on demand and renders one for Claude Code alone.
// Folding them into one command would have to keep explaining which half of the
// behaviour it means.

func harnessSkillsCommand() *Command {
	return &Command{
		Name:    "skills",
		Summary: "List the procedures this workspace defines",
		Usage:   "vat harness skills",
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("harness skills")
			if err := parseFlags(set, args); err != nil {
				return err
			}
			ws, err := env.Workspace()
			if err != nil {
				return err
			}
			skills, malformed, err := harness.LoadSkills(ws.Root)
			if err != nil {
				return err
			}
			if env.JSON {
				listed := make([]skillSummary, 0, len(skills))
				for _, skill := range skills {
					listed = append(listed, summariseSkill(skill))
				}
				if err := emitJSON(env, listed); err != nil {
					return err
				}
				return reportUnreadableDefinitions(env, malformed, "skill")
			}
			if len(skills) == 0 {
				env.Printer.Println("No skills defined.")
				env.Printer.Hint("Create one with `vat harness skill new <name>`.")
				return nil
			}
			rows := make([][]string, 0, len(skills))
			for _, skill := range skills {
				summary := summariseSkill(skill)
				// A skill that renders nothing is on disk and unreachable. The
				// column says so rather than printing an empty cell somebody
				// reads as "all of them".
				runtimes := "none"
				if len(summary.Runtimes) > 0 {
					runtimes = strings.Join(summary.Runtimes, ",")
				}
				rows = append(rows, []string{summary.Name, runtimes, summary.Description})
			}
			env.Printer.Table([]string{"SKILL", "RUNTIMES", "DESCRIPTION"}, rows)
			return reportUnreadableDefinitions(env, malformed, "skill")
		},
	}
}

// skillSummary is the shape `vat harness skills` reports, in either form.
type skillSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Runtimes    []string `json:"runtimes"`
	Path        string   `json:"path,omitempty"`
}

func summariseSkill(skill harness.Skill) skillSummary {
	// Reported from the adapters that are actually rendered rather than from
	// the runtimes: field, because those two differ exactly where it matters —
	// `runtimes: [codex]` names a real runtime and selects no skill adapter.
	runtimes := make([]string, 0, len(harness.SkillRuntimeNames()))
	for _, adapter := range harness.RenderSkillAdapters(skill) {
		runtimes = append(runtimes, adapter.Runtime)
	}
	return skillSummary{
		Name: skill.Name, Description: skill.Description, Runtimes: runtimes,
		Path: filepath.Join(harness.SkillsDir, skill.Dir, harness.SkillFile),
	}
}

func harnessSkillCommand() *Command {
	return &Command{
		Name:    "skill",
		Summary: "Create a new runtime-neutral procedure",
		Usage:   "vat harness skill new <name> [--description <text>] [--runtimes <list>]",
		Long: `Create a skill definition under .agents/skills/.

The skill body is the canonical procedure. Runtime adapters carry a pointer to
it and never a copy, because two procedures that disagree by one step are worse
than one nobody has read.

A skill is advertised by its description. Without one it is on disk, generated
into every adapter, and invisible to the agent that needed it.`,
		Run: runHarnessSkill,
	}
}

func runHarnessSkill(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("harness skill")
	description := set.String("description", "", "one line saying when to load this procedure")
	runtimes := set.String("runtimes", "", "runtimes to generate adapters for (default: all)")
	if err := parseFlags(set, args); err != nil {
		return err
	}
	if set.NArg() != 2 || set.Arg(0) != "new" {
		return usageErrorf("expected: vat harness skill new <name>")
	}
	name := set.Arg(1)
	// Validated with the role rule because the loader validates skill names the
	// same way, but reported as a skill: naming the wrong kind here sends the
	// reader to a directory that holds nothing of theirs.
	if !harness.ValidRoleName(name) {
		return usageErrorf(
			"%q is not a usable skill name; use letters, digits, '-', and '_' only.\n"+
				"  The name becomes a directory in every runtime's skill directory.", name)
	}

	ws, err := env.Workspace()
	if err != nil {
		return err
	}
	path := filepath.Join(ws.Root, harness.SkillsDir, name, harness.SkillFile)
	if fsx.Exists(path) {
		return usageErrorf("%s already exists", ws.Rel(path))
	}

	skill := harness.Skill{
		Name:        name,
		Description: *description,
		Runtimes:    splitList(*runtimes),
	}
	content, err := renderSkillTemplate(skill)
	if err != nil {
		return err
	}
	if err := fsx.WriteFileAtomic(path, content, fsx.DefaultFileMode); err != nil {
		return err
	}
	env.Printer.Status(ui.LevelOK, ws.Rel(path), "created")

	if err := renderSkillAdapters(env, ws.Root); err != nil {
		return err
	}
	reportSkillGaps(env, skill)
	env.Printer.Hint("\nWrite the procedure in %s. The adapters regenerate from it.", ws.Rel(path))
	return nil
}

// renderSkillAdapters regenerates every skill adapter and names what it wrote.
//
// A definition that was already broken is not this command's fault and must not
// fail it, but creating one beside a file nobody can read while saying nothing
// leaves the workspace quietly short of an adapter.
func renderSkillAdapters(env *Env, root string) error {
	skills, malformed, err := harness.LoadSkills(root)
	if err != nil {
		return err
	}
	adapters, err := harness.WriteSkillAdapters(root, skills)
	if err != nil {
		return err
	}
	for _, adapter := range adapters {
		env.Printer.Status(ui.LevelOK, adapter, "generated")
	}
	for _, entry := range malformed {
		env.Printer.Status(ui.LevelWarn, entry.Path,
			"cannot be read, so it renders no adapter: "+entry.Problem)
	}
	return nil
}

// reportSkillGaps says at creation what `vat lint` would say on the next run.
//
// One line each, and they save creating a skill, walking away, and discovering
// days later that nothing ever loaded it.
func reportSkillGaps(env *Env, skill harness.Skill) {
	if len(harness.RenderSkillAdapters(skill)) == 0 {
		env.Printer.Status(ui.LevelWarn, skill.Name, fmt.Sprintf(
			"renders no adapter, so nothing will load it; a skill is generated for %s or nothing at all",
			strings.Join(harness.SkillRuntimeNames(), ", ")))
	}
	// The description is left empty rather than filled with a placeholder: a
	// generated one would satisfy harness/skill-metadata while telling the
	// runtime nothing, and that rule exists to catch exactly the skill nobody
	// can be offered.
	if strings.TrimSpace(skill.Description) == "" {
		env.Printer.Status(ui.LevelWarn, skill.Name,
			"has no description, so no runtime can advertise it; add description: to the skill")
	}
}

func renderSkillTemplate(skill harness.Skill) ([]byte, error) {
	body := fmt.Sprintf(`# %s

## When to use this

_The situation that should make somebody load this procedure. A skill nobody can
tell when to read is a skill that is never read._

## Inputs

_What must be read, or true, before the first step._

## Steps

_Numbered. Each one an action somebody can take and then check._

## When it must stop

_The conditions under which this procedure reports rather than proceeds: a
judgement that belongs to a human, a step that cannot be undone, a gate this
procedure does not hold._
`, skill.Name)

	metadata := struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Runtimes    []string `yaml:"runtimes,omitempty"`
	}{Name: skill.Name, Description: skill.Description, Runtimes: skill.Runtimes}
	return renderFrontmatter(metadata, body)
}
