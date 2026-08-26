package brain

import (
	"fmt"
	"regexp"
	"strings"
)

// Credential detection for records.
//
// The methodology has always said a record holds no secret, and until now that
// was the only rule in this layer with nothing checking it — which rule 1 of
// this project says makes it not a rule at all. It matters more here than
// elsewhere: a brain repository is exactly the kind of thing an organisation
// points a search index at, so a token pasted into a record becomes a
// searchable token.
//
// Nothing here ever quotes what it matched. A finding about a credential that
// repeats the credential has published it a second time, in a place people
// paste into chat.

// credentialPattern is one recognisable shape of a secret.
type credentialPattern struct {
	name string
	// certain separates a shape that is only ever a credential from a
	// heuristic ordinary prose can trip. The first fails the check; the second
	// asks a human, because a rule that fires on ordinary writing is a rule
	// somebody switches off.
	certain bool
	match   *regexp.Regexp
}

var credentialPatterns = []credentialPattern{
	{name: "a PEM private key block", certain: true,
		match: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{name: "an AWS access key id", certain: true,
		match: regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{name: "a GitHub token", certain: true,
		match: regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`)},
	{name: "a Slack token", certain: true,
		match: regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{12,}\b`)},
	{name: "a Google API key", certain: true,
		match: regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{name: "an OpenAI-style key", certain: true,
		match: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{32,}\b`)},
	{name: "a JSON Web Token", certain: true,
		match: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{name: "credentials inside a URL", certain: true,
		match: regexp.MustCompile(`://[^/\s:@]+:[^/\s@]{3,}@`)},
	{name: "an assigned secret", certain: false,
		match: regexp.MustCompile(`(?i)\b(?:password|passwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret)\b\s*[:=]\s*["']?[A-Za-z0-9/+_=-]{16,}`)},
	{name: "a bearer token", certain: false,
		match: regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*["']?bearer\s+\S{16,}`)},
}

// checkSecrets reports records that appear to carry a credential.
func checkSecrets(store *Store) []Finding {
	var findings []Finding
	for _, record := range store.Records {
		for _, hit := range suspectedCredentials(record.Body) {
			severity := SeverityError
			qualifier := ""
			if !hit.certain {
				severity = SeverityWarn
				qualifier = " what looks like"
			}
			findings = append(findings, Finding{
				Rule: "brain/record-secret-suspected", Severity: severity,
				Path: record.Path, ID: record.ID,
				Message: fmt.Sprintf(
					"line %d carries%s %s; a record is read and indexed widely, so remove it and rotate the credential",
					hit.line, qualifier, hit.name),
			})
		}
	}
	return findings
}

type credentialHit struct {
	line    int
	name    string
	certain bool
}

// suspectedCredentials returns where a credential appears to be, and never what
// it is.
func suspectedCredentials(text string) []credentialHit {
	var hits []credentialHit
	for number, line := range strings.Split(text, "\n") {
		for _, pattern := range credentialPatterns {
			if pattern.match.MatchString(line) {
				hits = append(hits, credentialHit{
					line: number + 1, name: pattern.name, certain: pattern.certain,
				})
				// One report per line: a key matching two shapes is still one
				// thing to remove.
				break
			}
		}
	}
	return hits
}
