package brain

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// addHeaderLines inserts front matter lines into a created record, so a
// test states exactly the header it is judging.
func addHeaderLines(t *testing.T, root, relative string, lines ...string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	extended := strings.Replace(string(original), "\nstatus: ", "\n"+strings.Join(lines, "\n")+"\nstatus: ", 1)
	if err := os.WriteFile(path, []byte(extended), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func externalFindings(findings []Finding) []string {
	var out []string
	for _, finding := range findings {
		if strings.HasPrefix(finding.Rule, "brain/external-") {
			out = append(out, finding.Rule+" "+finding.ID+" "+string(finding.Severity))
		}
	}
	sort.Strings(out)
	return out
}

const quotedLine = "source_quote: The vendor confirmed the cutover date."

// A publishing job clears a queue by running check in a loop, so every missing
// field and every bad address is reported in one run, archived records
// included: a record leaves the working set, not the evidence rule.
func TestExternalEvidenceFindingsAreComplete(t *testing.T) {
	// Arrange
	root := newStore(t)
	create := func(id string, lines ...string) {
		relative := mustCreate(t, root, NewRecordInput{Kind: KindMemory, ID: id, Title: "External " + id, ClaimKind: ClaimHistorical})
		addHeaderLines(t, root, relative, lines...)
	}
	create("M-0001", "source_external: true")
	create("M-0002", "source_external: true", "source_url: https://example.com/thread/2")
	create("M-0003", "source_external: true", "source_url: ftp://example.com/file", quotedLine)
	create("M-0004", "source_external: true", "source_url: https://someone:hunter2hunter2@example.com/x", quotedLine)
	create("M-0005", "source_url: not a url")
	create("M-0006", "source_external: true", "source_url: https://example.com/thread/6", quotedLine)
	create("M-0007", "source_external: true", "source_url: /relative/path", quotedLine)
	archived := mustCreate(t, root, NewRecordInput{Kind: KindMemory, ID: "M-0008", Title: "Archived", ClaimKind: ClaimHistorical,
		Status: StatusRevoked})
	addHeaderLines(t, root, archived, "source_external: true", "reason: withdrawn")
	if _, err := Archive(mustLoad(t, root), true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Act
	findings := Check(mustLoad(t, root), CheckPolicy{RequireExternalEvidence: true}, observedOn)

	// Assert
	want := []string{
		"brain/external-evidence-missing M-0001 error",
		"brain/external-evidence-missing M-0002 error",
		"brain/external-evidence-missing M-0008 error",
		"brain/external-source-url M-0003 error",
		"brain/external-source-url M-0004 error",
		"brain/external-source-url M-0005 error",
		"brain/external-source-url M-0007 error",
	}
	if got := externalFindings(findings); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, finding := range findings {
		if strings.Contains(finding.Message, "hunter2") || strings.Contains(finding.Message, "someone") {
			t.Errorf("a finding repeated the credential it reported: %s", finding.Message)
		}
	}
}

// Records written before the fields existed, by the convention the policy now
// checks, must not start failing on upgrade. The policy is opt-in.
func TestLegacyExternalRecordsRemainValidByDefault(t *testing.T) {
	// Arrange
	root := newStore(t)
	relative := mustCreate(t, root, NewRecordInput{Kind: KindDecision, ID: "D-0001", Title: "An external decision",
		ClaimKind: ClaimHistorical, Status: StatusActive})
	addHeaderLines(t, root, relative, "source_external: true", "project: demo-project")

	// Act
	findings := Check(mustLoad(t, root), defaultPolicy(), observedOn)

	// Assert
	if got := externalFindings(findings); len(got) != 0 {
		t.Fatalf("a legacy external record fails by default: %v", got)
	}
	if Errors(findings) != 0 {
		t.Fatalf("a legacy external record has errors: %+v", findings)
	}
}

// The address is judged by its shape only. Checking that it resolves would
// make a brain check depend on the network and on a third party staying up.
func TestExternalURLNeverRequiresNetwork(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	for _, address := range []string{"https://demo.invalid/never/resolves", "http://example.com"} {
		if err := ValidateSourceURL(address); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}
	for _, address := range []string{"mailto:someone@example.com", "https://", "https://user@example.com/x", "example.com/path"} {
		if err := ValidateSourceURL(address); err == nil {
			t.Errorf("%s was accepted", address)
		}
	}
}

// The fields are scanned for credentials like the body is: an address that
// carries a token is a published token.
func TestExternalEvidenceFieldsAreScannedForSecrets(t *testing.T) {
	root := newStore(t)
	relative := mustCreate(t, root, NewRecordInput{Kind: KindMemory, ID: "M-0001", Title: "Leaky", ClaimKind: ClaimHistorical})
	token := "gh" + "p_" + strings.Repeat("A1", 18)
	addHeaderLines(t, root, relative, "source_external: true", "source_url: https://example.com/x", "source_quote: token "+token)
	findings := Check(mustLoad(t, root), defaultPolicy(), observedOn)
	found := false
	for _, finding := range findings {
		if finding.Rule == "brain/record-secret-suspected" && strings.Contains(finding.Message, "source_quote") {
			found = true
		}
		if strings.Contains(finding.Message, token) {
			t.Errorf("a finding repeated the credential: %s", finding.Message)
		}
	}
	if !found {
		t.Fatalf("a token in source_quote was not reported: %+v", findings)
	}
}
