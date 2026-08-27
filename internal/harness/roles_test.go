package harness_test

import (
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
)

// A model name lives in a vendor's namespace. vat generated `model = "opus"`
// into a Codex TOML — a name Codex cannot resolve — from the very tool whose
// job is to stop one role behaving differently per runtime.
func TestARoleSpanningRuntimesNamesNoModelItCannotHonour(t *testing.T) {
	// Arrange
	role := harness.Role{
		Name: "go-reviewer", Description: "Reviews Go changes.",
		Model: "opus", Runtimes: []string{"claude", "codex"},
	}

	// Act & Assert
	if got := role.ModelFor("codex"); got != "" {
		t.Errorf("codex adapter would request %q, a model outside its namespace", got)
	}
	if got := role.ModelFor("claude"); got != "" {
		t.Errorf("an ambiguous model must bind to no runtime, got %q for claude", got)
	}
	if !role.ModelIsAmbiguous() {
		t.Error("the role names one model for two vendors and should be reported")
	}
	for _, adapter := range harness.RenderAdapters(role) {
		if strings.Contains(adapter.Content, "opus") {
			t.Errorf("%s adapter still names opus:\n%s", adapter.Runtime, adapter.Content)
		}
	}
}

// The per-runtime map is what lets one role body run on two vendors.
func TestEachRuntimeGetsItsOwnModelFromTheMap(t *testing.T) {
	// Arrange
	role := harness.Role{
		Name: "go-reviewer", Description: "Reviews Go changes.",
		Models:   map[string]string{"claude": "opus", "codex": "gpt-5.6-sol"},
		Runtimes: []string{"claude", "codex"},
	}

	// Act & Assert
	if got := role.ModelFor("claude"); got != "opus" {
		t.Errorf("claude model = %q, want opus", got)
	}
	if got := role.ModelFor("codex"); got != "gpt-5.6-sol" {
		t.Errorf("codex model = %q, want gpt-5.6-sol", got)
	}
	if role.ModelIsAmbiguous() {
		t.Error("a role with an entry per runtime is not ambiguous")
	}
	for _, adapter := range harness.RenderAdapters(role) {
		other := map[string]string{"claude": "gpt-5.6-sol", "codex": "opus"}[adapter.Runtime]
		if strings.Contains(adapter.Content, other) {
			t.Errorf("%s adapter names the other runtime's model %q:\n%s", adapter.Runtime, other, adapter.Content)
		}
	}
}

// A role that targets one runtime has no ambiguity to resolve, and forcing a
// map on it would be ceremony for nothing.
func TestASingleRuntimeRoleStillHonoursABareModel(t *testing.T) {
	// Arrange
	role := harness.Role{
		Name: "planner", Description: "Plans.",
		Model: "opus", Runtimes: []string{"claude"},
	}

	// Act & Assert
	if got := role.ModelFor("claude"); got != "opus" {
		t.Errorf("model = %q, want opus", got)
	}
	if role.ModelIsAmbiguous() {
		t.Error("one runtime, one model: nothing is ambiguous")
	}
}

// A role name becomes a file in every runtime's adapter directory, so it is
// held to the same portability rule a repository name is: `.claude/agents/con.md`
// cannot exist on Windows, and the definition is committed for everybody.
func TestARoleNameThatCannotBecomeAFileOnEveryPlatformIsRefused(t *testing.T) {
	// Act & Assert
	for _, name := range []string{"con", "NUL", "aux", "com1", "lpt9", "prn"} {
		if harness.ValidRoleName(name) {
			t.Errorf("%q was accepted as a role name", name)
		}
	}
	for _, name := range []string{"console", "connect", "com", "reviewer", "go-reviewer"} {
		if !harness.ValidRoleName(name) {
			t.Errorf("%q is a usable name and was refused", name)
		}
	}
}

// Two definitions whose names differ only in case are one file on macOS and on
// Windows, so a pair authored on Linux arrives at a colleague's checkout as one
// file silently overwriting the other. Refused at load, on every platform: the
// filesystem catching it is not a rule, it is a coincidence of where the
// definition was typed.
func TestTwoDefinitionsDifferingOnlyInCaseAreRefused(t *testing.T) {
	// Arrange
	// Two files, because on a case-insensitive filesystem two paths differing
	// only in case are already one file. What collides is the name each one
	// declares, which is what the adapter path is built from.
	root := t.TempDir()
	writeAt(t, root, ".agents/roles/one.md",
		"---\nname: planner\ndescription: Lower.\n---\n\nBody.\n")
	writeAt(t, root, ".agents/roles/two.md",
		"---\nname: Planner\ndescription: Upper.\n---\n\nBody.\n")

	// Act
	_, _, err := harness.LoadRoles(root)

	// Assert
	if err == nil {
		t.Fatal("two roles claiming one file on macOS were loaded")
	}
	if !strings.Contains(err.Error(), "case") {
		t.Errorf("the refusal does not say what the collision is: %v", err)
	}
}

func TestTwoDefinitionsWithGenuinelyDifferentNamesLoad(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeAt(t, root, ".agents/roles/planner.md",
		"---\nname: planner\ndescription: One.\n---\n\nBody.\n")
	writeAt(t, root, ".agents/roles/reviewer.md",
		"---\nname: reviewer\ndescription: Two.\n---\n\nBody.\n")

	// Act
	roles, _, err := harness.LoadRoles(root)

	// Assert
	if err != nil {
		t.Fatalf("two distinct roles were refused: %v", err)
	}
	if len(roles) != 2 {
		t.Errorf("expected both roles, got %d", len(roles))
	}
}
