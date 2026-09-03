package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Severity ranks a finding.
type Severity string

const (
	// SeverityError makes the check fail.
	SeverityError Severity = "error"
	// SeverityWarn reports something a human should look at.
	SeverityWarn Severity = "warn"
)

// ruleClaimSource is named because it is reported from two places and listed in
// two more, and a rule name that drifts between them is a rule `--only` cannot
// select.
const ruleClaimSource = "brain/claim-source"

// Finding is one rule violation.
type Finding struct {
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path,omitempty"`
	ID       string   `json:"id,omitempty"`
	Message  string   `json:"message"`
	Fixable  bool     `json:"fixable,omitempty"`
}

// CheckPolicy configures the lifecycle rules.
type CheckPolicy struct {
	// StaleAfterDays is when an observed claim stops counting as verified.
	StaleAfterDays int
	// ReviewSLADays is how long a record may stay in a non-answerable state
	// before the queue itself is reported as failing.
	ReviewSLADays int
	// Only narrows the report to rules whose name contains one of these. It is
	// for working through one class of problem at a time; an empty list checks
	// everything.
	Only []string
}

// maxRecordWords is where a record stops being one judgement and starts being a
// directory nobody has made yet.
//
// It is set from measurement rather than taste. In the largest workspace
// available when this rule was written the median record was 189 words and the
// ninety-ninth percentile 938, while the longest was 4723 — twenty-five times
// the median. A limit at the ninety-ninth percentile would report the merely
// thorough record; this one reports the record that is visibly a different kind
// of object.
//
// It is a constant and not a policy field. The finding is a warning, which does
// not fail `vat brain check`, so a workspace that disagrees with the number
// loses nothing but a line of output — and a knob on `vat.yaml` is a published
// contract that can never be withdrawn. If a real workspace needs a different
// limit, that is the evidence for adding one; until then the setting would only
// be a place to hide from the rule.
const maxRecordWords = 1500

// selected reports whether a rule survives the caller's --only filter.
func selected(rule string, only []string) bool {
	if len(only) == 0 {
		return true
	}
	for _, want := range only {
		if want != "" && strings.Contains(rule, want) {
			return true
		}
	}
	return false
}

// RuleNames lists every rule Check can report.
//
// The same reason lint keeps one: a rule that is not listed cannot be selected
// with --only and cannot be documented, so it is a rule nobody knows to look
// for. Half of this package's rules had gone undocumented since the first
// release for exactly that reason — nothing held the list and nothing checked
// it against what the code reports.
func RuleNames() []string {
	return []string{
		"brain/claim-observed",
		"brain/claim-owner",
		ruleClaimSource,
		"brain/claim-source-branch",
		"brain/claim-stale",
		"brain/date-unreadable",
		"brain/id-duplicate",
		"brain/id-missing",
		"brain/link-broken",
		"brain/quarantine-reason",
		"brain/record-malformed",
		"brain/record-oversized",
		"brain/record-secret-suspected",
		"brain/ref-missing",
		"brain/ref-withdrawn",
		"brain/review-overdue",
		"brain/revoke-reason",
		"brain/schema-newer",
		"brain/status-unknown",
		"brain/supersede-cycle",
		"brain/superseded-asymmetric",
		"brain/superseded-missing",
		"brain/superseded-orphan",
		"brain/superseded-status",
		"brain/supersedes-asymmetric",
		"brain/supersedes-missing",
		"brain/terminal-unarchived",
		"brain/title-missing",
	}
}

// Check validates the whole knowledge surface and returns every finding at
// once. Returning the full list rather than the first error is deliberate:
// these rules are run in a loop while cleaning up a repository, and one finding
// per run makes that loop unbearable.
func Check(store *Store, policy CheckPolicy, now time.Time) []Finding {
	findings := make([]Finding, 0, len(store.Records))
	index := store.ByID()

	findings = append(findings, checkSchema(store)...)
	findings = append(findings, checkMalformed(store)...)
	findings = append(findings, checkSecrets(store)...)
	findings = append(findings, checkIdentity(store)...)
	findings = append(findings, checkStatuses(store)...)
	findings = append(findings, checkArchiveHygiene(store)...)
	findings = append(findings, checkRecordSize(store)...)
	findings = append(findings, checkProvenance(store, policy, now)...)
	findings = append(findings, checkSupersession(store, index)...)
	findings = append(findings, checkReferences(store, index)...)
	findings = append(findings, checkLinks(store)...)
	findings = append(findings, checkReviewQueue(store, policy, now)...)

	if len(policy.Only) > 0 {
		kept := findings[:0]
		for _, finding := range findings {
			if selected(finding.Rule, policy.Only) {
				kept = append(kept, finding)
			}
		}
		findings = kept
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity == SeverityError
		}
		if findings[i].Rule != findings[j].Rule {
			return findings[i].Rule < findings[j].Rule
		}
		return findings[i].Path < findings[j].Path
	})
	return findings
}

// Errors counts the findings that should fail a build.
func Errors(findings []Finding) int {
	count := 0
	for _, finding := range findings {
		if finding.Severity == SeverityError {
			count++
		}
	}
	return count
}

// checkMalformed reports the files that could not be read as records at all.
//
// The severity is error rather than warning because an unreadable record is
// invisible to every other rule here: its identifier is not checked for
// duplication, its claims are not checked for provenance, and its links are
// checked by nobody. A layer that quietly holds files it cannot read is a layer
// whose own report is incomplete.
func checkMalformed(store *Store) []Finding {
	findings := make([]Finding, 0, len(store.Malformed))
	for _, broken := range store.Malformed {
		findings = append(findings, Finding{
			Rule: "brain/record-malformed", Severity: SeverityError, Path: broken.Path,
			Message: fmt.Sprintf("could not be read as a record: %s", broken.Problem),
		})
	}
	return findings
}

func checkIdentity(store *Store) []Finding {
	var findings []Finding
	seen := map[string]string{}
	for _, record := range store.Records {
		if record.ID == "" {
			findings = append(findings, Finding{
				Rule: "brain/id-missing", Severity: SeverityError, Path: record.Path,
				Message: "record has no id in its front matter",
			})
			continue
		}
		if previous, exists := seen[record.ID]; exists {
			findings = append(findings, Finding{
				Rule: "brain/id-duplicate", Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: fmt.Sprintf("id %s is already used by %s", record.ID, previous),
			})
			continue
		}
		seen[record.ID] = record.Path
		if record.Title == "" || record.Title == record.ID {
			findings = append(findings, Finding{
				Rule: "brain/title-missing", Severity: SeverityWarn, Path: record.Path, ID: record.ID,
				Message: "record has no heading; the index will show only its id",
			})
		}
	}
	return findings
}

func checkStatuses(store *Store) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		if !record.Status.Valid() {
			findings = append(findings, Finding{
				Rule: "brain/status-unknown", Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: fmt.Sprintf("unknown status %q", record.Status),
			})
		}
		// A withdrawal with no stated cause cannot be reviewed, reversed, or
		// learned from. Requiring the reason is what makes the tombstone
		// useful rather than merely present.
		if record.Status == StatusQuarantined && strings.TrimSpace(record.Reason) == "" {
			findings = append(findings, Finding{
				Rule: "brain/quarantine-reason", Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: "quarantined records must state a reason",
			})
		}
		if record.Status == StatusRevoked && strings.TrimSpace(record.Reason) == "" {
			findings = append(findings, Finding{
				Rule: "brain/revoke-reason", Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: "revoked records must state a reason",
			})
		}
	}
	return findings
}

// checkArchiveHygiene reports the records that have reached an end state and
// are still sitting in the working directories.
//
// `vat brain archive` has always been able to find these, but only for somebody
// who thought to run it, so they accumulate unseen: the workspace this rule was
// written against held forty-nine terminal records and one archived one. The
// finding is a warning rather than an error because an unarchived record makes
// the entry point larger, not the layer wrong — and because every workspace
// that upgrades into this rule would otherwise fail its next build over a
// condition that was never previously reported.
func checkArchiveHygiene(store *Store) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		if !record.Status.Terminal() || record.Archived {
			continue
		}
		findings = append(findings, Finding{
			Rule: "brain/terminal-unarchived", Severity: SeverityWarn,
			Path: record.Path, ID: record.ID, Fixable: true,
			Message: fmt.Sprintf(
				"%s but still in the working set; `vat brain archive --apply` moves it", record.Status),
		})
	}
	return findings
}

// checkRecordSize reports a record that has outgrown the single judgement it is
// meant to hold.
//
// The cost is not storage. Query ranking discounts a document for length — the
// length normalisation in this package's own scorer — so the record that has
// swallowed five decisions ranks below any one of them written separately. The
// sprawling record is therefore both the one most in need of splitting and the
// one hardest to find, and nothing said so until now.
//
// Splitting is never automatic: which paragraph belongs to which record is a
// judgement about content, and this tool does not make those.
func checkRecordSize(store *Store) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		// An archived record is out of the working set, and asking somebody to
		// split a decision that has already been replaced is asking them to
		// edit history for tidiness.
		if record.Archived {
			continue
		}
		// The same measure the ranking uses, because the argument for the rule
		// is that the ranking punishes exactly this.
		words := len(strings.Fields(record.Body))
		if words <= maxRecordWords {
			continue
		}
		findings = append(findings, Finding{
			Rule: "brain/record-oversized", Severity: SeverityWarn,
			Path: record.Path, ID: record.ID,
			Message: fmt.Sprintf(
				"%d words against a limit of %d; a record this long holds more than one judgement and ranks below the records it should have been split into",
				words, maxRecordWords),
		})
	}
	return findings
}

func checkProvenance(store *Store, policy CheckPolicy, now time.Time) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		if !record.IsCurrentStateClaim() {
			continue
		}
		if record.Status.Terminal() {
			continue
		}
		if strings.TrimSpace(record.OwnedBy) == "" {
			findings = append(findings, Finding{
				Rule: "brain/claim-owner", Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: "a current-state claim must name the repository that owns the fact",
			})
		}
		if strings.TrimSpace(record.SourceRef) == "" {
			findings = append(findings, Finding{
				Rule: ruleClaimSource, Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: "a current-state claim must record source_ref as <repo>@<revision>[:<path>]",
			})
		} else if _, revision, _, ok := record.SourceParts(); !ok {
			findings = append(findings, Finding{
				Rule: ruleClaimSource, Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: fmt.Sprintf("source_ref %q is not <repo>@<revision>[:<path>]", record.SourceRef),
			})
		} else if isBranchName(revision) {
			// A branch keeps moving, so a claim pinned to one silently changes
			// what it was evidence for.
			findings = append(findings, Finding{
				Rule: "brain/claim-source-branch", Severity: SeverityWarn, Path: record.Path, ID: record.ID,
				Message: fmt.Sprintf("source_ref points at branch %q; pin an exact revision", revision),
			})
		}
		if _, ok := record.ObservedDate(); !ok {
			findings = append(findings, Finding{
				Rule: "brain/claim-observed", Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: "a current-state claim must record observed_at as YYYY-MM-DD",
			})
			continue
		}
		if record.Status == StatusActive && policy.StaleAfterDays > 0 {
			if age, ok := record.AgeDays(now); ok && age > policy.StaleAfterDays {
				findings = append(findings, Finding{
					Rule: "brain/claim-stale", Severity: SeverityWarn, Path: record.Path, ID: record.ID,
					Fixable: true,
					Message: fmt.Sprintf("observed %d days ago, past the %d-day window; run `vat brain sweep --apply`",
						age, policy.StaleAfterDays),
				})
			}
		}
	}
	return findings
}

var branchLike = regexp.MustCompile(`^(main|master|develop|trunk|HEAD|release/.*)$`)

func isBranchName(revision string) bool {
	if branchLike.MatchString(revision) {
		return true
	}
	// A full or abbreviated hash is hexadecimal and at least seven characters.
	if len(revision) < 7 {
		return true
	}
	for _, r := range revision {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
			return true
		}
	}
	return false
}

func checkSupersession(store *Store, index map[string]Record) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		if record.Status == StatusSuperseded && strings.TrimSpace(record.SupersededBy) == "" {
			findings = append(findings, Finding{
				Rule: "brain/superseded-orphan", Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: "status is superseded but superseded_by names nothing",
			})
		}
		if record.SupersededBy != "" {
			successor, exists := index[record.SupersededBy]
			if !exists {
				findings = append(findings, Finding{
					Rule: "brain/superseded-missing", Severity: SeverityError, Path: record.Path, ID: record.ID,
					Message: fmt.Sprintf("superseded_by %s does not exist", record.SupersededBy),
				})
			} else {
				if record.Status != StatusSuperseded {
					findings = append(findings, Finding{
						Rule: "brain/superseded-status", Severity: SeverityError, Path: record.Path, ID: record.ID,
						Message: fmt.Sprintf("superseded_by is set but status is %q, not superseded", record.Status),
					})
				}
				// The replacement chain has to be navigable from both ends, or
				// a reader who finds the new decision cannot tell what it
				// replaced.
				if !contains(successor.Supersedes, record.ID) {
					findings = append(findings, Finding{
						Rule: "brain/superseded-asymmetric", Severity: SeverityError,
						Path: successor.Path, ID: successor.ID,
						Message: fmt.Sprintf("%s claims to be superseded by %s, which does not list it in supersedes",
							record.ID, successor.ID),
					})
				}
			}
		}
		for _, predecessor := range record.Supersedes {
			previous, exists := index[predecessor]
			if !exists {
				findings = append(findings, Finding{
					Rule: "brain/supersedes-missing", Severity: SeverityError, Path: record.Path, ID: record.ID,
					Message: fmt.Sprintf("supersedes %s, which does not exist", predecessor),
				})
				continue
			}
			if previous.SupersededBy != record.ID {
				findings = append(findings, Finding{
					Rule: "brain/supersedes-asymmetric", Severity: SeverityError,
					Path: previous.Path, ID: previous.ID,
					Message: fmt.Sprintf("%s supersedes it, but its superseded_by is %q",
						record.ID, previous.SupersededBy),
				})
			}
		}
	}
	findings = append(findings, checkSupersessionCycles(store, index)...)
	return findings
}

func checkSupersessionCycles(store *Store, index map[string]Record) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		seen := map[string]bool{record.ID: true}
		cursor := record
		for cursor.SupersededBy != "" {
			next, exists := index[cursor.SupersededBy]
			if !exists {
				break
			}
			if seen[next.ID] {
				findings = append(findings, Finding{
					Rule: "brain/supersede-cycle", Severity: SeverityError, Path: record.Path, ID: record.ID,
					Message: fmt.Sprintf("supersession chain loops back to %s", next.ID),
				})
				break
			}
			seen[next.ID] = true
			cursor = next
		}
	}
	return findings
}

func checkReferences(store *Store, index map[string]Record) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		for _, ref := range record.Refs {
			target, exists := index[ref]
			if !exists {
				findings = append(findings, Finding{
					Rule: "brain/ref-missing", Severity: SeverityError, Path: record.Path, ID: record.ID,
					Message: fmt.Sprintf("refs %s, which does not exist", ref),
				})
				continue
			}
			// Citing a withdrawn claim as support is exactly the failure the
			// tombstone exists to catch.
			if target.Status == StatusRevoked || target.Status == StatusQuarantined {
				findings = append(findings, Finding{
					Rule: "brain/ref-withdrawn", Severity: SeverityWarn, Path: record.Path, ID: record.ID,
					Message: fmt.Sprintf("refs %s, which is %s", ref, target.Status),
				})
			}
		}
	}
	return findings
}

var markdownLink = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

func checkLinks(store *Store) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		for _, match := range markdownLink.FindAllStringSubmatch(record.Body, -1) {
			target := strings.TrimSpace(match[1])
			if target == "" || strings.Contains(target, "://") ||
				strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if hash := strings.Index(target, "#"); hash >= 0 {
				target = target[:hash]
			}
			if target == "" {
				continue
			}
			resolved := filepath.Join(store.Root, filepath.Dir(record.Path), filepath.FromSlash(target))
			if _, err := os.Stat(resolved); err != nil {
				findings = append(findings, Finding{
					Rule: "brain/link-broken", Severity: SeverityError, Path: record.Path, ID: record.ID,
					Message: fmt.Sprintf("link to %s does not resolve", target),
				})
			}
		}
	}
	return findings
}

func checkReviewQueue(store *Store, policy CheckPolicy, now time.Time) []Finding {
	if policy.ReviewSLADays <= 0 {
		return nil
	}
	var findings []Finding
	for _, record := range store.Records {
		switch record.Status {
		case StatusProvisional, StatusStale, StatusQuarantined:
		default:
			continue
		}
		age, ok := record.AgeDays(now)
		if !ok {
			// Both staleness rules ask the record how old it is and skip it
			// when it cannot say, so an unreadable date exempted a record from
			// brain/claim-stale and brain/review-overdue at once — the two
			// rules that stop this layer filling with statements nobody has
			// re-checked. An honest old record was reported and the unreadable
			// one was silent, which is the wrong way round.
			//
			// brain/claim-observed already reports this for a current-state
			// claim, and says something more specific, so it keeps that case.
			date := firstNonEmpty(record.ObservedAt, record.Date)
			if !record.IsCurrentStateClaim() && date != "" {
				findings = append(findings, Finding{
					Rule: "brain/date-unreadable", Severity: SeverityWarn, Path: record.Path, ID: record.ID,
					Message: fmt.Sprintf("date %q cannot be read, so no staleness rule can judge this record; write it as YYYY-MM-DD",
						date),
				})
			}
			continue
		}
		if age <= policy.ReviewSLADays {
			continue
		}
		// A review queue nobody drains becomes the very thing this layer was
		// built to prevent: a repository full of statements no one trusts.
		findings = append(findings, Finding{
			Rule: "brain/review-overdue", Severity: SeverityWarn, Path: record.Path, ID: record.ID,
			Message: fmt.Sprintf("%s for %d days, past the %d-day review window",
				record.Status, age, policy.ReviewSLADays),
		})
	}
	return findings
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// checkSchema reports a brain written against a contract this build does not
// know.
//
// The knowledge layer is meant to outlive the tool that wrote it, which means
// an older vat will eventually be pointed at a newer brain. Reading it silently
// and reporting on fields it cannot see would be the worst outcome: the records
// would look clean because half of what governs them was invisible.
func checkSchema(store *Store) []Finding {
	declared, ok := DeclaredSchema(store.Root)
	if !ok {
		return nil
	}
	if declared == 0 {
		return []Finding{{
			Rule: "brain/schema-newer", Severity: SeverityError, Path: MarkerFile,
			Message: "declares a schema version this build cannot parse, so it cannot tell whether these checks apply",
		}}
	}
	if declared <= SchemaVersion {
		return nil
	}
	return []Finding{{
		Rule: "brain/schema-newer", Severity: SeverityError, Path: MarkerFile,
		Message: fmt.Sprintf(
			"written against schema %d; this build understands %d, so anything newer is invisible to these checks",
			declared, SchemaVersion),
	}}
}

// firstNonEmpty returns the first value with content, so a finding quotes the
// field the reader has to go and fix rather than an empty string.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
