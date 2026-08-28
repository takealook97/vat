// Package brain implements the reviewed-knowledge layer: atomic records that
// own one fact each, generated projections over them, and a lifecycle that
// stops an old claim from quietly remaining "current" forever.
//
// The package deliberately knows nothing about the workspace manifest or about
// git. It reads and writes a directory of Markdown records. Everything that
// needs to correlate a record with a repository revision lives one layer up, so
// a workspace that never adopts this layer pays nothing for it.
package brain

import (
	"errors"
	"fmt"
	"github.com/takealook97/vat/internal/fsx"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kind is the category of an atomic record. Each kind owns a different class of
// fact and lives in its own directory.
type Kind string

const (
	// KindGoal is an outcome the organisation is trying to reach, with the
	// criterion that decides whether it has been reached.
	KindGoal Kind = "goal"
	// KindGap is a specific distance between a goal and the present.
	KindGap Kind = "gap"
	// KindDecision is a judgement that should not be silently reversed.
	KindDecision Kind = "decision"
	// KindMemory is a reviewed observation worth reaching for again: the
	// situation that should bring it back, and what to do differently when it
	// does. It is deliberately not a session handoff and not an agent's
	// journal — those belong to the runtime and to the agent's own repository,
	// and letting them in here is how a knowledge repository fills with
	// material nobody can cite.
	KindMemory Kind = "memory"
)

// Kinds lists every record kind.
func Kinds() []Kind { return []Kind{KindGoal, KindGap, KindDecision, KindMemory} }

// Dir returns the directory a kind's records live in.
func (k Kind) Dir() string {
	switch k {
	case KindGoal:
		return "goals"
	case KindGap:
		return "gaps"
	case KindDecision:
		return "decisions"
	case KindMemory:
		return "memory"
	default:
		return string(k)
	}
}

// Prefix returns the identifier prefix vat assigns when creating a record.
func (k Kind) Prefix() string {
	switch k {
	case KindGoal:
		return "O"
	case KindGap:
		return "G"
	case KindDecision:
		return "D"
	case KindMemory:
		return "M"
	default:
		return "X"
	}
}

// ParseKind resolves a user-supplied kind name, accepting singular and plural.
func ParseKind(value string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "goal", "goals":
		return KindGoal, nil
	case "gap", "gaps":
		return KindGap, nil
	case "decision", "decisions":
		return KindDecision, nil
	case "memory", "memories":
		return KindMemory, nil
	default:
		return "", fmt.Errorf("unknown record kind %q (want goal, gap, decision, or memory)", value)
	}
}

// Status is a record's position in the knowledge lifecycle.
//
// Two states carry most of the value. Stale exists because a claim about the
// present stops being evidence once nobody has re-checked it, and demoting it
// automatically is what keeps the review queue from growing without bound.
// Quarantined exists because a claim can be suspect without being disproven,
// and deleting it would destroy the trail that shows why it was doubted.
type Status string

const (
	// StatusProvisional is a claim recorded but not yet reviewed.
	StatusProvisional Status = "provisional"
	// StatusActive is a reviewed claim currently treated as true.
	StatusActive Status = "active"
	// StatusStale is an active claim whose observation aged past the policy
	// window. It is not false; it is unverified.
	StatusStale Status = "stale"
	// StatusQuarantined is a claim suspected of being wrong or contaminated,
	// withheld from answers until resolved.
	StatusQuarantined Status = "quarantined"
	// StatusSuperseded is a decision replaced by a later one, kept intact so
	// the original reasoning survives.
	StatusSuperseded Status = "superseded"
	// StatusRevoked is a withdrawn claim, kept as a tombstone so a stale
	// index or a cached answer can be traced back to a retraction.
	StatusRevoked Status = "revoked"
	// StatusResolved is a gap that has been closed.
	StatusResolved Status = "resolved"
)

// Statuses lists every valid status.
func Statuses() []Status {
	return []Status{StatusProvisional, StatusActive, StatusStale, StatusQuarantined,
		StatusSuperseded, StatusRevoked, StatusResolved}
}

// Valid reports whether s is a status vat understands.
func (s Status) Valid() bool {
	for _, known := range Statuses() {
		if s == known {
			return true
		}
	}
	return false
}

// Answerable reports whether a record may be cited as current truth.
func (s Status) Answerable() bool {
	return s == StatusActive
}

// Terminal reports whether a record has reached an end state and no longer
// belongs in the working index.
func (s Status) Terminal() bool {
	switch s {
	case StatusSuperseded, StatusRevoked, StatusResolved:
		return true
	default:
		return false
	}
}

// ClaimKind distinguishes a statement about the present, which decays, from a
// statement about a past event, which does not.
type ClaimKind string

const (
	// ClaimCurrentState asserts something is true right now. It requires
	// provenance and expires.
	ClaimCurrentState ClaimKind = "current-state"
	// ClaimHistorical records what happened. It does not decay.
	ClaimHistorical ClaimKind = "historical"
	// ClaimIntent records what the organisation means to do.
	ClaimIntent ClaimKind = "intent"
)

// Valid reports whether k is a claim kind vat understands.
func (k ClaimKind) Valid() bool {
	switch k {
	case ClaimCurrentState, ClaimHistorical, ClaimIntent:
		return true
	default:
		return false
	}
}

// ClaimKinds lists the valid claim kinds for an error message.
func ClaimKinds() string {
	return string(ClaimCurrentState) + ", " + string(ClaimHistorical) + ", " + string(ClaimIntent)
}

// Metadata is the front matter of an atomic record.
type Metadata struct {
	ID     string `yaml:"id"`
	Status Status `yaml:"status"`
	Date   string `yaml:"date,omitempty"`

	// ClaimKind selects which provenance rules apply.
	ClaimKind ClaimKind `yaml:"claim_kind,omitempty"`
	// OwnedBy names the repository or system that is canonical for the fact.
	OwnedBy string `yaml:"owned_by,omitempty"`
	// SourceRef is "<repo>@<revision>:<path>" — the exact place the claim was
	// read from. A revision, not a branch: a branch moves and takes the
	// evidence with it.
	SourceRef string `yaml:"source_ref,omitempty"`
	// SourceExternal declares that SourceRef names a system this workspace does
	// not govern, and deliberately so.
	//
	// Without it the only way to stop vat reporting an ungoverned source was to
	// add that system to vat.yaml — which silences the warning by making the
	// workspace claim to sync, diagnose, and ship a repository it does not own.
	// A rule whose only remedy is the wrong action is worse than no rule.
	//
	// It suppresses nothing else. A claim about an external system still expires
	// like any other; nothing here can re-read it for you, which is the reason
	// to be explicit rather than quiet.
	SourceExternal bool `yaml:"source_external,omitempty"`
	// ObservedAt is the date the claim was last verified against its source.
	ObservedAt string `yaml:"observed_at,omitempty"`
	// RevalidateOn describes what should trigger a re-check.
	RevalidateOn string `yaml:"revalidate_on,omitempty"`

	// Supersedes and SupersededBy form the replacement chain for decisions.
	Supersedes   []string `yaml:"supersedes,omitempty"`
	SupersededBy string   `yaml:"superseded_by,omitempty"`

	// Refs are the identifiers of related records.
	Refs []string `yaml:"refs,omitempty"`
	// Axis groups goals into themes.
	Axis string `yaml:"axis,omitempty"`
	// Reason explains a quarantine or a revocation. Required for both: a
	// withdrawal with no stated cause cannot be reviewed later.
	Reason string `yaml:"reason,omitempty"`
	// ReviewedBy records who promoted the record.
	ReviewedBy string `yaml:"reviewed_by,omitempty"`
}

// Record is one atomic fact: its metadata, its prose, and where it lives.
type Record struct {
	Metadata
	Kind Kind
	// Path is relative to the brain root, using forward slashes.
	Path  string
	Title string
	Body  string
	// Archived reports that the record has been moved out of the working set.
	// It is still loaded — the supersession chain it belongs to is checked
	// from both ends — but it is not part of what the layer is working on.
	Archived bool
}

// Rel returns the record's path relative to the brain root.
func (r Record) Rel() string { return r.Path }

// IsCurrentStateClaim reports whether the record asserts something about now.
func (r Record) IsCurrentStateClaim() bool {
	return r.ClaimKind == ClaimCurrentState
}

// ObservedDate parses ObservedAt, falling back to Date.
func (r Record) ObservedDate() (time.Time, bool) {
	for _, value := range []string{r.ObservedAt, r.Date} {
		if parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value)); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// AgeDays returns how many days have passed since the record was observed.
func (r Record) AgeDays(now time.Time) (int, bool) {
	observed, ok := r.ObservedDate()
	if !ok {
		return 0, false
	}
	return int(now.Sub(observed).Hours() / 24), true
}

// SourceParts splits a source_ref into its repository, revision, and path.
func (r Record) SourceParts() (repo, revision, filePath string, ok bool) {
	return ParseSourceRef(r.SourceRef)
}

var sourceRefPattern = regexp.MustCompile(`^([^@\s]+)@([^:\s]+)(?::(.+))?$`)

// ParseSourceRef splits "<repo>@<revision>[:<path>]".
func ParseSourceRef(ref string) (repo, revision, filePath string, ok bool) {
	match := sourceRefPattern.FindStringSubmatch(strings.TrimSpace(ref))
	if match == nil {
		return "", "", "", false
	}
	return match[1], match[2], match[3], true
}

// ValidateID reports whether an identifier is safe to build a filename from.
//
// Ids are normally generated, but `vat brain new --id` lets a caller supply
// one, and it is pasted straight into a path: an unchecked "../../../x" wrote
// the record outside the workspace and reported success. The rule is
// deliberately looser than any particular numbering scheme -- a workspace may
// name records however it likes -- and only forbids what makes an id a path.
func ValidateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("an identifier is required")
	}
	if len(id) > 64 {
		return errors.New("an identifier may not be longer than 64 characters")
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("identifier %q may contain only letters, digits, '.', '_', and '-'", id)
		}
	}
	if strings.HasPrefix(id, ".") {
		return fmt.Errorf("identifier %q may not begin with '.'", id)
	}
	// The identifier becomes a filename, so it is held to the rule every name
	// that becomes one is: a workspace whose records only its author can check
	// out is not the shared account this layer exists to be.
	if err := fsx.PortableName(id); err != nil {
		return fmt.Errorf("identifier %q: %w", id, err)
	}
	return nil
}

// numericID extracts the leading number of an identifier for sorting, so G-2
// sorts before G-10.
func numericID(id string) int {
	digits := regexp.MustCompile(`\d+`).FindString(id)
	if digits == "" {
		return -1
	}
	value, err := strconv.Atoi(digits)
	if err != nil {
		return -1
	}
	return value
}

// SortRecords orders records by identifier prefix then numeric value.
func SortRecords(records []Record) []Record {
	sorted := make([]Record, len(records))
	copy(sorted, records)
	sort.SliceStable(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		leftPrefix, rightPrefix := idPrefix(left.ID), idPrefix(right.ID)
		if leftPrefix != rightPrefix {
			return leftPrefix < rightPrefix
		}
		leftNum, rightNum := numericID(left.ID), numericID(right.ID)
		if leftNum != rightNum {
			return leftNum < rightNum
		}
		return left.ID < right.ID
	})
	return sorted
}

func idPrefix(id string) string {
	if prefix, _, ok := strings.Cut(id, "-"); ok {
		return prefix
	}
	return id
}

// Slug turns a title into a filename-safe fragment.
func Slug(title string) string {
	var b strings.Builder
	previousDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			previousDash = false
		case r > 127:
			// Keep non-ASCII letters: a record titled in the team's own
			// language should not become an unreadable string of dashes.
			b.WriteRune(r)
			previousDash = false
		default:
			if !previousDash && b.Len() > 0 {
				b.WriteRune('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// FileName composes the filename for a new record.
func FileName(id, title string) string {
	slug := Slug(title)
	if slug == "" {
		return id + ".md"
	}
	return id + "-" + slug + ".md"
}

// JoinPath joins brain-relative path segments with forward slashes.
func JoinPath(parts ...string) string { return path.Join(parts...) }
