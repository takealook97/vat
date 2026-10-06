package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func isolateGitIdentity(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, empty, "")
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestBrainRecordsUseLocalGitAuthor(t *testing.T) {
	isolateGitIdentity(t)
	h := brainFixture(t)
	git(t, h.path("brain"), "config", "user.name", "Fixture Author")
	git(t, h.path("brain"), "config", "user.email", "author@example.com")
	h.mustRun("brain", "new", "decision", "--title", "A reviewed decision")
	h.mustRun("brain", "promote", "D-0001")
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	record := store.ByID()["D-0001"]
	if record.RecordedBy != "Fixture Author <author@example.com>" || record.ReviewedBy != record.RecordedBy {
		t.Fatalf("identity not recorded: %#v", record.Metadata)
	}
}

func TestBrainPromotionRequiresConfiguredIdentity(t *testing.T) {
	for _, gate := range []string{"manual", "agent", "auto"} {
		t.Run(gate, func(t *testing.T) {
			isolateGitIdentity(t)
			h := brainFixture(t)
			git(t, h.path("brain"), "config", "--unset", "user.name")
			git(t, h.path("brain"), "config", "--unset", "user.email")
			manifest := readFile(t, h.path("vat.yaml"))
			writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: "+gate, 1))
			h.mustRun("brain", "new", "goal", "--title", "An intended outcome")
			code, output := h.run("brain", "promote", "O-0001")
			if gate == "auto" {
				if code != ExitOK {
					t.Fatalf("auto refused: %s", output)
				}
			} else if code == ExitOK || !strings.Contains(output, "git user.name/user.email") {
				t.Fatalf("%s did not refuse missing identity: %s", gate, output)
			}
			store, err := brain.Load(h.path("brain"))
			if err != nil {
				t.Fatal(err)
			}
			record := store.ByID()["O-0001"]
			if record.RecordedBy != "" || record.ReviewedBy != "" {
				t.Fatalf("invented identity: %#v", record.Metadata)
			}
		})
	}
}

func TestAgentPromotionReportsEvidenceRefusalsAndPromotesGoals(t *testing.T) {
	isolateGitIdentity(t)
	h := brainFixture(t, "payments")
	manifest := readFile(t, h.path("vat.yaml"))
	writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: agent", 1))
	h.mustRun("brain", "new", "decision", "--title", "A decision without provenance")
	h.mustRun("brain", "new", "gap", "--title", "A repo-only pin", "--claim", "current-state", "--owner", "payments")
	h.mustRun("brain", "new", "gap", "--title", "A path pin", "--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	h.mustRun("brain", "new", "goal", "--title", "An intended outcome")
	code, output := h.run("brain", "promote", "D-0001", "G-0001", "G-0002", "O-0001")
	if code != ExitFindings || !strings.Contains(output, "2 of 4 refused") {
		t.Fatalf("batch: %d %s", code, output)
	}
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"D-0001", "G-0001"} {
		if store.ByID()[id].Status != brain.StatusProvisional {
			t.Fatalf("%s bypassed gate", id)
		}
	}
	for _, id := range []string{"G-0002", "O-0001"} {
		if store.ByID()[id].Status != brain.StatusActive {
			t.Fatalf("%s did not promote: %s", id, output)
		}
	}
	h.mustRun("brain", "check", "--only", "reviewer-unattributed")
}

func TestAgentReverificationRefusesUnreadableSourceHEAD(t *testing.T) {
	isolateGitIdentity(t)
	h := brainFixture(t, "payments")
	manifest := readFile(t, h.path("vat.yaml"))
	writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: agent", 1))
	h.mustRun("brain", "new", "gap", "--title", "A path pin", "--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	git(t, h.root, "add", "vat.yaml")
	git(t, h.root, "commit", "--quiet", "-m", "workspace")
	if err := os.RemoveAll(h.path("payments", ".git")); err != nil {
		t.Fatal(err)
	}
	// A plain directory nested in the workspace must not inherit its HEAD.
	code, output := h.run("brain", "promote", "G-0001", "--reverified")
	if code != ExitFindings || !strings.Contains(output, "readable owning repository HEAD") {
		t.Fatalf("unreadable HEAD: %d %s", code, output)
	}
}

func TestBrainCheckThreadsPromotionGateIntoAttributionRule(t *testing.T) {
	for _, gate := range []string{"manual", "agent", "auto"} {
		t.Run(gate, func(t *testing.T) {
			isolateGitIdentity(t)
			h := brainFixture(t)
			manifest := readFile(t, h.path("vat.yaml"))
			writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: "+gate, 1))
			h.mustRun("brain", "new", "decision", "--title", "A legacy active record")
			path := findRecord(t, h, "D-0001")
			writeFile(t, path, strings.Replace(readFile(t, path), "status: provisional", "status: active", 1))
			output := h.mustRun("brain", "check", "--only", "reviewer-unattributed")
			if strings.Contains(output, "brain/reviewer-unattributed") != (gate != "auto") {
				t.Fatalf("%s: %s", gate, output)
			}
		})
	}
}

func TestConfiguredIdentityIsRecordedUnderAutoAndAgentGoals(t *testing.T) {
	for _, gate := range []string{"auto", "agent"} {
		t.Run(gate, func(t *testing.T) {
			isolateGitIdentity(t)
			h := brainFixture(t)
			manifest := readFile(t, h.path("vat.yaml"))
			writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: "+gate, 1))
			h.mustRun("brain", "new", "goal", "--title", "An intended outcome")
			h.mustRun("brain", "promote", "O-0001")
			store, err := brain.Load(h.path("brain"))
			if err != nil {
				t.Fatal(err)
			}
			record := store.ByID()["O-0001"]
			if record.ReviewedBy != "Fixture Author <author@example.com>" || record.RecordedBy != record.ReviewedBy {
				t.Fatalf("%s identity: %#v", gate, record.Metadata)
			}
		})
	}
}

func TestMissingGitAllowsCreationAndAutoPromotionOnly(t *testing.T) {
	for _, gate := range []string{"manual", "agent", "auto"} {
		t.Run(gate, func(t *testing.T) {
			isolateGitIdentity(t)
			h := brainFixture(t)
			manifest := readFile(t, h.path("vat.yaml"))
			writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: "+gate, 1))
			t.Setenv("PATH", t.TempDir())
			h.mustRun("brain", "new", "decision", "--title", "Creation without git")
			code, output := h.run("brain", "promote", "D-0001")
			if gate == "auto" {
				if code != ExitOK {
					t.Fatalf("auto refused: %d %s", code, output)
				}
			} else if code != ExitFindings || !strings.Contains(output, "promotion requires a git identity") {
				t.Fatalf("%s: %d %s", gate, code, output)
			}
			store, err := brain.Load(h.path("brain"))
			if err != nil {
				t.Fatal(err)
			}
			record := store.ByID()["D-0001"]
			if record.RecordedBy != "" || record.ReviewedBy != "" {
				t.Fatalf("invented identity: %#v", record.Metadata)
			}
			if (record.Status == brain.StatusActive) != (gate == "auto") {
				t.Fatalf("%s: status %s", gate, record.Status)
			}
		})
	}
}

func TestAgentPromotionRefusesMissingPinnedPathsAlongsideOtherRefusals(t *testing.T) {
	isolateGitIdentity(t)
	h := brainFixture(t, "payments")
	manifest := readFile(t, h.path("vat.yaml"))
	writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: agent", 1))
	h.mustRun("brain", "new", "gap", "--title", "Missing evidence", "--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	path := findRecord(t, h, "G-0001")
	writeFile(t, path, strings.Replace(readFile(t, path), ":README.md", ":absent.md", 1))
	// An untracked file cannot turn a nonexistent pinned path into evidence.
	writeFile(t, h.path("payments", "absent.md"), "Only in the working tree")
	h.mustRun("brain", "new", "decision", "--title", "No provenance")
	h.mustRun("brain", "new", "goal", "--title", "Valid goal")
	for _, reverified := range []bool{false, true} {
		args := []string{"brain", "promote", "G-0001", "D-0001", "O-0001"}
		if reverified {
			args = append(args, "--reverified")
		}
		code, output := h.run(args...)
		if code != ExitFindings || !strings.Contains(output, "2 of 3 refused") || !strings.Contains(output, "does not hold absent.md at") || !strings.Contains(output, "source_ref pinned with a path") {
			t.Fatalf("reverified %v: %d %s", reverified, code, output)
		}
		store, err := brain.Load(h.path("brain"))
		if err != nil {
			t.Fatal(err)
		}
		if store.ByID()["G-0001"].Status != brain.StatusProvisional || store.ByID()["D-0001"].Status != brain.StatusProvisional || store.ByID()["O-0001"].Status != brain.StatusActive {
			t.Fatal("batch changed refused records or skipped valid goal")
		}
	}
}

func TestAgentReverificationRefusesPathsDeletedAtHEAD(t *testing.T) {
	// Arrange: the evidence exists at the pin but is deleted at the new HEAD.
	isolateGitIdentity(t)
	h := brainFixture(t, "payments")
	manifest := readFile(t, h.path("vat.yaml"))
	writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: agent", 1))
	h.mustRun("brain", "new", "gap", "--title", "Deleted evidence", "--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
	path := findRecord(t, h, "G-0001")
	before := readFile(t, path)
	git(t, h.path("payments"), "rm", "README.md")
	git(t, h.path("payments"), "commit", "--quiet", "-m", "Delete evidence")
	head := gitOutput(t, h.path("payments"), "rev-parse", "HEAD")

	// Act: re-verification would re-pin the record to the new HEAD.
	code, output := h.run("brain", "promote", "G-0001", "--reverified")

	// Assert: missing evidence is refused without changing the record.
	want := "G-0001: payments does not hold README.md at " + shortRevision(head)
	if code != ExitFindings || !strings.Contains(output, want) {
		t.Fatalf("deleted evidence: %d %s", code, output)
	}
	if after := readFile(t, path); after != before {
		t.Fatal("refused re-verification changed the record")
	}
}

func TestAgentPromotionRefusesSourcesOutsideWorkspace(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "ungoverned", true: "external"}[external], func(t *testing.T) {
			isolateGitIdentity(t)
			h := brainFixture(t, "payments")
			manifest := readFile(t, h.path("vat.yaml"))
			writeFile(t, h.path("vat.yaml"), strings.Replace(manifest, "brain_promote: manual", "brain_promote: agent", 1))
			h.mustRun("brain", "new", "gap", "--title", "Outside source", "--claim", "current-state", "--owner", "payments", "--source-path", "README.md")
			path := findRecord(t, h, "G-0001")
			content := strings.Replace(readFile(t, path), "source_ref: payments@", "source_ref: outside@", 1)
			if external {
				content = strings.Replace(content, "status: provisional", "status: provisional\nsource_external: true", 1)
			}
			writeFile(t, path, content)
			for _, reverified := range []bool{false, true} {
				args := []string{"brain", "promote", "G-0001"}
				if reverified {
					args = append(args, "--reverified")
				}
				code, output := h.run(args...)
				if code != ExitFindings || !strings.Contains(output, "1 of 1 refused") {
					t.Fatalf("reverified %v: %d %s", reverified, code, output)
				}
				if reverified && !strings.Contains(output, "readable owning repository HEAD") {
					t.Fatalf("wrong refusal: %s", output)
				}
				store, err := brain.Load(h.path("brain"))
				if err != nil {
					t.Fatal(err)
				}
				if store.ByID()["G-0001"].Status != brain.StatusProvisional {
					t.Fatal("outside source bypassed gate")
				}
			}
		})
	}
}
