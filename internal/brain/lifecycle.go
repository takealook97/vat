package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/frontmatter"
	"github.com/takealook97/vat/internal/fsx"
)

// Transition is a proposed or applied change of a record's status.
type Transition struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	From    Status `json:"from"`
	To      Status `json:"to"`
	Reason  string `json:"reason"`
	Applied bool   `json:"applied"`
}

// Sweep finds active current-state claims whose observation has aged past the
// policy window and demotes them to stale.
//
// This is the mechanism that keeps the knowledge layer honest over years.
// Without it, a claim recorded once stays "active" forever and an agent quotes
// a two-year-old observation as the present. Demotion is not deletion: the
// record and its reasoning survive, it simply stops being citable until someone
// re-checks it.
func Sweep(store *Store, policy CheckPolicy, now time.Time, apply bool) ([]Transition, error) {
	if policy.StaleAfterDays <= 0 {
		return []Transition{}, nil
	}
	transitions := []Transition{}
	for _, record := range store.CurrentStateClaims() {
		if record.Status != StatusActive {
			continue
		}
		age, ok := record.AgeDays(now)
		if !ok || age <= policy.StaleAfterDays {
			continue
		}
		transition := Transition{
			ID: record.ID, Path: record.Path, From: record.Status, To: StatusStale,
			Reason: fmt.Sprintf("observed %d days ago, past the %d-day window",
				age, policy.StaleAfterDays),
		}
		if apply {
			if err := SetStatus(store.Root, record, StatusStale, transition.Reason); err != nil {
				return nil, err
			}
			transition.Applied = true
		}
		transitions = append(transitions, transition)
	}
	return transitions, nil
}

// ReviewItem is one entry in the prioritised re-check queue.
type ReviewItem struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Status     Status `json:"status"`
	Title      string `json:"title"`
	AgeDays    int    `json:"age_days"`
	References int    `json:"references"`
	Priority   int    `json:"priority"`
	Overdue    bool   `json:"overdue"`
	Why        string `json:"why"`
}

// ReviewQueue returns everything awaiting human judgement, ordered by how much
// it costs to leave unresolved.
//
// Priority weights how many other records depend on the claim against how long
// it has gone unverified. A stale claim nothing cites can wait; a stale claim
// half the roadmap rests on cannot. Without this ordering the queue is a flat
// list that grows until it is ignored wholesale.
func ReviewQueue(store *Store, policy CheckPolicy, now time.Time) []ReviewItem {
	references := store.ReferenceCounts()
	items := make([]ReviewItem, 0, len(store.Records))
	for _, record := range store.Records {
		var why string
		switch record.Status {
		case StatusProvisional:
			why = "recorded but never reviewed"
		case StatusStale:
			why = "observation aged out; re-verify against the owning repository"
		case StatusQuarantined:
			why = "suspected wrong; confirm or revoke"
		default:
			continue
		}
		age, _ := record.AgeDays(now)
		count := references[record.ID]
		items = append(items, ReviewItem{
			ID: record.ID, Path: record.Path, Status: record.Status, Title: record.Title,
			AgeDays: age, References: count,
			Priority: priorityOf(record.Status, age, count),
			Overdue:  policy.ReviewSLADays > 0 && age > policy.ReviewSLADays,
			Why:      why,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func priorityOf(status Status, age, references int) int {
	weight := age + references*10
	switch status {
	case StatusQuarantined:
		// A suspected-wrong claim is more urgent than an unverified one,
		// because it may already have been quoted as fact.
		weight += 100
	case StatusProvisional:
		weight += 20
	}
	if weight < 0 {
		weight = 0
	}
	return weight
}

// SetStatus rewrites a record's front matter to a new status, preserving the
// body byte for byte. Reasons accumulate rather than replace, so the record
// keeps the trail of why it moved.
func SetStatus(root string, record Record, status Status, reason string) error {
	path := filepath.Join(root, filepath.FromSlash(record.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc := frontmatter.Split(string(data))
	if !doc.Present {
		return fmt.Errorf("%s has no front matter to update", record.Path)
	}
	metadata := record.Metadata
	metadata.Status = status
	if reason != "" {
		if metadata.Reason == "" {
			metadata.Reason = reason
		} else if !strings.Contains(metadata.Reason, reason) {
			metadata.Reason = metadata.Reason + "; " + reason
		}
	}
	rendered, err := doc.Merge(metadata)
	if err != nil {
		return fmt.Errorf("%s: %w", record.Path, err)
	}
	return fsx.WriteFileAtomic(path, rendered, fsx.DefaultFileMode)
}

// Retire moves a record out of the answerable set by hand: quarantined for a
// claim that is suspect, revoked for one that is withdrawn, resolved for a gap
// that has been closed.
//
// These three states carried check rules and review-queue weights from the
// start, and no command could reach them — so the way to a quarantine ran
// through hand-editing the YAML of the very record whose trustworthiness was in
// doubt. Promotion is deliberately not reachable from here: bringing a record
// back is a review, and a review goes through the gate.
func Retire(root string, record Record, to Status, reason string) error {
	switch to {
	case StatusQuarantined, StatusRevoked, StatusResolved:
	default:
		return fmt.Errorf("%s cannot be set by hand; promote a record to make it citable", to)
	}
	if record.Status.Terminal() {
		return fmt.Errorf("%s is already %s; an end state is not reopened, record a new claim instead",
			record.ID, record.Status)
	}
	if to == StatusResolved && record.Kind != KindGap {
		return fmt.Errorf("%s is a %s; resolved describes a gap that has been closed", record.ID, record.Kind)
	}
	if to == StatusQuarantined || to == StatusRevoked {
		// check already fails on a tombstone with no stated cause. Writing one
		// and then reporting it is worse than refusing to write it.
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%s: a %s record must state a reason, or nobody can review it later",
				record.ID, to)
		}
	}
	return SetStatus(root, record, to, strings.TrimSpace(reason))
}

// PromoteRequest is what a promotion has to satisfy before a record becomes
// citable.
type PromoteRequest struct {
	// Reviewer is who checked it.
	Reviewer string
	// Now is the date the promotion stamps.
	Now time.Time
	// RequireReviewer refuses an unattributed promotion. It carries
	// policy.gates.brain_promote: a manual gate nobody has to sign is not a
	// gate, it is a note.
	RequireReviewer bool
	// SourceRevision is the revision the owning repository is at right now,
	// when the caller could read it. Empty means it could not be checked.
	SourceRevision string
	// Reverified is the reviewer stating they re-read the source themselves.
	// It is the only thing that lets an observation date move forward when the
	// evidence is not demonstrably unchanged.
	Reverified bool
}

// Promote moves a record to active, recording who reviewed it and when it was
// observed.
//
// Three refusals are what make this a gate rather than a status field. A claim
// with no provenance cannot be promoted at all. A withdrawn or replaced record
// cannot be revived — a tombstone that can be flipped back to active is not a
// tombstone, and check would then fail forever with no command able to clear
// it. And a claim about the present cannot have its observation date moved
// forward unless the evidence is provably unchanged or the reviewer says they
// re-read it: otherwise a four-hundred-day-old statement becomes "verified
// today" with one keystroke, which is the exact failure this whole layer exists
// to prevent.
func Promote(root string, record Record, request PromoteRequest) error {
	if record.Status.Terminal() {
		return fmt.Errorf("%s is %s; record a new claim rather than reviving this one",
			record.ID, record.Status)
	}
	if request.RequireReviewer && strings.TrimSpace(request.Reviewer) == "" {
		return fmt.Errorf("%s: policy.gates.brain_promote is manual, so a promotion must name its reviewer", record.ID)
	}
	metadata := record.Metadata
	if record.IsCurrentStateClaim() {
		if strings.TrimSpace(record.SourceRef) == "" {
			return fmt.Errorf("%s: a current-state claim needs source_ref before it can be promoted", record.ID)
		}
		if strings.TrimSpace(record.OwnedBy) == "" {
			return fmt.Errorf("%s: a current-state claim needs owned_by before it can be promoted", record.ID)
		}
		repointed, err := confirmEvidence(record, request)
		if err != nil {
			return err
		}
		metadata.SourceRef = repointed
	}
	path := filepath.Join(root, filepath.FromSlash(record.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc := frontmatter.Split(string(data))
	metadata.Status = StatusActive
	metadata.ObservedAt = request.Now.Format("2006-01-02")
	if request.Reviewer != "" {
		metadata.ReviewedBy = request.Reviewer
	}
	rendered, err := doc.Merge(metadata)
	if err != nil {
		return fmt.Errorf("%s: %w", record.Path, err)
	}
	return fsx.WriteFileAtomic(path, rendered, fsx.DefaultFileMode)
}

// confirmEvidence decides whether the observation date may move, and returns
// the source_ref the promotion should write.
//
// Unchanged evidence is the one case where re-dating needs no ceremony: the
// repository is at the same revision the claim was read from, so nothing about
// the source has changed since. Everything else — the source moved, or vat
// could not see it — needs the reviewer to say they looked.
func confirmEvidence(record Record, request PromoteRequest) (string, error) {
	repo, revision, filePath, ok := record.SourceParts()
	if !ok {
		return "", fmt.Errorf("%s: source_ref %q is not <repo>@<revision>[:<path>]", record.ID, record.SourceRef)
	}
	if request.SourceRevision != "" && request.SourceRevision == revision {
		return record.SourceRef, nil
	}
	if !request.Reverified {
		if request.SourceRevision == "" {
			return "", fmt.Errorf(
				"%s: vat could not read %s to confirm the evidence is unchanged.\n"+
					"  Re-read the source yourself, then: vat brain promote %s --reverified",
				record.ID, repo, record.ID)
		}
		return "", fmt.Errorf(
			"%s: %s has moved since this was observed (pinned %s, now %s), so the observation date cannot be advanced.\n"+
				"  Re-read the source at the new revision, then: vat brain promote %s --reverified",
			record.ID, repo, revision, request.SourceRevision, record.ID)
	}
	if request.SourceRevision == "" {
		return record.SourceRef, nil
	}
	// The reviewer read the source as it stands now, so that is the revision
	// the claim is evidence for. Leaving the old one would date the record
	// today against a revision nobody looked at.
	if filePath != "" {
		return fmt.Sprintf("%s@%s:%s", repo, request.SourceRevision, filePath), nil
	}
	return fmt.Sprintf("%s@%s", repo, request.SourceRevision), nil
}

// SupersedeOptions bounds what superseding is allowed to do beyond linking.
type SupersedeOptions struct {
	// PromotionGated keeps the replacement provisional. It carries
	// policy.brain.require_promotion_gate, and it closes the one path by which
	// a record used to become canonical without anyone reviewing it: writing a
	// new decision and superseding an old one with it.
	PromotionGated bool
}

// Supersede links a replacement decision to the one it replaces, updating both
// records so the chain reads correctly from either end.
func Supersede(root string, previous, replacement Record, opts SupersedeOptions) error {
	previousMeta := previous.Metadata
	previousMeta.Status = StatusSuperseded
	previousMeta.SupersededBy = replacement.ID

	replacementMeta := replacement.Metadata
	if !contains(replacementMeta.Supersedes, previous.ID) {
		replacementMeta.Supersedes = append(replacementMeta.Supersedes, previous.ID)
	}
	if replacementMeta.Status == StatusProvisional && !opts.PromotionGated {
		replacementMeta.Status = StatusActive
	}

	for _, update := range []struct {
		record   Record
		metadata Metadata
	}{{previous, previousMeta}, {replacement, replacementMeta}} {
		path := filepath.Join(root, filepath.FromSlash(update.record.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		doc := frontmatter.Split(string(data))
		rendered, err := doc.Merge(update.metadata)
		if err != nil {
			return fmt.Errorf("%s: %w", update.record.Path, err)
		}
		if err := fsx.WriteFileAtomic(path, rendered, fsx.DefaultFileMode); err != nil {
			return err
		}
	}
	return nil
}
