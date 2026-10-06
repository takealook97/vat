package cli

import (
	"context"
	"sort"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/workspace"
)

// driftedClaims reports, by record identifier, why each active claim's evidence
// is no longer known to hold.
//
// It asks the lint rule rather than re-deriving the answer. Two commands that
// each decide for themselves whether evidence moved eventually disagree, and a
// workspace where `vat lint` and `vat brain review` say different things about
// the same record is worse than one where only one of them speaks.
//
// The knowledge layer cannot answer this itself: `internal/brain` imports
// neither the manifest nor git, deliberately, so a workspace that never adopts
// it pays nothing for it. Resolving a revision lives out here.
func driftedClaims(ctx context.Context, ws *workspace.Workspace, now time.Time) (map[string]brain.DriftClaim, error) {
	report, err := lint.Run(ctx, ws, lint.Options{
		Now: now, Only: []string{lint.RuleSourceRevisionDrift},
	})
	if err != nil {
		return nil, err
	}
	reasons := make(map[string]brain.DriftClaim, len(report.Findings))
	for _, finding := range report.Findings {
		if finding.Subject == "" || finding.SourceDrift == nil {
			continue
		}
		evidence := finding.SourceDrift
		reasons[finding.Subject] = brain.DriftClaim{
			Why: finding.Message,
			Evidence: brain.DriftEvidence{
				Repo: evidence.Repo, PinnedRevision: evidence.PinnedRevision,
				SourcePath: evidence.SourcePath, HeadRevision: evidence.HeadRevision,
				PinUnresolvable: evidence.PinUnresolvable,
			},
		}
	}
	return reasons, nil
}

// sortedKeys returns the identifiers in a stable order, so two runs over an
// unchanged workspace report the same thing in the same sequence.
func sortedKeys(reasons map[string]brain.DriftClaim) []string {
	ids := make([]string, 0, len(reasons))
	for id := range reasons {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
