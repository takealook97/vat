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
		return fmt.Errorf("read %s: %w", record.Path, err)
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
	rendered, err := frontmatter.Render(metadata, doc.Body)
	if err != nil {
		return fmt.Errorf("%s: %w", record.Path, err)
	}
	return fsx.WriteFileAtomic(path, rendered, fsx.DefaultFileMode)
}

// Promote moves a record to active, recording who reviewed it and when it was
// observed. It refuses to promote a claim that has no provenance, which is what
// makes the promotion gate more than an honour system.
func Promote(root string, record Record, reviewer string, now time.Time) error {
	if record.IsCurrentStateClaim() {
		if strings.TrimSpace(record.SourceRef) == "" {
			return fmt.Errorf("%s: a current-state claim needs source_ref before it can be promoted", record.ID)
		}
		if strings.TrimSpace(record.OwnedBy) == "" {
			return fmt.Errorf("%s: a current-state claim needs owned_by before it can be promoted", record.ID)
		}
	}
	path := filepath.Join(root, filepath.FromSlash(record.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", record.Path, err)
	}
	doc := frontmatter.Split(string(data))
	metadata := record.Metadata
	metadata.Status = StatusActive
	metadata.ObservedAt = now.Format("2006-01-02")
	if reviewer != "" {
		metadata.ReviewedBy = reviewer
	}
	rendered, err := frontmatter.Render(metadata, doc.Body)
	if err != nil {
		return fmt.Errorf("%s: %w", record.Path, err)
	}
	return fsx.WriteFileAtomic(path, rendered, fsx.DefaultFileMode)
}

// Supersede links a replacement decision to the one it replaces, updating both
// records so the chain reads correctly from either end.
func Supersede(root string, previous, replacement Record) error {
	previousMeta := previous.Metadata
	previousMeta.Status = StatusSuperseded
	previousMeta.SupersededBy = replacement.ID

	replacementMeta := replacement.Metadata
	if !contains(replacementMeta.Supersedes, previous.ID) {
		replacementMeta.Supersedes = append(replacementMeta.Supersedes, previous.ID)
	}
	if replacementMeta.Status == StatusProvisional {
		replacementMeta.Status = StatusActive
	}

	for _, update := range []struct {
		record   Record
		metadata Metadata
	}{{previous, previousMeta}, {replacement, replacementMeta}} {
		path := filepath.Join(root, filepath.FromSlash(update.record.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", update.record.Path, err)
		}
		doc := frontmatter.Split(string(data))
		rendered, err := frontmatter.Render(update.metadata, doc.Body)
		if err != nil {
			return fmt.Errorf("%s: %w", update.record.Path, err)
		}
		if err := fsx.WriteFileAtomic(path, rendered, fsx.DefaultFileMode); err != nil {
			return err
		}
	}
	return nil
}
