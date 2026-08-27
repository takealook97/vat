// Package metrics answers a question the methodology cannot answer about
// itself: is any of this working?
//
// A checklist measures whether people performed rituals. These numbers measure
// whether the rituals produced the effect they were for. If the review queue
// grows every month, the knowledge layer is decaying no matter how diligently
// records are written; if lint findings never reach zero, the rules are being
// ignored rather than followed.
package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/workspace"
)

// Snapshot is one measurement of the workspace.
type Snapshot struct {
	At string `json:"at"`

	Repositories int `json:"repositories"`
	Cloned       int `json:"cloned"`

	// LintErrors and LintWarnings measure how far the workspace is from its
	// own declared rules.
	LintErrors   int `json:"lint_errors"`
	LintWarnings int `json:"lint_warnings"`

	// BrainRecords and BrainCitable measure how much of the knowledge layer is
	// actually usable as evidence rather than merely present.
	BrainRecords int `json:"brain_records"`
	BrainCitable int `json:"brain_citable"`
	// ReviewQueue and ReviewOverdue are the leading indicator of decay: a queue
	// that only grows means claims are being written faster than they are
	// verified.
	ReviewQueue   int `json:"review_queue"`
	ReviewOverdue int `json:"review_overdue"`
	// MedianClaimAgeDays is how old the typical current-state claim's evidence
	// is. Rising means the repository is drifting away from reality.
	//
	// ClaimsMeasured is how many claims it was taken over. A median of nothing
	// is not zero, and "0 days since the typical claim was verified" is the
	// most flattering possible reading of a workspace that has verified none.
	MedianClaimAgeDays int `json:"median_claim_age_days"`
	ClaimsMeasured     int `json:"claims_measured"`

	// ChangesetsOpen and ChangesetsStale measure unfinished cross-repository
	// work — the cost multi-repo layouts pay and usually never count.
	ChangesetsOpen  int `json:"changesets_open"`
	ChangesetsStale int `json:"changesets_stale"`
	// ReworkRate is the share of recorded checks that failed: how often work
	// reported as done did not survive verification.
	//
	// ChecksRecorded is the denominator. Without it, no check having ever run
	// and every check having passed are the same number.
	ReworkRate     float64 `json:"rework_rate"`
	ChecksRecorded int     `json:"checks_recorded"`
}

// noReading is what a measurement with an empty population prints. It is the
// same dash the delta column uses for "nothing to compare against", because it
// says the same thing: this is not a number yet.
const noReading = "—"

// Ledger is the append-only local history of snapshots. It lives in derived
// state, never in the manifest, because it is regenerable and uninteresting to
// version.
const Ledger = "metrics.jsonl"

// Collect measures the workspace now.
func Collect(ctx context.Context, ws *workspace.Workspace, now time.Time) (Snapshot, error) {
	snapshot := Snapshot{At: now.Format(time.RFC3339)}

	for _, repo := range ws.Manifest.Active() {
		snapshot.Repositories++
		if ws.Exists(repo) {
			snapshot.Cloned++
		}
	}

	report, err := lint.Run(ctx, ws, lint.Options{Now: now, Offline: true})
	if err != nil {
		return snapshot, err
	}
	for _, finding := range report.Findings {
		if finding.Severity == lint.SeverityError {
			snapshot.LintErrors++
			continue
		}
		snapshot.LintWarnings++
	}

	if root, ok := ws.BrainPath(); ok && fsx.IsDir(root) {
		store, err := brain.Load(root)
		if err != nil {
			return snapshot, err
		}
		policy := brain.CheckPolicy{
			StaleAfterDays: ws.Manifest.Policy.Brain.StaleAfterDays,
			ReviewSLADays:  ws.Manifest.Policy.Brain.ReviewSLADays,
		}
		snapshot.BrainRecords = len(store.WorkingSet())
		snapshot.BrainCitable = len(store.Answerable())
		for _, item := range brain.ReviewQueue(store, policy, now) {
			snapshot.ReviewQueue++
			if item.Overdue {
				snapshot.ReviewOverdue++
			}
		}
		snapshot.MedianClaimAgeDays, snapshot.ClaimsMeasured = medianClaimAge(store, now)
	}

	sets, err := changeset.LoadAll(ws.Root)
	if err != nil {
		return snapshot, err
	}
	totalChecks, failedChecks := 0, 0
	for _, set := range sets {
		if set.Status.Open() {
			snapshot.ChangesetsOpen++
			if limit := ws.Manifest.Policy.Changeset.MaxOpenDays; limit > 0 && set.AgeDays(now) > limit {
				snapshot.ChangesetsStale++
			}
		}
		for _, participant := range set.Repositories {
			for _, check := range participant.Checks {
				totalChecks++
				if !check.Passed() {
					failedChecks++
				}
			}
		}
	}
	snapshot.ChecksRecorded = totalChecks
	if totalChecks > 0 {
		snapshot.ReworkRate = float64(failedChecks) / float64(totalChecks)
	}
	return snapshot, nil
}

// medianClaimAge returns the median and the number of claims it was taken over,
// because a caller cannot tell an empty population from a population of zeroes.
func medianClaimAge(store *brain.Store, now time.Time) (median, measured int) {
	var ages []int
	for _, record := range store.CurrentStateClaims() {
		if age, ok := record.AgeDays(now); ok {
			ages = append(ages, age)
		}
	}
	if len(ages) == 0 {
		return 0, 0
	}
	for i := 1; i < len(ages); i++ {
		for j := i; j > 0 && ages[j] < ages[j-1]; j-- {
			ages[j], ages[j-1] = ages[j-1], ages[j]
		}
	}
	return ages[len(ages)/2], len(ages)
}

// Append records a snapshot in the local ledger so trends become visible. A
// single reading says little; the direction of travel says everything.
//
// The whole ledger is rewritten atomically rather than appended to. It is small
// — one line per run — and vat's contract is that no write can leave a
// half-finished file behind, including this one.
func Append(ws *workspace.Workspace, snapshot Snapshot) error {
	if err := fsx.EnsureDir(ws.StateDir()); err != nil {
		return err
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	path := filepath.Join(ws.StateDir(), Ledger)
	existing, _, err := fsx.ReadFileIfExists(path)
	if err != nil {
		return err
	}
	next := existing
	if len(next) > 0 && !strings.HasSuffix(string(next), "\n") {
		next = append(next, '\n')
	}
	next = append(next, encoded...)
	next = append(next, '\n')
	return fsx.WriteFileAtomic(path, next, fsx.DefaultFileMode)
}

// History reads previous snapshots, oldest first.
func History(ws *workspace.Workspace) ([]Snapshot, error) {
	data, exists, err := fsx.ReadFileIfExists(filepath.Join(ws.StateDir(), Ledger))
	if err != nil || !exists {
		return nil, err
	}
	var snapshots []Snapshot
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var snapshot Snapshot
		if err := json.Unmarshal([]byte(line), &snapshot); err != nil {
			// A corrupt line in a derived ledger is not worth failing over.
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// Trend describes how one measure moved since the previous snapshot.
type Trend struct {
	Name     string
	Current  string
	Delta    string
	Reading  string
	Improved bool
	Worsened bool
}

// Compare renders the current snapshot against the most recent earlier one.
func Compare(current Snapshot, history []Snapshot) []Trend {
	var previous *Snapshot
	if len(history) > 0 {
		last := history[len(history)-1]
		previous = &last
	}
	measures := []struct {
		name        string
		value       int
		lowerBetter bool
		reading     string
	}{
		{"lint errors", current.LintErrors, true,
			"rules the workspace declares but does not meet"},
		{"lint warnings", current.LintWarnings, true,
			"things to look at that are not yet failures"},
		{"review queue", current.ReviewQueue, true,
			"claims awaiting verification; sustained growth means knowledge is decaying"},
		{"review overdue", current.ReviewOverdue, true,
			"past the review window"},
		{"median claim age", current.MedianClaimAgeDays, true,
			"days since the typical current-state claim was verified"},
		{"citable records", current.BrainCitable, false,
			"records usable as evidence right now"},
		{"open changesets", current.ChangesetsOpen, true,
			"cross-repository work with no closing evidence"},
		{"stale changesets", current.ChangesetsStale, true,
			"open past the limit, so the revision bundle is drifting from what shipped"},
	}

	trends := make([]Trend, 0, len(measures)+1)
	for _, measure := range measures {
		trend := Trend{
			Name:    measure.name,
			Current: fmt.Sprintf("%d", measure.value),
			Reading: measure.reading,
			Delta:   "—",
		}
		// A median taken over no claims is not zero. Printed as a number it
		// reads as "every claim was verified today", which is the most
		// flattering thing that could be said about a workspace that has
		// verified nothing — and a trend against it is arithmetic on nothing.
		if measure.name == "median claim age" && current.ClaimsMeasured == 0 {
			trend.Current = noReading
			trends = append(trends, trend)
			continue
		}
		if previous != nil {
			before := previousValue(*previous, measure.name)
			delta := measure.value - before
			if delta != 0 {
				trend.Delta = fmt.Sprintf("%+d", delta)
				if measure.lowerBetter {
					trend.Improved = delta < 0
					trend.Worsened = delta > 0
				} else {
					trend.Improved = delta > 0
					trend.Worsened = delta < 0
				}
			}
		}
		trends = append(trends, trend)
	}
	// No check having run and every check having passed are the same number
	// without the denominator, and the second is the one a reader assumes.
	rework := noReading
	if current.ChecksRecorded > 0 {
		rework = fmt.Sprintf("%.0f%%", current.ReworkRate*100)
	}
	trends = append(trends, Trend{
		Name:    "rework rate",
		Current: rework,
		Delta:   "—",
		Reading: "share of recorded checks that failed",
	})
	return trends
}

func previousValue(snapshot Snapshot, name string) int {
	switch name {
	case "lint errors":
		return snapshot.LintErrors
	case "lint warnings":
		return snapshot.LintWarnings
	case "review queue":
		return snapshot.ReviewQueue
	case "review overdue":
		return snapshot.ReviewOverdue
	case "median claim age":
		return snapshot.MedianClaimAgeDays
	case "citable records":
		return snapshot.BrainCitable
	case "open changesets":
		return snapshot.ChangesetsOpen
	case "stale changesets":
		return snapshot.ChangesetsStale
	default:
		return 0
	}
}
