// Package changeset records the completion evidence for a change that spans
// several repositories.
//
// Choosing many repositories over one costs you the atomic commit. The usual
// answer is to hope: each repository is committed separately, the combination
// is verified once by hand, and six weeks later nobody can say which revisions
// were ever tested together or what to roll back. A changeset is the record
// that pays that cost back — the revision bundle, the check that passed on each
// one, and the point each repository can return to.
package changeset

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/takealook97/vat/internal/fsx"
)

// Status is where a changeset sits in its lifecycle.
type Status string

const (
	// StatusOpen means repositories are mid-change and nothing is proven.
	StatusOpen Status = "open"
	// StatusVerified means every repository's canonical checks passed at a
	// recorded revision.
	StatusVerified Status = "verified"
	// StatusClosed means the change was accepted and released.
	StatusClosed Status = "closed"
	// StatusRolledBack means the change was reverted using the recorded
	// rollback points.
	StatusRolledBack Status = "rolled_back"
	// StatusAbandoned means the work stopped without shipping.
	StatusAbandoned Status = "abandoned"
)

// Open reports whether the changeset still represents unfinished work.
func (s Status) Open() bool { return s == StatusOpen || s == StatusVerified }

// CheckRun is one canonical check, its outcome, and when it ran.
type CheckRun struct {
	Command  string `yaml:"command" json:"command"`
	Status   string `yaml:"status" json:"status"`
	RanAt    string `yaml:"ran_at,omitempty" json:"ran_at,omitempty"`
	Revision string `yaml:"revision,omitempty" json:"revision,omitempty"`
	Detail   string `yaml:"detail,omitempty" json:"detail,omitempty"`
}

// Passed reports whether the check succeeded.
func (c CheckRun) Passed() bool { return c.Status == "pass" }

// Participant is one repository's part in the change.
type Participant struct {
	Name string `yaml:"name" json:"name"`
	// RollbackPoint is the revision before the change began. Recorded at
	// enrolment, because after the change lands it can no longer be observed.
	RollbackPoint string `yaml:"rollback_point,omitempty" json:"rollback_point,omitempty"`
	// Revision is the revision the checks were verified against.
	Revision string     `yaml:"revision,omitempty" json:"revision,omitempty"`
	Branch   string     `yaml:"branch,omitempty" json:"branch,omitempty"`
	Checks   []CheckRun `yaml:"checks,omitempty" json:"checks,omitempty"`
	Notes    string     `yaml:"notes,omitempty" json:"notes,omitempty"`
	// LandedOn is the ref the verified revision was found to be an ancestor of,
	// and LandedAt is when that was observed. Verifying a revision proves the
	// checks passed on it; it says nothing about whether that revision ever
	// reached the branch the repository ships from. Recording the two
	// separately is what stops "we tested it" from being read as "we shipped
	// it" six weeks later.
	LandedOn string `yaml:"landed_on,omitempty" json:"landed_on,omitempty"`
	LandedAt string `yaml:"landed_at,omitempty" json:"landed_at,omitempty"`
	// Review is the forge's own record — a pull request, a merge request, a
	// change. It is evidence, never the gate: every forge names it differently
	// and vat refuses to depend on any of them.
	Review string `yaml:"review,omitempty" json:"review,omitempty"`
}

// Landed reports whether the verified revision was observed on the branch this
// repository ships from.
func (p Participant) Landed() bool { return p.LandedOn != "" }

// Verified reports whether every recorded check for this repository passed and
// the revision they ran against is known.
func (p Participant) Verified() bool {
	if p.Revision == "" || len(p.Checks) == 0 {
		return false
	}
	for _, check := range p.Checks {
		if !check.Passed() {
			return false
		}
	}
	return true
}

// Changeset is the whole record.
type Changeset struct {
	ID        string   `yaml:"id" json:"id"`
	Objective string   `yaml:"objective" json:"objective"`
	Status    Status   `yaml:"status" json:"status"`
	OpenedAt  string   `yaml:"opened_at" json:"opened_at"`
	ClosedAt  string   `yaml:"closed_at,omitempty" json:"closed_at,omitempty"`
	NonGoals  []string `yaml:"non_goals,omitempty" json:"non_goals,omitempty"`
	Contracts []string `yaml:"contracts,omitempty" json:"contracts,omitempty"`
	// Acceptance is the one end-to-end outcome that proves the pieces work
	// together. Per-repository checks passing is not the same thing.
	Acceptance   string        `yaml:"integration_acceptance,omitempty" json:"integration_acceptance,omitempty"`
	Repositories []Participant `yaml:"repositories" json:"repositories"`
	// Decisions links the changeset to the reasoning that authorised it.
	// LandingWaived records that closing went ahead without landing evidence.
	//
	// The rule that reports the gap keys on this rather than on absent
	// landed_on, because absence means two different things: a gate somebody
	// waived, and a changeset closed by a vat that did not yet record landing
	// at all. Keying on absence reported every historical changeset in every
	// workspace, forever, with nothing anyone could do about it.
	LandingWaived bool     `yaml:"landing_waived,omitempty" json:"landing_waived,omitempty"`
	Decisions     []string `yaml:"decisions,omitempty" json:"decisions,omitempty"`
	ApprovedBy    string   `yaml:"approved_by,omitempty" json:"approved_by,omitempty"`
	Notes         string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// Dir is the workspace directory holding changesets.
const Dir = "changesets"

// SchemaURL is where the published JSON Schema for this record version lives.
// It is written into every record vat saves. The schema version is in the
// filename, so this URL does not move when a version 2 is published beside it.
const SchemaURL = "https://raw.githubusercontent.com/takealook97/vat/main/schemas/vat-changeset-v1.schema.json"

var idPattern = regexp.MustCompile(`^CS-(\d+)$`)

// ErrNotFound is returned when no changeset exists for an identifier.
var ErrNotFound = errors.New("no such changeset")

// ErrInvalidID is returned for an identifier that is not of the form CS-0001.
var ErrInvalidID = errors.New("invalid changeset id")

// ValidID reports whether an identifier is safe to build a path from.
//
// The identifier is pasted into a filename and the file is then written whole.
// An unchecked "../../../x" escapes the workspace entirely — through `Load`
// from a command-line argument, and through `Save` from the `id:` field of a
// changeset somebody committed. Writing outside the root is the boundary the
// whole tool rests on, and the defect that retracted three releases.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Path returns the file a changeset lives in, relative to the workspace root.
// It is meaningful only for an identifier ValidID accepts; callers reach it
// through Load and Save, which check first.
func Path(id string) string { return filepath.Join(Dir, id+".yaml") }

// Load reads one changeset from the workspace root.
func Load(root, id string) (Changeset, error) {
	if !ValidID(id) {
		return Changeset{}, fmt.Errorf("%w: %s is not of the form CS-0001", ErrInvalidID, id)
	}
	path := filepath.Join(root, Path(id))
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// The absolute path is dropped: the identifier is what the user typed
		// and the only part they can act on.
		return Changeset{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Changeset{}, fmt.Errorf("read changeset %s: %w", id, err)
	}
	var parsed Changeset
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&parsed); err != nil {
		return Changeset{}, fmt.Errorf("parse %s: %w", Path(id), err)
	}
	// The identifier decides where the next Save writes. A file whose id
	// disagrees with its own name would be read as one changeset and written
	// back as another, silently overwriting it.
	if parsed.ID != id {
		return Changeset{}, fmt.Errorf("%w: %s declares id %q", ErrInvalidID, Path(id), parsed.ID)
	}
	return parsed, nil
}

// LoadAll reads every changeset in the workspace, newest identifier first.
func LoadAll(root string) ([]Changeset, error) {
	dir := filepath.Join(root, Dir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []Changeset{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	sets := []Changeset{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		set, err := Load(root, id)
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	sort.SliceStable(sets, func(i, j int) bool {
		return numeric(sets[i].ID) > numeric(sets[j].ID)
	})
	return sets, nil
}

func numeric(id string) int {
	match := idPattern.FindStringSubmatch(id)
	if match == nil {
		return 0
	}
	value, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return value
}

// NextID returns the next unused changeset identifier.
func NextID(root string) (string, error) {
	sets, err := LoadAll(root)
	if err != nil {
		return "", err
	}
	highest := 0
	for _, set := range sets {
		if value := numeric(set.ID); value > highest {
			highest = value
		}
	}
	return fmt.Sprintf("CS-%04d", highest+1), nil
}

// Save writes a changeset atomically.
func Save(root string, set Changeset) error {
	if set.ID == "" {
		return fmt.Errorf("a changeset needs an id")
	}
	// The identifier decides where this is written, and it arrives from the
	// `id:` field of a file on disk. A changeset committed with a traversing id
	// would otherwise place its next save outside the workspace.
	if !ValidID(set.ID) {
		return fmt.Errorf("%w: %s is not of the form CS-0001", ErrInvalidID, set.ID)
	}
	encoded, err := yaml.Marshal(set)
	if err != nil {
		return fmt.Errorf("encode %s: %w", set.ID, err)
	}
	// A modeline first, so the record carries the means of validating itself to
	// any tool that reads YAML — the format is published for readers that are
	// not vat, and a reader that is not a person needs more than prose.
	header := "# yaml-language-server: $schema=" + SchemaURL + "\n" +
		"# vat changeset — the revision bundle, the evidence, and the way back.\n" +
		"# Written by `vat changeset`. Safe to edit by hand; `vat lint` checks it.\n"
	path := filepath.Join(root, Path(set.ID))
	return fsx.WriteFileAtomic(path, append([]byte(header), encoded...), fsx.DefaultFileMode)
}

// New builds an empty changeset.
func New(id, objective string, now time.Time) Changeset {
	return Changeset{
		ID:        id,
		Objective: objective,
		Status:    StatusOpen,
		OpenedAt:  now.Format("2006-01-02"),
	}
}

// WithParticipant returns a copy of the changeset with a repository added or
// replaced. The receiver is never mutated.
func WithParticipant(set Changeset, participant Participant) Changeset {
	repos := make([]Participant, len(set.Repositories), len(set.Repositories)+1)
	copy(repos, set.Repositories)
	for i := range repos {
		if repos[i].Name == participant.Name {
			repos[i] = participant
			out := set
			out.Repositories = repos
			return out
		}
	}
	repos = append(repos, participant)
	out := set
	out.Repositories = repos
	return out
}

// Participant returns the named repository's record.
func (c Changeset) Participant(name string) (Participant, bool) {
	for _, participant := range c.Repositories {
		if participant.Name == name {
			return participant, true
		}
	}
	return Participant{}, false
}

// FullyVerified reports whether every participating repository has passing
// checks recorded against a known revision.
func (c Changeset) FullyVerified() bool {
	if len(c.Repositories) == 0 {
		return false
	}
	for _, participant := range c.Repositories {
		if !participant.Verified() {
			return false
		}
	}
	return true
}

// FullyLanded reports whether every participating repository's verified
// revision was observed on the branch it ships from.
//
// Verification and landing are deliberately separate questions. Checks passing
// on a revision proves the combination works; it says nothing about whether
// that revision ever reached anyone else's default branch. A changeset closed
// on the first without the second is a claim that outran its evidence.
func (c Changeset) FullyLanded() bool {
	if len(c.Repositories) == 0 {
		return false
	}
	for _, participant := range c.Repositories {
		if !participant.Landed() {
			return false
		}
	}
	return true
}

// AgeDays returns how long the changeset has been open.
func (c Changeset) AgeDays(now time.Time) int {
	opened, err := time.Parse("2006-01-02", c.OpenedAt)
	if err != nil {
		return 0
	}
	return int(now.Sub(opened).Hours() / 24)
}

// RollbackPlan returns the ordered instructions to undo the change, or an error
// naming the repositories whose rollback point was never recorded.
//
// The plan is generated rather than written down because it is only correct if
// it matches what was actually verified, and a hand-written one drifts the
// moment a repository is added.
func (c Changeset) RollbackPlan() ([]string, error) {
	var missing []string
	var steps []string
	// Undo in reverse enrolment order: consumers first, then the contract they
	// depend on, so no window exists where a consumer expects an interface that
	// is already gone.
	for i := len(c.Repositories) - 1; i >= 0; i-- {
		participant := c.Repositories[i]
		if participant.RollbackPoint == "" {
			missing = append(missing, participant.Name)
			continue
		}
		steps = append(steps, fmt.Sprintf("git -C %s reset --hard %s   # was %s",
			participant.Name, participant.RollbackPoint, shortOr(participant.Revision, "unverified")))
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no rollback point recorded for: %s", strings.Join(missing, ", "))
	}
	return steps, nil
}

func shortOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

// Validate reports structural problems in a changeset.
func Validate(set Changeset, requireRollbackPoint bool) []string {
	var problems []string
	if !idPattern.MatchString(set.ID) {
		problems = append(problems, fmt.Sprintf("id %q is not of the form CS-0001", set.ID))
	}
	if strings.TrimSpace(set.Objective) == "" {
		problems = append(problems, "objective is empty")
	}
	switch set.Status {
	case StatusOpen, StatusVerified, StatusClosed, StatusRolledBack, StatusAbandoned:
	default:
		problems = append(problems, fmt.Sprintf("unknown status %q", set.Status))
	}
	if len(set.Repositories) == 0 {
		problems = append(problems, "no repositories enrolled; a changeset with one repository is just a commit")
	}
	seen := map[string]bool{}
	for _, participant := range set.Repositories {
		if participant.Name == "" {
			problems = append(problems, "a participating repository has no name")
			continue
		}
		if seen[participant.Name] {
			problems = append(problems, fmt.Sprintf("%s is enrolled twice", participant.Name))
		}
		seen[participant.Name] = true
		if requireRollbackPoint && participant.RollbackPoint == "" {
			problems = append(problems,
				fmt.Sprintf("%s has no rollback_point; it cannot be undone", participant.Name))
		}
	}
	if set.Status == StatusClosed {
		if set.Acceptance == "" {
			problems = append(problems,
				"closed without integration_acceptance; per-repository checks do not prove the pieces work together")
		}
		if !set.FullyVerified() {
			problems = append(problems, "closed while some repository has no passing checks at a recorded revision")
		}
	}
	return problems
}
