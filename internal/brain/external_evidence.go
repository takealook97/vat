package brain

import (
	"fmt"
	"net/url"
	"strings"
)

// Evidence for claims about systems this workspace does not govern.
//
// source_ref can only name a repository at a revision, so a claim read from a
// chat thread, a vendor page, or a ticket had nowhere to say where it came
// from: source_external marked it as outside, and that was all. These two
// fields hold the address and the words, and two rules check them.
//
// Neither rule opens the address. Whether it resolves, and whether the quote
// is really on the page, depend on the network and on a third party; a check
// that answered differently on a train is not a check. What vat can judge is
// the shape.

const (
	ruleExternalSourceURL       = "brain/external-source-url"
	ruleExternalEvidenceMissing = "brain/external-evidence-missing"
)

// ValidateSourceURL reports whether value is an address a reader can follow:
// an absolute http or https URL naming a host and carrying no user
// information.
//
// User information is refused rather than tolerated because it is where a
// credential sits in a URL, and a record is read and indexed widely. The error
// never repeats the address for the same reason.
func ValidateSourceURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("source_url is not a URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("source_url must be an absolute http or https URL")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("source_url names no host")
	}
	if parsed.User != nil {
		return fmt.Errorf("source_url carries user information, which is where a credential sits; remove it")
	}
	return nil
}

// checkExternalEvidence reports bad addresses on every record, and missing
// evidence on external records when the policy asks for it.
//
// Archived records are included. Leaving the working set does not make a claim
// any better sourced, and a publishing job that turns the policy on is told to
// migrate its existing records first; a rule that skipped the archive would let
// that migration stop half way and pass.
//
// An address on a record that is not external is still judged: a malformed
// address is wrong wherever it is written. It changes nothing about the
// record's lifecycle.
func checkExternalEvidence(store *Store, policy CheckPolicy) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		if strings.TrimSpace(record.SourceURL) != "" {
			if err := ValidateSourceURL(record.SourceURL); err != nil {
				findings = append(findings, Finding{
					Rule: ruleExternalSourceURL, Severity: SeverityError, Path: record.Path, ID: record.ID,
					Message: err.Error(),
				})
			}
		}
		if !policy.RequireExternalEvidence || !record.SourceExternal {
			continue
		}
		var missing []string
		if strings.TrimSpace(record.SourceURL) == "" {
			missing = append(missing, "source_url")
		}
		if strings.TrimSpace(record.SourceQuote) == "" {
			missing = append(missing, "source_quote")
		}
		if len(missing) > 0 {
			findings = append(findings, Finding{
				Rule: ruleExternalEvidenceMissing, Severity: SeverityError, Path: record.Path, ID: record.ID,
				Message: fmt.Sprintf("an external record has no %s; policy.brain.require_external_evidence asks for the address and the quoted words",
					strings.Join(missing, " or ")),
			})
		}
	}
	return findings
}
