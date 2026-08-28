package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/ui"
)

var testNow = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

// workspaceFixture drives real commands against a real workspace on disk. The CLI is the
// contract users depend on, so these exercise it end to end rather than
// asserting on the packages underneath.
type workspaceFixture struct {
	t    *testing.T
	root string
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, output)
	}
}

// newHarness creates a workspace root with the named repositories already
// cloned from local upstreams, so fetch and fast-forward are real operations.
func newFixture(t *testing.T, repos ...string) *workspaceFixture {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create: %v", err)
	}
	git(t, root, "init", "--quiet", "--initial-branch", "main", ".")

	for _, name := range repos {
		upstream := filepath.Join(base, "upstream", name)
		if err := os.MkdirAll(upstream, 0o755); err != nil {
			t.Fatalf("create: %v", err)
		}
		git(t, upstream, "init", "--quiet", "--initial-branch", "main", ".")
		if err := os.WriteFile(filepath.Join(upstream, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		git(t, upstream, "add", "-A")
		git(t, upstream, "commit", "--quiet", "-m", "init")
		git(t, root, "clone", "--quiet", upstream, name)
	}
	return &workspaceFixture{t: t, root: root}
}

// run executes one vat invocation and returns its exit code and output.
func (h *workspaceFixture) run(args ...string) (int, string) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     testNow,
		Cwd:     h.root,
		Root:    h.root,
		Yes:     true,
	}
	code := dispatch(context.Background(), env, Root(), args, nil)
	return code, out.String() + errOut.String()
}

// runSplit executes one invocation and keeps the two streams apart, which is
// the only way to assert which of them a message went to.
func (h *workspaceFixture) runSplit(args ...string) (int, string, string) {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     testNow,
		Cwd:     h.root,
		Root:    h.root,
		JSON:    true,
		Yes:     true,
	}
	code := dispatch(context.Background(), env, Root(), args, nil)
	return code, out.String(), errOut.String()
}

// runJSON executes an invocation with --json and decodes the payload.
func (h *workspaceFixture) runJSON(target any, args ...string) int {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     testNow,
		Cwd:     h.root,
		Root:    h.root,
		JSON:    true,
		Yes:     true,
	}
	code := dispatch(context.Background(), env, Root(), args, nil)
	if strings.TrimSpace(out.String()) == "" {
		h.t.Fatalf("--json produced no output for %v (stderr: %s)", args, errOut.String())
	}
	if err := json.Unmarshal(out.Bytes(), target); err != nil {
		h.t.Fatalf("--json output for %v is not valid JSON: %v\n%s", args, err, out.String())
	}
	return code
}

func (h *workspaceFixture) mustRun(args ...string) string {
	h.t.Helper()
	code, output := h.run(args...)
	if code != ExitOK {
		h.t.Fatalf("`vat %s` exited %d, want 0:\n%s", strings.Join(args, " "), code, output)
	}
	return output
}

// upstream is the local origin newFixture cloned a repository from, so a test
// can enrol it without inventing a URL that does not resolve.
func (h *workspaceFixture) upstream(name string) string {
	return filepath.Join(filepath.Dir(h.root), "upstream", name)
}

func (h *workspaceFixture) path(parts ...string) string {
	return filepath.Join(append([]string{h.root}, parts...)...)
}

func commitAll(t *testing.T, h *workspaceFixture, name string) {
	t.Helper()
	git(t, h.path(name), "add", "-A")
	git(t, h.path(name), "commit", "--quiet", "-m", "adopt")
}

// addCheck gives a repository a canonical check so a changeset can verify it.
func addCheck(t *testing.T, h *workspaceFixture, name, command string) {
	t.Helper()
	path := h.path("vat.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	updated := strings.Replace(string(content),
		"    - name: "+name+"\n",
		"    - name: "+name+"\n      checks:\n        - "+command+"\n", 1)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
