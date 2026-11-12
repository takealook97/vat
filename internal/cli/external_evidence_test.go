package cli

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func requireExternalEvidence(t *testing.T, h *workspaceFixture) {
	t.Helper()
	manifest := readFile(t, h.path("vat.yaml"))
	updated := manifest
	for _, line := range strings.SplitAfter(manifest, "\n") {
		if strings.TrimSpace(line) == "require_promotion_gate: true" {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			updated = strings.Replace(manifest, line, line+indent+"require_external_evidence: true\n", 1)
		}
	}
	if updated == manifest {
		t.Fatalf("fixture manifest has no brain policy to extend:\n%s", manifest)
	}
	writeFile(t, h.path("vat.yaml"), updated)
}

// The policy lives in vat.yaml and the rule in the brain package; the only
// thing joining them is the command. A policy that parses and never reaches
// the rule is a rule silently switched off.
func TestExternalEvidencePolicyReachesWorkspaceChecks(t *testing.T) {
	// Arrange
	isolateGitIdentity(t)
	h := brainFixture(t)
	h.mustRun("brain", "new", "memory", "--title", "An external event", "--claim", "historical")
	path := findRecord(t, h, "M-0001")
	writeFile(t, path, strings.Replace(readFile(t, path), "status: provisional", "status: provisional\nsource_external: true", 1))

	// Act
	before := h.mustRun("brain", "check", "--only", "external")
	requireExternalEvidence(t, h)
	code, after := h.run("brain", "check", "--only", "external")

	// Assert
	if strings.Contains(before, "brain/external-evidence-missing") {
		t.Fatalf("the rule fired with the policy off: %s", before)
	}
	if code != ExitFindings || !strings.Contains(after, "brain/external-evidence-missing") ||
		!strings.Contains(after, "source_url or source_quote") {
		t.Fatalf("the policy did not reach the check: %d %s", code, after)
	}
}

func TestBrainNewWritesExternalEvidenceAndRefusesABadAddress(t *testing.T) {
	isolateGitIdentity(t)
	h := brainFixture(t)
	h.mustRun("brain", "new", "memory", "--title", "Vendor cutover", "--claim", "historical",
		"--source-url", "https://example.com/notes/1", "--source-quote", "The cutover moves to Monday.")
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	record := store.ByID()["M-0001"]
	if record.SourceURL != "https://example.com/notes/1" || record.SourceQuote != "The cutover moves to Monday." {
		t.Fatalf("evidence not written: %#v", record.Metadata)
	}
	code, output := h.run("brain", "new", "memory", "--title", "Leaky", "--source-url", "https://someone:secretsecret@example.com/x")
	if code != ExitUsage || !strings.Contains(output, "user information") || strings.Contains(output, "secretsecret") {
		t.Fatalf("bad address: %d %s", code, output)
	}
	store, err = brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Records) != 1 {
		t.Fatalf("a refused record was written: %d records", len(store.Records))
	}
}

// Publishing jobs wrote source_url as an extension key before it was a field.
// Those records must read the same value through the typed field and keep it
// through every transition vat performs.
func TestFormerExtensionURLSurvivesEveryTransition(t *testing.T) {
	// Arrange
	isolateGitIdentity(t)
	h := brainFixture(t)
	setPromotionGate(t, h, "auto")
	const address = "https://example.com/thread/0001"
	for _, title := range []string{"Old external decision", "New external decision"} {
		h.mustRun("brain", "new", "decision", "--title", title, "--claim", "historical")
	}
	for _, id := range []string{"D-0001", "D-0002"} {
		path := findRecord(t, h, id)
		writeFile(t, path, strings.Replace(readFile(t, path), "status: provisional",
			"status: provisional\nsource_external: true\nproject: demo-project\nsource_url: "+address, 1))
	}

	// Act
	h.mustRun("brain", "promote", "D-0001", "D-0002")
	h.mustRun("brain", "supersede", "D-0001", "D-0002")
	h.mustRun("brain", "revoke", "D-0002", "--reason", "the thread was about another vendor")
	h.mustRun("brain", "archive", "--apply")
	h.mustRun("brain", "build")

	// Assert
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"D-0001", "D-0002"} {
		record := store.ByID()[id]
		if record.SourceURL != address {
			t.Errorf("%s source_url %q, want %q", id, record.SourceURL, address)
		}
		if !strings.Contains(readFile(t, findRecord(t, h, id)), "project: demo-project\n") {
			t.Errorf("%s lost its extension key", id)
		}
	}
}
