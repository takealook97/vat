package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/ui"
)

// runWithStdin runs one invocation against the fixture with stdin supplied.
func (h *workspaceFixture) runWithStdin(stdin string, args ...string) (int, string) {
	h.t.Helper()
	return h.runWithReader(strings.NewReader(stdin), args...)
}

func (h *workspaceFixture) runWithReader(stdin interface{ Read([]byte) (int, error) }, args ...string) (int, string) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     testNow, Cwd: h.root, Root: h.root, Yes: true, Stdin: stdin,
	}
	code := dispatch(context.Background(), env, Root(), args, nil)
	return code, out.String() + errOut.String()
}

func recordFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func TestBrainNewReadsBodyAndExtensions(t *testing.T) {
	// Arrange
	h := brainFixture(t)

	// Act
	code, output := h.runWithStdin("## Lesson\n\nRetries need a key.\n",
		"brain", "new", "memory", "--title", "Retries need a key", "--body-stdin",
		"--set", "project=checkout", "--set", "observed_on=2026-01-02", "--set", "count=12",
		"--source-external")

	// Assert
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, output)
	}
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	record := store.ByID()["M-0001"]
	if record.Title != "Retries need a key" || !strings.HasPrefix(record.Body, "# M-0001 — Retries need a key\n\n## Lesson\n") {
		t.Errorf("heading not written ahead of the piped body:\n%s", record.Body)
	}
	for key, want := range map[string]string{"project": "checkout", "observed_on": "2026-01-02", "count": "12"} {
		if record.Extensions[key] != want {
			t.Errorf("%s = %#v, want the string %q", key, record.Extensions[key], want)
		}
	}
	if !record.SourceExternal || record.SourceRef != "" {
		t.Errorf("source_external not recorded alone: %+v", record.Metadata)
	}
	h.mustRun("brain", "build")
	h.mustRun("brain", "check")
}

func TestBrainNewKeepsTheDefaultBodyWithoutStdin(t *testing.T) {
	h := brainFixture(t)
	h.mustRun("brain", "new", "decision", "--title", "A default body", "--set", "project=checkout")
	store, err := brain.Load(h.path("brain"))
	if err != nil {
		t.Fatal(err)
	}
	record := store.ByID()["D-0001"]
	if !strings.Contains(record.Body, "## Reversal") || record.Extensions["project"] != "checkout" {
		t.Errorf("default body or extension lost: %+v\n%s", record.Extensions, record.Body)
	}
}

func TestBrainNewRejectsHeadingAndReservedKeys(t *testing.T) {
	h := brainFixture(t)
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"heading", "# My own title\n\ntext\n", []string{"--body-stdin"}, "H1"},
		{"empty body", "  \n", []string{"--body-stdin"}, "read nothing"},
		{"status", "", []string{"--set", "status=active"}, "status is a field vat defines"},
		{"reviewed_by", "", []string{"--set", "reviewed_by=me"}, "reviewed_by is a field vat defines"},
		{"empty key", "", []string{"--set", "=x"}, "has no key"},
		{"no equals", "", []string{"--set", "project"}, "key=value"},
		{"repeated", "", []string{"--set", "a=1", "--set", "a=2"}, "given twice"},
		{"external and owner", "", []string{"--source-external", "--owner", "brain"}, "--source-external"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"brain", "new", "decision", "--title", "x"}, tc.args...)
			code, output := h.runWithStdin(tc.stdin, args...)
			if code != ExitUsage || !strings.Contains(output, tc.want) {
				t.Errorf("exit %d, want %d naming %q:\n%s", code, ExitUsage, tc.want, output)
			}
		})
	}
	// Every bad --set at once.
	code, output := h.run("brain", "new", "decision", "--title", "x", "--set", "status=a", "--set", "=b")
	if code != ExitUsage || !strings.Contains(output, "status") || !strings.Contains(output, "no key") {
		t.Errorf("not every bad --set was named (exit %d):\n%s", code, output)
	}
	if files := recordFiles(t, h.path("brain", "decisions")); len(files) != 0 {
		t.Errorf("refusals wrote %v", files)
	}
}

func TestBrainNewRejectsSecretsBeforeAnyWrite(t *testing.T) {
	token := "gh" + "p_" + strings.Repeat("A", 36)
	h := brainFixture(t)
	cases := []struct {
		name  string
		stdin string
		args  []string
	}{
		{"body", "the token is " + token + "\n", []string{"--title", "x", "--body-stdin"}},
		{"title", "", []string{"--title", "token " + token}},
		{"extension", "", []string{"--title", "x", "--set", "channel_url=https://bot:" + strings.Repeat("s", 9) + "@example.com/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"brain", "new", "decision"}, tc.args...)
			code, output := h.runWithStdin(tc.stdin, args...)
			if code == ExitOK {
				t.Fatalf("a record carrying a credential was written:\n%s", output)
			}
			if strings.Contains(output, token) || strings.Contains(output, strings.Repeat("s", 9)) {
				t.Errorf("the refusal printed the secret:\n%s", output)
			}
			if !strings.Contains(output, "nothing was written") {
				t.Errorf("the refusal does not say nothing was written:\n%s", output)
			}
			if files := recordFiles(t, h.path("brain", "decisions")); len(files) != 0 {
				t.Errorf("wrote %v before refusing", files)
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestBrainNewReportsAnUnreadableStdin(t *testing.T) {
	h := brainFixture(t)
	code, output := h.runWithReader(failingReader{}, "brain", "new", "decision", "--title", "x", "--body-stdin")
	if code != ExitFindings || !strings.Contains(output, "standard input") {
		t.Errorf("exit %d:\n%s", code, output)
	}
	if files := recordFiles(t, h.path("brain", "decisions")); len(files) != 0 {
		t.Errorf("wrote %v", files)
	}
}

func TestBrainNewRefusesAnExistingFile(t *testing.T) {
	h := brainFixture(t)
	h.mustRun("brain", "new", "decision", "--title", "Same", "--id", "D-0009")
	code, output := h.runWithStdin("body\n", "brain", "new", "decision", "--title", "Same", "--id", "D-0009", "--body-stdin")
	if code == ExitOK || !strings.Contains(output, "already exists") {
		t.Errorf("exit %d:\n%s", code, output)
	}
}

// The write goes through fsx.WriteFileAtomic: a directory that cannot take the
// temporary file leaves no partial record behind.
func TestBrainNewLeavesNothingWhenTheWriteFails(t *testing.T) {
	h := brainFixture(t)
	dir := h.path("brain", "decisions")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o644); err == nil {
		_ = os.Remove(probe)
		t.Skip("this platform or user ignores directory permissions")
	}
	code, output := h.runWithStdin("body\n", "brain", "new", "decision", "--title", "x", "--body-stdin")
	if code == ExitOK {
		t.Fatalf("a write into a read-only directory reported success:\n%s", output)
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.Name() != ".gitkeep" {
			t.Errorf("left %s behind", entry.Name())
		}
	}
}

// source_url and source_quote were extension keys before they were fields, so
// a script written then still passes them through --set. The refusal has to
// name the flag that now writes them, or the script's author is left guessing.
func TestBrainNewPointsDefinedSourceFieldsAtTheirFlags(t *testing.T) {
	h := brainFixture(t)
	code, output := h.run("brain", "new", "memory", "--title", "x",
		"--set", "source_url=https://example.com/a", "--set", "source_quote=said so")
	if code != ExitUsage {
		t.Fatalf("a defined field passed through --set (exit %d):\n%s", code, output)
	}
	// The usage line names every flag, so each refusal line must name its own.
	for field, flag := range map[string]string{"source_url": "--source-url", "source_quote": "--source-quote"} {
		named := false
		for _, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "--set "+field) && strings.Contains(line, flag) {
				named = true
			}
		}
		if !named {
			t.Errorf("the refusal of %s does not point at %s:\n%s", field, flag, output)
		}
	}
	if files := recordFiles(t, h.path("brain", "memory")); len(files) != 0 {
		t.Errorf("a refused record was written: %v", files)
	}
	for field := range definedFieldFlags {
		if !brain.IsDefinedField(field) {
			t.Errorf("%s is mapped to a flag but is not a defined field", field)
		}
	}
}
