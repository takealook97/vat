package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/harness"
)

func TestEveryStarterSkillCanBeAdvertisedAndRendered(t *testing.T) {
	// Arrange: a seed that trips the rules vat reports on every other workspace
	// would be vat writing its own lint findings into a new workspace on day one.
	starters := harness.StarterSkills()
	if len(starters) == 0 {
		t.Fatal("no starter skills; a seed nobody defines cannot be reviewed")
	}

	// Act & Assert
	for _, skill := range starters {
		if !harness.ValidRoleName(skill.Name) {
			t.Errorf("starter skill %q has a name that cannot become a directory", skill.Name)
		}
		if strings.TrimSpace(skill.Description) == "" {
			t.Errorf("starter skill %q has no description, so no runtime can advertise it", skill.Name)
		}
		if strings.TrimSpace(skill.Body) == "" {
			t.Errorf("starter skill %q has no procedure in it", skill.Name)
		}
		if len(harness.RenderSkillAdapters(skill)) == 0 {
			t.Errorf("starter skill %q renders no adapter", skill.Name)
		}
	}
}

func TestCrossRepoStarterShipsBeforeItCloses(t *testing.T) {
	// A seeded procedure that closes first sends every new adopter into a command
	// the CLI must refuse, because close requires landing evidence from ship.
	var body string
	for _, skill := range harness.StarterSkills() {
		if skill.Name == "before-cross-repo-work" {
			body = skill.Body
			break
		}
	}
	ship := strings.Index(body, "vat ship <id>")
	close := strings.Index(body, "vat changeset close <id>")
	if ship < 0 || close < 0 || ship > close {
		t.Fatalf("starter does not follow verify -> ship -> close:\n%s", body)
	}
}

func TestWriteStarterSkillsNeverOverwritesWhatIsAlreadyThere(t *testing.T) {
	// Arrange: the seed is the user's file the moment it lands. Rewriting an
	// edited procedure on some later run would make vat the author of a document
	// somebody else is responsible for following.
	root := t.TempDir()
	if _, err := harness.WriteStarterSkills(root); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first := harness.StarterSkills()[0]
	path := filepath.Join(root, harness.SkillsDir, first.Name, harness.SkillFile)
	const edited = "---\nname: edited\ndescription: mine now\n---\n\nMy steps.\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}

	// Act
	written, err := harness.WriteStarterSkills(root)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}

	// Assert
	if len(written) != 0 {
		t.Errorf("a second run rewrote %v", written)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != edited {
		t.Errorf("the edited procedure was overwritten:\n%s", after)
	}
}
