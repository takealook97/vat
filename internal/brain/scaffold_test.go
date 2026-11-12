package brain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func TestMarkMakesADirectoryABrainAndWritesNothingElse(t *testing.T) {
	// Arrange: a repository somebody already keeps notes in. Adoption asserts
	// that this directory is the brain and promises to convert nothing.
	root := t.TempDir()
	existing := filepath.Join(root, "NOTES.md")
	if err := os.WriteFile(existing, []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Act
	marked, err := brain.Mark(root)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}

	// Assert
	if !marked {
		t.Error("Mark reported it wrote nothing to a directory that was not a brain")
	}
	if !brain.IsBrain(root) {
		t.Error("the directory is still not a brain after being marked")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("marking scaffolded more than the marker: %v", names)
	}
	content, err := os.ReadFile(existing)
	if err != nil || string(content) != "mine\n" {
		t.Errorf("the file that was already there was disturbed: %v %q", err, content)
	}
}

func TestMarkIsANoOpOnADirectoryThatIsAlreadyABrain(t *testing.T) {
	// Arrange: adoption can be run twice, and the second run must not rewrite a
	// marker whose contents record when the layout was created.
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(root, brain.MarkerFile))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}

	// Act
	marked, err := brain.Mark(root)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}

	// Assert
	if marked {
		t.Error("Mark claimed to write a marker that was already there")
	}
	after, err := os.ReadFile(filepath.Join(root, brain.MarkerFile))
	if err != nil || string(after) != string(before) {
		t.Errorf("the existing marker was rewritten: %v", err)
	}
}

func TestCreatePreservesExtensionsAndWritesHeading(t *testing.T) {
	// Arrange
	root := t.TempDir()
	if _, err := brain.Init(root, reference); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Act
	path, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindDecision, ID: "D-0001", Title: "Orders own their keys",
		BodyWithoutHeading: "## Decision\n\nOrders own their keys.\n",
		ExtensionFields:    map[string]any{"project": "checkout", "observed_on": "2026-01-02"},
		SourceExternal:     true,
		Now:                reference,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Assert
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !strings.Contains(text, "\n# D-0001 — Orders own their keys\n\n## Decision\n") {
		t.Errorf("the heading was not written once ahead of the body:\n%s", text)
	}
	if strings.Count(text, "\n# ") != 1 {
		t.Errorf("want exactly one H1:\n%s", text)
	}
	store, err := brain.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	record := store.ByID()["D-0001"]
	if record.Title != "Orders own their keys" || !record.SourceExternal || record.SourceRef != "" {
		t.Errorf("record read back wrong: %+v", record)
	}
	if record.Extensions["project"] != "checkout" || record.Extensions["observed_on"] != "2026-01-02" {
		t.Errorf("extensions not preserved as strings: %#v", record.Extensions)
	}
	if findings := brain.Check(store, brain.CheckPolicy{StaleAfterDays: 90, ReviewSLADays: 30}, reference); brain.Errors(findings) > 0 {
		t.Errorf("a created record fails check: %+v", findings)
	}
}

func TestCreateRejectsHeadingDefinedKeysAndEmptyKeys(t *testing.T) {
	cases := map[string]brain.NewRecordInput{
		"heading in body": {BodyWithoutHeading: "# Another title\n\ntext\n"},
		"defined key":     {ExtensionFields: map[string]any{"status": "active"}},
		"empty key":       {ExtensionFields: map[string]any{" ": "x"}},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			input.Kind, input.ID, input.Title, input.Now = brain.KindDecision, "D-0001", "A title", reference
			if _, err := brain.Create(root, input); err == nil {
				t.Fatal("accepted")
			}
			if entries, _ := os.ReadDir(filepath.Join(root, "decisions")); len(entries) != 0 {
				t.Errorf("wrote %d files before refusing", len(entries))
			}
		})
	}
}

func TestCreateRejectsSecretInMetadata(t *testing.T) {
	token := "AKIA" + strings.Repeat("Q", 16)
	cases := map[string]brain.NewRecordInput{
		"title":     {Title: "Collector key " + token},
		"body":      {BodyWithoutHeading: "the key is " + token + "\n"},
		"extension": {ExtensionFields: map[string]any{"source_url": "https://user:" + strings.Repeat("p", 8) + "@example.com/x"}},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			// Arrange
			root := t.TempDir()
			input.Kind, input.ID, input.Now = brain.KindDecision, "D-0001", reference
			if input.Title == "" {
				input.Title = "A title"
			}

			// Act
			_, err := brain.Create(root, input)

			// Assert
			if err == nil {
				t.Fatal("a record carrying a credential was written")
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), strings.Repeat("p", 8)) {
				t.Errorf("the refusal quotes the secret: %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(root, "decisions")); statErr == nil {
				entries, _ := os.ReadDir(filepath.Join(root, "decisions"))
				if len(entries) != 0 {
					t.Error("a file was written before the refusal")
				}
			}
		})
	}
}

func TestCreateRefusesUnusableExtensionKeyWithoutQuotingIt(t *testing.T) {
	root := t.TempDir()
	token := "xoxb-" + strings.Repeat("a", 20)
	_, err := brain.Create(root, brain.NewRecordInput{
		Kind: brain.KindDecision, ID: "D-0001", Title: "Example", Now: reference,
		ExtensionFields: map[string]any{" " + token: "x"},
	})
	if err == nil {
		t.Fatal("a key with surrounding space was written")
	}
	if strings.Contains(err.Error(), token[:7]) {
		t.Fatalf("refusal quoted the key: %v", err)
	}
}

func TestCreateRefusesCredentialTitleWithLegacyBody(t *testing.T) {
	token := "xoxb-" + strings.Repeat("a", 20)
	for name, input := range map[string]brain.NewRecordInput{
		"legacy body":     {Body: "# Safe heading\n\nSafe prose.\n"},
		"body no heading": {BodyWithoutHeading: "Safe prose.\n"},
		"default body":    {},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			input.Kind, input.ID, input.Title, input.Now = brain.KindDecision, "D-0001", token, reference
			relative, err := brain.Create(root, input)
			if err == nil {
				t.Fatalf("a credential title was accepted and written to %d-byte path", len(relative))
			}
			if strings.Contains(err.Error(), token[:7]) {
				t.Fatal("the refusal quoted the title")
			}
			entries, _ := os.ReadDir(filepath.Join(root, "decisions"))
			if len(entries) != 0 {
				t.Fatalf("refusal left %d files", len(entries))
			}
		})
	}
}

func TestArchiveCannotMoveThroughASymlinkedDirectory(t *testing.T) {
	root, _ := newStore(t)
	writeRecord(t, root, "decisions/D-0001-old.md", "id: D-0001\nstatus: superseded\nsuperseded_by: D-0002", "# Old")
	writeRecord(t, root, "decisions/D-0002-new.md", "id: D-0002\nstatus: active\nsupersedes: [D-0001]", "# New")
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(root, "archive")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "archive")); err != nil {
		t.Skip(err)
	}
	if _, err := brain.Archive(reload(t, root), true); err == nil {
		t.Error("archive moved a record through a symbolic link out of the brain")
	}
	if entries, _ := os.ReadDir(outside); len(entries) > 0 {
		t.Fatalf("archive wrote %s outside the brain", entries[0].Name())
	}
	if _, err := os.Stat(filepath.Join(root, "decisions", "D-0001-old.md")); err != nil {
		t.Errorf("the record left its place: %v", err)
	}
}
