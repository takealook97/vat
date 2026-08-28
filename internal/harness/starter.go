package harness

import (
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
)

// A new workspace arrives with contracts and no procedures. The generated
// AGENTS.md says what a repository is responsible for and what it never does; it
// cannot say when to open a changeset or when to consult the knowledge layer,
// because those are sequences rather than boundaries.
//
// What is seeded is deliberately narrow: only vat's own command sequences. An
// opinion about how somebody should engineer their software would be a second
// source of truth vat writes once and never maintains, which is the failure this
// tool exists to prevent. A procedure made of vat's own commands is one vat can
// be held to — a test asserts every command these bodies name actually exists.

// StarterSkills returns the procedures a new workspace is seeded with.
func StarterSkills() []Skill {
	return []Skill{
		{
			Name:        "before-cross-repo-work",
			Description: "Open and close a changeset when a change spans more than one repository.",
			Body:        starterCrossRepo,
		},
		{
			Name:        "consult-the-brain-first",
			Description: "Check the knowledge layer before stating something as true of this workspace.",
			Body:        starterConsultBrain,
		},
	}
}

// WriteStarterSkills seeds the canonical procedures, returning the relative
// paths it wrote.
//
// A skill that is already on disk is left exactly as it is. The seed becomes the
// user's file the moment it lands, and rewriting an edited procedure would make
// vat the author of a document somebody else is responsible for following.
func WriteStarterSkills(root string) ([]string, error) {
	var written []string
	for _, skill := range StarterSkills() {
		rel := filepath.Join(SkillsDir, skill.Name, SkillFile)
		path := filepath.Join(root, rel)
		if fsx.Exists(path) {
			continue
		}
		if err := fsx.WriteFileAtomic(path, []byte(renderStarter(skill)), fsx.DefaultFileMode); err != nil {
			return nil, err
		}
		written = append(written, rel)
	}
	return written, nil
}

func renderStarter(skill Skill) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + skill.Name + "\n")
	b.WriteString("description: " + yamlScalar(skill.Description) + "\n")
	b.WriteString("---\n\n")
	b.WriteString(skill.Body)
	return b.String()
}

const starterCrossRepo = `# Before cross-repository work

## When to use this

A change will touch more than one repository. Open the record before the first
edit, not after: ` + "`new`" + ` and ` + "`add`" + ` record where each repository stood before
the change began, and once it has landed that can no longer be observed.

## Steps

1. ` + "`vat changeset new \"<objective>\" --repos a,b`" + `. The objective is the one
   claim the record makes, so write what should be true afterwards rather than
   what you are about to type.
2. ` + "`vat changeset add <id> <repo>`" + ` for a repository that turns out to be
   involved after all. It refuses once the changeset is closed, because
   enrolling a repository afterwards rewrites the claim the record exists to
   make.
3. Do the work.
4. ` + "`vat changeset verify <id>`" + `. It runs each repository's canonical checks
   and records the result against the exact revision it ran on. It refuses on a
   dirty working tree.
5. Land the verified commits through the repository's normal review and merge
   path, then run ` + "`vat ship <id>`" + `. It judges whether those exact revisions
   reached the branch each repository ships from; it never pushes or merges.
6. ` + "`vat changeset close <id> --acceptance \"...\"`" + `. The acceptance must
   describe something end to end; verifying proves the combination builds, not
   that it does what it was for. Close refuses before shipping is recorded.

## When it must stop

If verification fails, stop. Do not close the changeset to tidy the list — a
closed record asserts that this combination was checked together, and one closed
over a red check is worse than no record, because somebody will trust it.

If the work is abandoned, ` + "`vat changeset abandon <id> --reason \"...\"`" + `. Why it
stopped is the whole value of an abandoned record.

To undo, ` + "`vat changeset undo-plan <id>`" + ` prints the commands that would return
every repository to its start point. It prints them and never runs them; read
them before you do.
`

const starterConsultBrain = `# Consult the brain first

## When to use this

Before stating something as true of this workspace — how a service behaves, why
a decision was made, what a repository owns — when the answer is not visible in
the file in front of you.

## Steps

1. ` + "`vat brain query <terms>`" + ` for what is held to be true now. The default
   surface is deliberately narrow.
2. ` + "`vat brain query <terms> --all`" + ` when the question is why something was
   decided rather than what is true, since that widens the search to history,
   archives, and terminal records.
3. Read the record's status before citing it. A ` + "`provisional`" + ` record has not
   crossed the promotion gate, and a claim pinned to a revision its repository
   has since moved past is evidence about a tree that no longer exists.
4. ` + "`vat brain review`" + ` to see which claims are most worth re-checking: it
   orders by how many records cite a claim against how long it has gone
   unverified.
5. ` + "`vat brain check`" + ` before trusting the layer as a whole.

## When it must stop

If no record answers the question, say so. Inventing the answer and writing it
into a record is how a knowledge layer becomes a place where wrong things are
harder to correct than they were when nobody had written them down.

Do not promote a record to make a citation look stronger. Promotion is a claim
that the evidence was re-read, and ` + "`vat brain promote`" + ` refuses to move the
observation date forward unless the evidence is demonstrably unchanged or you
state with ` + "`--reverified`" + ` that you read the source yourself.
`
