package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/ui"
)

func TestFixReviewSymlinkCannotBypassWorkspaceGate(t *testing.T) {
	isolateGitIdentity(t)
	h := brainFixture(t)
	h.mustRun("brain", "new", "decision", "--title", "Manual gate")
	writeFile(t, h.path("brain/.brain"), "schema: 1\npolicy:\n  promotion_gate: auto\n")
	git(t, h.path("brain"), "config", "user.name", "")
	git(t, h.path("brain"), "config", "user.email", "")
	elsewhere := t.TempDir()
	link := filepath.Join(elsewhere, "brain-link")
	if err := os.Symlink(h.path("brain"), link); err != nil {
		t.Skip(err)
	}
	var out, errOut bytes.Buffer
	env := &Env{Cwd: elsewhere, BrainDir: link, Now: testNow, Yes: true, Printer: ui.NewWith(&out, &errOut, false)}
	bc, err := resolveBrain(env)
	if err != nil {
		t.Fatal(err)
	}
	code := dispatch(context.Background(), env, Root(), []string{"brain", "promote", "D-0001"}, nil)
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	r := store.ByID()["D-0001"]
	if r.Status == brain.StatusActive {
		t.Fatalf("workspace manual gate bypassed through root symlink: standalone=%v gate=%s exit=%d status=%s reviewer=%q",
			bc.Standalone(), bc.Policy.PromotionGate, code, r.Status, r.ReviewedBy)
	}
	if bc.Standalone() {
		t.Fatal("a symbolic link to the workspace brain opened it standalone")
	}
}

func TestFixReviewArchiveRefusalWritesNothing(t *testing.T) {
	h := brainFixture(t)
	writeFile(t, h.path("brain/decisions/D-0001-first.md"), "---\nid: D-0001\nstatus: revoked\n---\n\n# First\n")
	writeFile(t, h.path("brain/gaps/G-0001-second.md"), "---\nid: G-0001\nstatus: resolved\n---\n\n# Second\n")
	outside := t.TempDir()
	if err := os.Symlink(outside, h.path("brain/archive/gaps")); err != nil {
		t.Skip(err)
	}
	code, output := h.run("brain", "archive", "--apply")
	if code != 1 || !strings.Contains(output, "nothing was written") {
		t.Fatalf("exit=%d output=%s", code, output)
	}
	if _, err := os.Stat(h.path("brain/decisions/D-0001-first.md")); os.IsNotExist(err) {
		t.Fatalf("refusal said nothing was written but D-0001 already moved into archive; exit=%d output=%s", code, output)
	}
}

func TestFixReviewWorkspaceLifecycleUnderVarAlias(t *testing.T) {
	isolateGitIdentity(t)
	h := brainFixture(t)
	h.mustRun("brain", "new", "decision", "--title", "Orders use idempotency")
	h.mustRun("brain", "new", "decision", "--title", "Orders use tenant keys")
	h.mustRun("brain", "new", "decision", "--title", "Withdraw this decision")
	h.mustRun("brain", "promote", "D-0001")
	h.mustRun("brain", "supersede", "D-0001", "D-0002")
	h.mustRun("brain", "revoke", "D-0003", "--reason", "withdrawn")
	h.mustRun("brain", "archive", "--apply")
	h.mustRun("brain", "build")
	h.mustRun("brain", "check")
	h.mustRun("brain", "list")
	h.mustRun("brain", "query", "tenant")
}

func TestFixReviewLifecycleKeepsComplexExtensionKey(t *testing.T) {
	h := brainFixture(t)
	writeFile(t, h.path("brain/decisions/D-0001-example.md"),
		"---\nid: D-0001\nstatus: provisional\n? [a, b]\n: value\nnote: retained\n---\n\n# Example\n")
	h.mustRun("brain", "promote", "D-0001")
	h.mustRun("brain", "revoke", "D-0001", "--reason", "withdrawn")
	h.mustRun("brain", "archive", "--apply")
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	r := store.ByID()["D-0001"]
	if r.Status != brain.StatusRevoked || !r.Archived || r.Extensions["note"] != "retained" {
		t.Fatal("lifecycle lost the recovered record")
	}
}
