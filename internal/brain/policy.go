package brain

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/takealook97/vat/internal/fsx"
)

// The lifecycle policy a brain carries for itself, for the case where no
// workspace manifest stands over it.
//
// A brain used to be reachable only through vat.yaml, so a service that wanted
// one directory of records had to generate a manifest naming a single
// repository it never synchronised. The marker already sits in every brain;
// letting it hold the four values the manifest would have held is what makes
// the brain usable on its own. The values are plain strings and numbers so
// this package still imports nothing about workspaces.

// MarkerPolicy is the policy block of a marker, with every value resolved.
type MarkerPolicy struct {
	PromotionGate        string `json:"promotion_gate"`
	StaleAfterDays       int    `json:"stale_after_days"`
	ReviewSLADays        int    `json:"review_sla_days"`
	RequirePromotionGate bool   `json:"require_promotion_gate"`
}

// Policy keys as they appear in the marker.
const (
	PolicyPromotionGate        = "promotion_gate"
	PolicyStaleAfterDays       = "stale_after_days"
	PolicyReviewSLADays        = "review_sla_days"
	PolicyRequirePromotionGate = "require_promotion_gate"
)

// DefaultMarkerPolicy is what a brain with no workspace and no declared value
// runs under. It matches the manifest's defaults except for the promotion
// gate requirement, which is on: a brain nobody governs from outside is the
// one place an agent could otherwise write canonical records unreviewed.
func DefaultMarkerPolicy() MarkerPolicy {
	return MarkerPolicy{
		PromotionGate:        PromotionGateManual,
		StaleAfterDays:       90,
		ReviewSLADays:        30,
		RequirePromotionGate: true,
	}
}

// CheckPolicy returns the lifecycle clock the check and sweep rules read.
func (p MarkerPolicy) CheckPolicy() CheckPolicy {
	return CheckPolicy{
		PromotionGate:  p.PromotionGate,
		StaleAfterDays: p.StaleAfterDays,
		ReviewSLADays:  p.ReviewSLADays,
	}
}

// DeclaredPolicy is what a marker states, key by key. A key absent from the
// marker is absent from the map, so a caller comparing it against another
// source can tell a value somebody wrote from a default nobody chose.
type DeclaredPolicy map[string]any

// Keys returns the declared keys in a stable order.
func (d DeclaredPolicy) Keys() []string {
	keys := make([]string, 0, len(d))
	for key := range d {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Resolve fills every key the marker did not declare from the defaults.
func (d DeclaredPolicy) Resolve() MarkerPolicy {
	out := DefaultMarkerPolicy()
	if gate, ok := d[PolicyPromotionGate].(string); ok {
		out.PromotionGate = gate
	}
	if days, ok := d[PolicyStaleAfterDays].(int); ok {
		out.StaleAfterDays = days
	}
	if days, ok := d[PolicyReviewSLADays].(int); ok {
		out.ReviewSLADays = days
	}
	if required, ok := d[PolicyRequirePromotionGate].(bool); ok {
		out.RequirePromotionGate = required
	}
	return out
}

// ReadMarkerPolicy returns the policy the marker under root declares. A
// missing marker, or one with no policy block, declares nothing and is not an
// error.
//
// This is a YAML reading of the whole marker, unlike DeclaredSchema, which
// stays line-based on purpose: the schema line must stay readable in a marker
// some other tool wrote, while a policy is only ever read when somebody chose
// to write one, and a value that cannot be read must be refused rather than
// silently replaced by a default.
func ReadMarkerPolicy(root string) (DeclaredPolicy, error) {
	path := filepath.Join(root, MarkerFile)
	data, found, err := fsx.ReadFileIfExists(path)
	if err != nil {
		return nil, err
	}
	declared := DeclaredPolicy{}
	if !found || strings.TrimSpace(string(data)) == "" {
		return declared, nil
	}
	var document struct {
		Policy map[string]yaml.Node `yaml:"policy"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%s: the marker is not valid YAML: %w", path, err)
	}
	var problems []string
	for _, key := range sortedNodeKeys(document.Policy) {
		node := document.Policy[key]
		value, problem := policyValue(key, node)
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		declared[key] = value
	}
	// Every problem at once: a marker is fixed by hand, and one complaint per
	// run turns three typos into three round trips.
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s: %s", path, strings.Join(problems, "; "))
	}
	return declared, nil
}

func sortedNodeKeys(nodes map[string]yaml.Node) []string {
	keys := make([]string, 0, len(nodes))
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func policyValue(key string, node yaml.Node) (any, string) {
	switch key {
	case PolicyPromotionGate:
		var gate string
		if node.Kind != yaml.ScalarNode || node.Decode(&gate) != nil {
			return nil, fmt.Sprintf("policy.%s must be manual, auto, or agent", key)
		}
		switch gate {
		case PromotionGateManual, PromotionGateAuto, PromotionGateAgent:
			return gate, ""
		}
		return nil, fmt.Sprintf("policy.%s is %q; want manual, auto, or agent", key, gate)
	case PolicyStaleAfterDays, PolicyReviewSLADays:
		var days int
		if node.Kind != yaml.ScalarNode || node.Tag != "!!int" || node.Decode(&days) != nil || days < 1 {
			return nil, fmt.Sprintf("policy.%s must be a whole number of days, 1 or more", key)
		}
		return days, ""
	case PolicyRequirePromotionGate:
		var required bool
		if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" || node.Decode(&required) != nil {
			return nil, fmt.Sprintf("policy.%s must be true or false", key)
		}
		return required, ""
	}
	// An unknown key is refused for the reason the manifest refuses one: a
	// typo in a policy key would otherwise disable the rule it was meant to
	// configure, silently.
	return nil, fmt.Sprintf("policy.%s is not a policy key", key)
}

// PolicyKeys names every key a marker policy block may hold.
func PolicyKeys() []string {
	return []string{PolicyPromotionGate, PolicyStaleAfterDays, PolicyReviewSLADays, PolicyRequirePromotionGate}
}

// markerPolicyBlock renders a policy block for a marker.
func markerPolicyBlock(p MarkerPolicy) string {
	return "policy:\n" +
		fmt.Sprintf("  %s: %s\n", PolicyPromotionGate, p.PromotionGate) +
		fmt.Sprintf("  %s: %d\n", PolicyStaleAfterDays, p.StaleAfterDays) +
		fmt.Sprintf("  %s: %d\n", PolicyReviewSLADays, p.ReviewSLADays) +
		fmt.Sprintf("  %s: %t\n", PolicyRequirePromotionGate, p.RequirePromotionGate)
}
