package gitx_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takealook97/vat/internal/gitx"
)

func TestAuthorIdentityReadsConfigWithoutInventingMissingParts(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := newRepo(t)
	identity, err := gitx.AuthorIdentity(context.Background(), dir)
	if err != nil || identity != "" {
		t.Fatalf("unset: %q %v", identity, err)
	}
	run(t, dir, "config", "user.name", "Fixture Author")
	identity, err = gitx.AuthorIdentity(context.Background(), dir)
	if err != nil || identity != "" {
		t.Fatalf("partial: %q %v", identity, err)
	}
	run(t, dir, "config", "user.email", "author@example.com")
	identity, err = gitx.AuthorIdentity(context.Background(), dir)
	if err != nil || identity != "Fixture Author <author@example.com>" {
		t.Fatalf("configured: %q %v", identity, err)
	}
	run(t, dir, "config", "user.name", "two\nlines")
	if _, err := gitx.AuthorIdentity(context.Background(), dir); err == nil {
		t.Fatal("multiline identity accepted")
	}
	if _, err := gitx.AuthorIdentity(context.Background(), filepath.Join(dir, "absent")); err == nil {
		t.Fatal("config failure hidden")
	}
}

func TestMissingGitMeansUnsetAuthorIdentity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	identity, err := gitx.AuthorIdentity(context.Background(), dir)
	if err != nil || identity != "" {
		t.Fatalf("missing git: %q %v", identity, err)
	}
}
