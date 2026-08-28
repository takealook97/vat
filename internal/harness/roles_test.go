package harness_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
	"gopkg.in/yaml.v3"
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

// The path is printed beside the problem by every caller, so repeating it inside
// the problem is noise in the one situation a reader is already confused by. A
// directory where a SKILL.md should be produced
// ".agents/skills/weird/SKILL.md: read .agents/skills/weird/SKILL.md: read
// .agents/skills/weird/SKILL.md: is a directory" — the path three times, once
// per layer that thought it was adding context.
func TestAMalformedDefinitionIsReportedWithoutRepeatingItsPath(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, harness.SkillsDir, "weird", harness.SkillFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Act
	_, malformed, err := harness.LoadSkills(root)
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}

	// Assert
	if len(malformed) != 1 {
		t.Fatalf("expected one malformed definition, got %+v", malformed)
	}
	entry := malformed[0]
	if entry.Path != ".agents/skills/weird/SKILL.md" {
		t.Errorf("path = %q", entry.Path)
	}
	if strings.Contains(entry.Problem, entry.Path) {
		t.Errorf("the problem repeats the path it is printed beside: %q", entry.Problem)
	}
	// The operating system supplies the cause and words it differently: POSIX
	// says "is a directory", Windows says "Incorrect function." Asserting the
	// POSIX phrase made this a test of the platform. What has to hold
	// everywhere is that a cause is carried at all.
	if strings.TrimSpace(entry.Problem) == "" {
		t.Errorf("the problem does not say what went wrong: %q", entry.Problem)
	}
}

// The adapter's front matter is what a runtime parses to find the role at all.
// A backslash in a description produced `description: "a back\slash"` — `\s` is
// not a YAML escape, so the header does not parse and the role is invisible to
// the runtime it was generated for. vat never noticed: nothing reads an adapter
// back except a comparison against the string it just rendered.
func TestAGeneratedHeaderSurvivesWhateverADescriptionContains(t *testing.T) {
	// Arrange
	cases := []struct{ name, description string }{
		{"quoted", `He said "hello"`},
		{"backslash", `a back\slash`},
		{"both", `He said "hi" and a back\slash`},
		{"newline", "line one\nline two"},
		{"tab", "before\tafter"},
		{"colon", "key: value"},
		{"leading-space", "  indented"},
		{"yaml-ish", "*anchor &ref !tag |block >fold %directive @at `tick`"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeAt(t, root, ".agents/roles/"+testCase.name+".md",
				"---\nname: "+testCase.name+"\ndescription: "+strconv.Quote(testCase.description)+
					"\nruntimes: [claude]\n---\n\nBody.\n")
			roles, _, err := harness.LoadRoles(root)
			if err != nil {
				t.Fatalf("LoadRoles: %v", err)
			}
			if len(roles) != 1 {
				t.Fatalf("expected the role, got %+v", roles)
			}

			// Act
			adapter := harness.RenderAdapters(roles[0])[0]
			header, _, found := strings.Cut(strings.TrimPrefix(adapter.Content, "---\n"), "\n---")
			if !found {
				t.Fatalf("the adapter has no front matter:\n%s", adapter.Content)
			}

			// Assert: what a runtime does with it.
			var parsed struct {
				Name        string `yaml:"name"`
				Description string `yaml:"description"`
			}
			if err := yaml.Unmarshal([]byte(header), &parsed); err != nil {
				t.Fatalf("the generated header does not parse: %v\n%s", err, header)
			}
			if parsed.Name != testCase.name {
				t.Errorf("name = %q", parsed.Name)
			}
			if parsed.Description != roles[0].Description {
				t.Errorf("description round-tripped as %q, want %q", parsed.Description, roles[0].Description)
			}
		})
	}
}

// The model is written into the same header the description is, and the
// adoption path beside it escapes both. This one did not: a model name carrying
// a colon would have produced a header the runtime cannot parse, which is the
// same failure with a different field.
func TestAModelNameIsEscapedIntoTheHeaderToo(t *testing.T) {
	// Arrange
	root := t.TempDir()
	writeAt(t, root, ".agents/roles/planner.md",
		"---\nname: planner\ndescription: Plans.\nmodels:\n  claude: \"vendor: preview\"\nruntimes: [claude]\n---\n\nBody.\n")
	roles, _, err := harness.LoadRoles(root)
	if err != nil {
		t.Fatalf("LoadRoles: %v", err)
	}

	// Act
	adapter := harness.RenderAdapters(roles[0])[0]
	header, _, _ := strings.Cut(strings.TrimPrefix(adapter.Content, "---\n"), "\n---")

	// Assert
	var parsed struct {
		Model string `yaml:"model"`
	}
	if err := yaml.Unmarshal([]byte(header), &parsed); err != nil {
		t.Fatalf("the generated header does not parse: %v\n%s", err, header)
	}
	if parsed.Model != "vendor: preview" {
		t.Errorf("model round-tripped as %q", parsed.Model)
	}
}

// A role declaring reasoning_effort reached Codex as model_reasoning_effort and
// reached Claude as nothing at all: the field was written, the adapter was
// generated, and one runtime silently ignored it. That is the shape of setting
// this repository keeps finding — deliberate to read, inert in effect.
//
// Claude Code spells it `effort` on a subagent, so the canonical field is
// translated per runtime exactly as the model name already is.
func TestReasoningEffortReachesEveryRuntimeThatHasAWordForIt(t *testing.T) {
	// Arrange
	role := harness.Role{
		Name: "reviewer", Description: "Reviews changes.",
		ReasoningEffort: "high",
		Runtimes:        []string{"claude", "codex"},
	}

	// Act
	adapters := harness.RenderAdapters(role)

	// Assert
	if len(adapters) != 2 {
		t.Fatalf("rendered %d adapters, want 2", len(adapters))
	}
	for _, adapter := range adapters {
		field := "effort: high"
		if adapter.Runtime == "codex" {
			field = `model_reasoning_effort = "high"`
		}
		if !strings.Contains(adapter.Content, field) {
			t.Errorf("the %s adapter does not carry the declared effort:\n%s", adapter.Runtime, adapter.Content)
		}
	}
}

// And a role that declares none must not have one invented for it: an effort
// nobody chose is a setting nobody can account for.
func TestNoEffortIsWrittenWhenNoneIsDeclared(t *testing.T) {
	// Arrange
	role := harness.Role{
		Name: "reviewer", Description: "Reviews changes.",
		Runtimes: []string{"claude", "codex"},
	}

	// Act
	adapters := harness.RenderAdapters(role)

	// Assert
	for _, adapter := range adapters {
		if strings.Contains(adapter.Content, "effort") {
			t.Errorf("the %s adapter invented an effort:\n%s", adapter.Runtime, adapter.Content)
		}
	}
}
