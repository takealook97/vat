package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// "Never put a secret in a record" was the one rule in this layer that lived
// only in prose, which makes it the one rule that violates rule 1 — a rule that
// cannot be checked is not a rule. It matters more here than in most places: a
// brain repository is exactly the thing an organisation points an external
// index at, so a pasted token becomes a searchable token.
func TestCheckReportsASecretPastedIntoARecord(t *testing.T) {
	// Arrange
	// Create refuses this content, so the file is written the way a person
	// pasting into an editor would write it.
	root := newStore(t)
	writeRecordFile(t, root, "memory/M-0001-deploy-notes.md",
		"---\nid: M-0001\nstatus: provisional\n---\n\n# M-0001 — Deploy notes\n\nThe collector reads AKIA"+
			strings.Repeat("Q", 16)+" from the environment.\n")

	// Act
	findings := Check(mustLoad(t, root), CheckPolicy{}, observedOn)

	// Assert
	var found *Finding
	for i, finding := range findings {
		if finding.Rule == "brain/record-secret-suspected" {
			found = &findings[i]
		}
	}
	if found == nil {
		t.Fatalf("a credential in a record was not reported: %+v", findings)
	}
	if found.Severity != SeverityError {
		t.Errorf("severity = %q, want error", found.Severity)
	}
	// Rule 4: a finding about a credential must not become a second copy of it.
	if strings.Contains(found.Message, "AKIA") {
		t.Errorf("the finding quoted the credential back: %q", found.Message)
	}
	if !strings.Contains(found.Message, "line 3") {
		t.Errorf("the finding does not say where to look: %q", found.Message)
	}
}

func writeRecordFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A publishing job writes the source address into an extension key, which is
// exactly where credentials inside a URL land, and the body check never read
// the header.
func TestCheckReportsASecretInAnExtensionField(t *testing.T) {
	// Arrange
	root := newStore(t)
	writeRecordFile(t, root, "decisions/D-0001-x.md",
		"---\nid: D-0001\nstatus: provisional\nsource_url: https://bot:"+strings.Repeat("k", 9)+
			"@example.com/x\n---\n\n# D-0001 — x\n")

	// Act
	findings := Check(mustLoad(t, root), CheckPolicy{}, observedOn)

	// Assert
	for _, finding := range findings {
		if finding.Rule == "brain/record-secret-suspected" {
			if !strings.Contains(finding.Message, "source_url") || strings.Contains(finding.Message, strings.Repeat("k", 9)) {
				t.Errorf("finding %q should name the field and not the value", finding.Message)
			}
			return
		}
	}
	t.Fatalf("a credential in an extension field was not reported: %+v", findings)
}

func TestCheckLeavesOrdinaryProseAlone(t *testing.T) {
	// Arrange: a rule that fires on ordinary writing gets switched off, and
	// then it protects nothing.
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindDecision, ID: "D-0001", Title: "Tokens live in the secret manager",
		ClaimKind: ClaimHistorical,
		Body: "# D-0001 — Tokens live in the secret manager\n\n" +
			"## Decision\n\nThe deploy token is read from the secret manager at startup,\n" +
			"never from source_ref payments@3f9a1c2e8b74:docs/DEPLOY.md or a config file.\n",
	})

	// Act
	findings := Check(mustLoad(t, root), CheckPolicy{}, observedOn)

	// Assert
	for _, finding := range findings {
		if finding.Rule == "brain/record-secret-suspected" {
			t.Errorf("ordinary prose was reported as a credential: %+v", finding)
		}
	}
}
