package cli

import (
	"strings"
	"testing"
)

func TestBrainCheckDoesNotEchoCredentialInExtensionKey(t *testing.T) {
	h := brainFixture(t)
	// Assembled at run time so no credential-shaped literal sits in the tree.
	token := "ghp_" + strings.Repeat("A", 36)
	writeFile(t, h.path("brain/decisions/D-0001-example.md"),
		"---\nid: D-0001\nstatus: provisional\n"+token+": "+token+"\n---\n\n# Example\n")
	code, output := h.run("brain", "check", "--only", "record-secret-suspected")
	if code != ExitFindings {
		t.Fatalf("exit %d: %s", code, output)
	}
	if strings.Contains(output, token[:7]) {
		t.Fatal("credential in extension key echoed in secret finding")
	}
}

func TestBrainCheckScansExtensionKeyWithItsValue(t *testing.T) {
	h := brainFixture(t)
	value := strings.Repeat("example", 4)
	// Create scans the rendered header, so it sees "password: <value>" and
	// refuses; check has to see the same text.
	code, _ := h.run("brain", "new", "decision", "--title", "Refused", "--set", "password="+value)
	if code != ExitFindings {
		t.Fatalf("setup: new exit=%d, want credential refusal", code)
	}
	writeFile(t, h.path("brain/decisions/D-0001-existing.md"),
		"---\nid: D-0001\nstatus: provisional\npassword: "+value+"\n---\n\n# Existing\n")
	code, output := h.run("brain", "check", "--only", "record-secret-suspected", "--fail-on-warning")
	if code != ExitFindings {
		t.Fatalf("same credential bypassed check's extension scan; exit=%d: %s", code, output)
	}
	if strings.Contains(output, value) {
		t.Fatal("finding quoted the value")
	}
	if !strings.Contains(output, "password") {
		t.Errorf("a key with no credential shape should be named: %s", output)
	}
}
