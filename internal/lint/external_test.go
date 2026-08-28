package lint_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/workspace"
)

// A rule whose only remedy is the wrong action is worse than no rule. Silencing
// "this source is not governed" meant enrolling the system in vat.yaml, which
// stops the warning by making the workspace claim to sync, diagnose, and ship a
// repository it does not own — and that is what somebody did, and had to undo.

func TestAClaimAboutAnUngovernedSourceIsToldHowToSayItIsExternal(t *testing.T) {
	// Arrange
	ws, root := withBrain(t)
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	writeBrainRecord(t, root, "gaps/G-0001-x.md",
		"id: G-0001\nstatus: active\nclaim_kind: current-state\nowned_by: hermes\n"+
			"source_ref: hermes@abc1234\nobserved_at: \"2026-08-25\"")

	// Act: these rules read the manifest and the clone, and the source-ref
	// family sits behind the offline gate, so the non-offline runner is the one
	// that reaches them.
	report := runOnline(t, ws)

	// Assert
	finding, found := rules(report)["brain/source-repo-unknown"]
	if !found {
		t.Fatalf("a claim about an ungoverned source went unreported: %+v", report.Findings)
	}
	if finding.Fix == "" {
		t.Fatal("the rule states a problem and no remedy, which is how the wrong remedy gets chosen")
	}
	if !strings.Contains(finding.Fix, "source_external") {
		t.Errorf("the remedy does not name the way to declare it external: %q", finding.Fix)
	}
}

func TestADeclaredExternalSourceIsNotReported(t *testing.T) {
	// Arrange: the workspace has said, once, that this system is deliberately
	// outside it. Saying so again on every run is nagging, and nagging is what
	// people silence by enrolling the repository.
	ws, root := withBrain(t)
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	writeBrainRecord(t, root, "gaps/G-0001-x.md",
		"id: G-0001\nstatus: active\nclaim_kind: current-state\nowned_by: hermes\n"+
			"source_ref: hermes@abc1234\nsource_external: true\nobserved_at: \"2026-08-25\"")

	// Act: these rules read the manifest and the clone, and the source-ref
	// family sits behind the offline gate, so the non-offline runner is the one
	// that reaches them.
	report := runOnline(t, ws)

	// Assert
	if _, found := rules(report)["brain/source-repo-unknown"]; found {
		t.Errorf("a source declared external was still reported: %+v", report.Findings)
	}
}

func TestDeclaringAGovernedSourceExternalIsAnError(t *testing.T) {
	// Arrange: otherwise the field is a way to exempt a checkable claim from
	// every check that makes it checkable.
	ws, root := withBrain(t)
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("brain init: %v", err)
	}
	writeBrainRecord(t, root, "gaps/G-0001-x.md",
		"id: G-0001\nstatus: active\nclaim_kind: current-state\nowned_by: knowledge\n"+
			"source_ref: knowledge@abc1234\nsource_external: true\nobserved_at: \"2026-08-25\"")

	// Act: these rules read the manifest and the clone, and the source-ref
	// family sits behind the offline gate, so the non-offline runner is the one
	// that reaches them.
	report := runOnline(t, ws)

	// Assert
	finding, found := rules(report)["brain/source-external-governed"]
	if !found {
		t.Fatalf("a governed repository declared external went unreported: %+v", report.Findings)
	}
	if finding.Severity != lint.SeverityError {
		t.Errorf("severity = %s, want error", finding.Severity)
	}
}

// runOnline runs every rule, including the source-ref family the offline gate
// holds back.
func runOnline(t *testing.T, ws *workspace.Workspace) lint.Report {
	t.Helper()
	report, err := lint.Run(context.Background(), ws, lint.Options{Now: reference})
	if err != nil {
		t.Fatalf("lint.Run returned an error: %v", err)
	}
	return report
}

func writeBrainRecord(t *testing.T, root, relative, frontMatter string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\n" + frontMatter + "\n---\n\n# " + filepath.Base(relative) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
