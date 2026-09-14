package cli

import (
	"context"
	"sort"
	"time"

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
func driftedClaims(ctx context.Context, ws *workspace.Workspace, now time.Time) (map[string]string, error) {
	report, err := lint.Run(ctx, ws, lint.Options{
		Now: now, Only: []string{"brain/source-revision-drift"},
	})
	if err != nil {
		return nil, err
	}
	reasons := make(map[string]string, len(report.Findings))
	for _, finding := range report.Findings {
		if finding.Subject == "" {
			continue
		}
		reasons[finding.Subject] = finding.Message
	}
	return reasons, nil
}

// sortedKeys returns the identifiers in a stable order, so two runs over an
// unchanged workspace report the same thing in the same sequence.
func sortedKeys(reasons map[string]string) []string {
	ids := make([]string, 0, len(reasons))
	for id := range reasons {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
