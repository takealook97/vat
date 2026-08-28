package lint

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/gitx"
)

// The maintained views are the documents the generated index recommends —
// STATUS.md, ROADMAP.md, DECISIONS.md and the rest. vat generates neither their
// content nor their judgements, so no schema rule reaches them, and a knowledge
// repository could report zero findings while every document its own entry point
// routes to had gone months without being revisited. The records had moved; the
// synthesis of them had not.
//
// What is checkable is exactly that: whether the records moved on and the view
// did not. Whether the view is *correct* is a judgement, and vat does not make
// judgements about content.

// ruleViewStale names the rule reported below.
const ruleViewStale = "brain/view-stale"

// checkViews reports maintained views the records have left behind.
//
// The window is the review SLA, not zero. Every record change makes every view
// technically older, and a rule that fires on every commit is one people
// silence rather than act on. A month of records with no revisit is a different
// claim, and one somebody can do something about.
// The comparison is between two commit dates, never against the wall clock: a
// checkout made today would otherwise report every view in the repository as
// months behind on the day it was cloned.
func checkViews(ctx context.Context, root string, slaDays int) []Finding {
	if slaDays <= 0 || !gitx.IsRepository(root) {
		return nil
	}
	recordsChanged, ok := lastCommit(ctx, root, brain.RecordDirs())
	if !ok {
		return nil
	}
	window := time.Duration(slaDays) * 24 * time.Hour

	var findings []Finding
	for _, view := range brain.CanonicalViews(root) {
		if brain.IsGeneratedProjection(view) {
			// Generated files are checked for drift, which is a stronger claim
			// than staleness and already reported.
			continue
		}
		viewChanged, tracked := lastCommit(ctx, root, []string{view})
		if !tracked {
			// Never committed. That is either a file being written right now or
			// one nobody tracks, and neither is something git can date.
			continue
		}
		behind := recordsChanged.Sub(viewChanged)
		if behind <= window {
			continue
		}
		findings = append(findings, Finding{
			Rule: ruleViewStale, Severity: SeverityWarn, Subject: view,
			Message: fmt.Sprintf(
				"the records moved on %d days after this view was last revisited, and the index routes readers here",
				int(behind.Hours()/24)),
			Fix: "revisit it against the records, or delete it if it is no longer maintained",
		})
	}
	return findings
}

// lastCommit returns when git last recorded a change to any of the given paths.
func lastCommit(ctx context.Context, dir string, paths []string) (time.Time, bool) {
	args := append([]string{"log", "-1", "--format=%cI", "--"}, paths...)
	out, err := gitx.Run(ctx, dir, args...)
	if err != nil || strings.TrimSpace(out) == "" {
		return time.Time{}, false
	}
	stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(out))
	if err != nil {
		return time.Time{}, false
	}
	return stamp, true
}
