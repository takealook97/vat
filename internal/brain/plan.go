package brain

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// The adoption plan.
//
// The philosophy `vat brain adopt` already holds is right: nothing is rewritten,
// because a knowledge repository is the one thing in a workspace whose content
// no tool can be trusted to reinterpret. What was missing is the other half —
// making the work enumerable. Reading two hundred findings one file at a time
// tells a team nothing about which conversions are mechanical, which are model
// decisions, and how many of each there are.
//
// A plan proposes no mapping. It counts, groups, and names examples, so a team
// can build only the migration rules its own data proved it needs.

// PlanGroup is one class of work adoption would leave to a human.
type PlanGroup struct {
	// Kind is the stable name of the group, for a script.
	Kind string `json:"kind"`
	// Summary says what the group is, for a person.
	Summary string `json:"summary"`
	// Count is how many records or files are in it.
	Count int `json:"count"`
	// Mechanical marks a group whose members can be converted without deciding
	// what anything means. It is a hint about effort, never permission.
	Mechanical bool `json:"mechanical"`
	// Examples names a few members, so the group can be recognised.
	Examples []string `json:"examples,omitempty"`
}

// Plan is what adopting a repository would find.
type Plan struct {
	Root string `json:"root"`
	// Records is how many were read, sound or not.
	Records int         `json:"records"`
	Groups  []PlanGroup `json:"groups"`
}

// planExamples is how many members of a group are named. Enough to recognise
// the shape; not so many that the plan becomes the list it replaces.
const planExamples = 3

// journalPattern matches a directory named as a month or a file named as a
// date, which is what a session journal looks like on disk.
var journalPattern = regexp.MustCompile(`^\d{4}-\d{2}(-\d{2})?`)

// BuildPlan groups what adoption would report, without changing anything.
func BuildPlan(store *Store, policy CheckPolicy, now time.Time) Plan {
	plan := Plan{Root: store.Root, Records: len(store.Records) + len(store.Malformed)}
	findings := Check(store, policy, now)

	// One group per class of work, not one per rule: a team deciding how to
	// migrate needs to know that twenty-six links are one-sided, not which
	// twenty-six rules fired.
	byKind := map[string]*PlanGroup{}
	order := []struct {
		kind, summary string
		mechanical    bool
		rules         []string
	}{
		{"syntax-malformed", "cannot be read as a record at all", false,
			[]string{"brain/record-malformed"}},
		{"status-unknown", "carries a lifecycle status this schema does not have", false,
			[]string{"brain/status-unknown"}},
		{"relation-asymmetric", "names a supersession the other record does not name back", true,
			[]string{"brain/superseded-asymmetric", "brain/supersedes-asymmetric"}},
		{"relation-missing", "points at a record that is not here", false,
			[]string{"brain/superseded-missing", "brain/supersedes-missing", "brain/ref-missing", "brain/link-broken"}},
		{"date-unreadable", "has a date no staleness rule can read", true,
			[]string{"brain/date-unreadable"}},
		{"identity-incomplete", "has no identifier or no title", true,
			[]string{"brain/id-missing", "brain/id-duplicate", "brain/title-missing"}},
		{"provenance-missing", "claims something about the present without saying where it was read", false,
			[]string{"brain/claim-owner", ruleClaimSource, "brain/claim-observed", "brain/claim-source-branch"}},
		{"withdrawal-unexplained", "was withdrawn or quarantined without a reason", false,
			[]string{"brain/quarantine-reason", "brain/revoke-reason"}},
	}
	for _, group := range order {
		entry := &PlanGroup{Kind: group.kind, Summary: group.summary, Mechanical: group.mechanical}
		for _, rule := range group.rules {
			byKind[rule] = entry
		}
		plan.Groups = append(plan.Groups, *entry)
	}

	counted := map[string]*PlanGroup{}
	for _, finding := range findings {
		entry, known := byKind[finding.Rule]
		if !known {
			// A rule with no group is still work. Reporting it under its own
			// name is better than dropping it, which is how a plan quietly
			// under-reports the size of a migration.
			entry = &PlanGroup{Kind: finding.Rule, Summary: "reported by " + finding.Rule}
			byKind[finding.Rule] = entry
		}
		entry.Count++
		if len(entry.Examples) < planExamples {
			entry.Examples = append(entry.Examples, firstNonEmpty(finding.ID, finding.Path))
		}
		counted[entry.Kind] = entry
	}

	if foreign, err := Unmanaged(store.Root); err == nil && len(foreign) > 0 {
		entry := &PlanGroup{
			Kind: "projection-unmanaged", Count: len(foreign), Examples: foreign,
			Summary: "holds the name of a generated projection but was not written by vat",
		}
		counted[entry.Kind] = entry
	}
	if journals := journalDirectories(store.Root); len(journals) > 0 {
		entry := &PlanGroup{
			Kind: "journal-shaped", Count: len(journals),
			Summary: "dated files under memory/, which reads as a session journal rather than a reusable observation",
		}
		entry.Examples = journals
		if len(entry.Examples) > planExamples {
			entry.Examples = entry.Examples[:planExamples]
		}
		counted[entry.Kind] = entry
	}

	plan.Groups = plan.Groups[:0]
	for _, entry := range counted {
		plan.Groups = append(plan.Groups, *entry)
	}
	sort.SliceStable(plan.Groups, func(i, j int) bool {
		if plan.Groups[i].Count != plan.Groups[j].Count {
			return plan.Groups[i].Count > plan.Groups[j].Count
		}
		return plan.Groups[i].Kind < plan.Groups[j].Kind
	})
	return plan
}

// journalDirectories returns the dated directories and files under memory/.
//
// vat defines a memory as a reviewed observation worth citing again, and a
// repository that kept a dated tree there loads every session note as a
// provisional record — which fills the review queue and the recent-observations
// section with a log. Named, never moved: where a journal belongs is a decision
// about the repository's own history.
func journalDirectories(root string) []string {
	dir := filepath.Join(root, KindMemory.Dir())
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var journals []string
	for _, entry := range entries {
		if !journalPattern.MatchString(entry.Name()) {
			continue
		}
		journals = append(journals, JoinPath(KindMemory.Dir(), entry.Name()))
	}
	sort.Strings(journals)
	return journals
}

// Mechanical reports whether every group in a plan can be converted without
// deciding what a record means.
func (p Plan) Mechanical() bool {
	for _, group := range p.Groups {
		if !group.Mechanical {
			return false
		}
	}
	return true
}

// Total is how many items the plan found across every group.
func (p Plan) Total() int {
	total := 0
	for _, group := range p.Groups {
		total += group.Count
	}
	return total
}
