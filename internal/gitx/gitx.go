// Package gitx wraps the git command line. vat shells out rather than linking a
// git library so that a user's own git configuration, credential helpers, and
// hooks apply unchanged — the workspace must behave the same whether a human or
// vat drives it.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ErrNotRepository is returned when a directory has no git repository.
var ErrNotRepository = errors.New("not a git repository")

// CommandError carries the failing git invocation and its stderr, so callers
// can report what git actually said instead of a generic exit status.
type CommandError struct {
	Args   []string
	Dir    string
	Stderr string
	Err    error
}

// Error redacts before it formats. A failing `git push` puts the remote URL in
// both the arguments and git's own stderr, and this string is printed by every
// command that surfaces a git failure.
func (e *CommandError) Error() string {
	detail := Redact(strings.TrimSpace(e.Stderr))
	if detail == "" {
		detail = Redact(e.Err.Error())
	}
	args := make([]string, len(e.Args))
	for i, arg := range e.Args {
		args[i] = Redact(arg)
	}
	return fmt.Sprintf("git %s (in %s): %s", strings.Join(args, " "), e.Dir, detail)
}

func (e *CommandError) Unwrap() error { return e.Err }

// Run executes git in dir and returns trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Keep git non-interactive: a prompt inside a parallel sync would hang the
	// whole workspace with no indication of which repository is waiting.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stdout.String()), &CommandError{
			Args: args, Dir: dir, Stderr: stderr.String(), Err: err,
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Available reports whether a git executable is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Version returns git's reported version string.
func Version(ctx context.Context) (string, error) {
	out, err := Run(ctx, ".", "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(out, "git version "), nil
}

// IsRepository reports whether dir is the root of a git working tree. A nested
// path inside another repository is deliberately not accepted: the workspace
// expects each governed directory to own its own .git.
func IsRepository(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

// RemoteURL returns the configured URL of the named remote.
func RemoteURL(ctx context.Context, dir, remote string) (string, error) {
	return Run(ctx, dir, "remote", "get-url", remote)
}

// SetRemoteURL points an existing remote at a new URL.
func SetRemoteURL(ctx context.Context, dir, remote, url string) error {
	_, err := Run(ctx, dir, "remote", "set-url", remote, url)
	return err
}

// CurrentBranch returns the checked-out branch, or an empty string when HEAD is
// detached.
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		var cmdErr *CommandError
		// A detached HEAD is a normal state, not an error to propagate.
		if errors.As(err, &cmdErr) && strings.TrimSpace(cmdErr.Stderr) == "" {
			return "", nil
		}
		return "", err
	}
	return out, nil
}

// HeadRevision returns the full commit hash at HEAD.
func HeadRevision(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "HEAD")
}

// ShortRevision returns the abbreviated commit hash at the given revision.
func ShortRevision(ctx context.Context, dir, rev string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--short", rev)
}

// IsDirty reports whether the working tree has uncommitted changes, including
// untracked files.
func IsDirty(ctx context.Context, dir string) (bool, error) {
	out, err := Run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// HasRef reports whether a ref such as refs/remotes/origin/main resolves.
func HasRef(ctx context.Context, dir, ref string) bool {
	_, err := Run(ctx, dir, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// Divergence is how far a local branch has moved away from its upstream.
type Divergence struct {
	Ahead  int
	Behind int
}

// Diverged reports whether both sides hold commits the other does not.
func (d Divergence) Diverged() bool { return d.Ahead > 0 && d.Behind > 0 }

// InSync reports whether the two refs point at the same commit.
func (d Divergence) InSync() bool { return d.Ahead == 0 && d.Behind == 0 }

// AheadBehind counts commits between local and upstream.
func AheadBehind(ctx context.Context, dir, local, upstream string) (Divergence, error) {
	out, err := Run(ctx, dir, "rev-list", "--left-right", "--count",
		fmt.Sprintf("%s...%s", local, upstream))
	if err != nil {
		return Divergence{}, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return Divergence{}, fmt.Errorf("unexpected rev-list output %q in %s", out, dir)
	}
	ahead, err := strconv.Atoi(fields[0])
	if err != nil {
		return Divergence{}, fmt.Errorf("parse ahead count %q: %w", fields[0], err)
	}
	behind, err := strconv.Atoi(fields[1])
	if err != nil {
		return Divergence{}, fmt.Errorf("parse behind count %q: %w", fields[1], err)
	}
	return Divergence{Ahead: ahead, Behind: behind}, nil
}

// Fetch updates remote-tracking refs and prunes deleted ones.
func Fetch(ctx context.Context, dir, remote string) error {
	_, err := Run(ctx, dir, "fetch", remote, "--prune", "--quiet")
	return err
}

// FastForward advances the current branch to upstream, refusing anything that
// would create a merge commit.
func FastForward(ctx context.Context, dir, upstream string) error {
	_, err := Run(ctx, dir, "merge", "--ff-only", upstream)
	return err
}

// Clone copies a repository into dir.
func Clone(ctx context.Context, url, dir string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", parent, err)
	}
	_, err := Run(ctx, parent, "clone", "--quiet", url, filepath.Base(dir))
	return err
}

// Init creates a new repository with the given initial branch.
func Init(ctx context.Context, dir, branch string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	_, err := Run(ctx, dir, "init", "--quiet", "--initial-branch", branch)
	return err
}

// AddRemote registers a new remote.
func AddRemote(ctx context.Context, dir, remote, url string) error {
	_, err := Run(ctx, dir, "remote", "add", remote, url)
	return err
}

// UnpushedCommits counts commits on the current branch that no remote-tracking
// ref contains. It is what makes `vat repo remove` refuse to delete work that
// exists only on this machine.
func UnpushedCommits(ctx context.Context, dir string) (int, error) {
	out, err := Run(ctx, dir, "rev-list", "--count", "HEAD", "--not", "--remotes")
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse unpushed count %q: %w", out, err)
	}
	return count, nil
}

// StashCount returns how many stash entries the repository holds. Stashes are
// invisible to `git status`, so removing a repository without checking them
// loses work silently.
func StashCount(ctx context.Context, dir string) (int, error) {
	out, err := Run(ctx, dir, "stash", "list")
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(out) == "" {
		return 0, nil
	}
	return len(strings.Split(out, "\n")), nil
}

// LastCommitDate returns the committer date of HEAD in RFC3339 form.
func LastCommitDate(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "log", "-1", "--format=%cI")
}

// FileAtRevision returns a file's content at a specific revision without
// touching the working tree.
func FileAtRevision(ctx context.Context, dir, rev, path string) (string, error) {
	return Run(ctx, dir, "show", fmt.Sprintf("%s:%s", rev, path))
}

// RevisionExists reports whether a commit-ish resolves in the repository.
func RevisionExists(ctx context.Context, dir, rev string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return err == nil
}

// ToplevelOf returns the working-tree root containing dir, if any.
func ToplevelOf(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", ErrNotRepository
	}
	return out, nil
}

// NormaliseURL reduces cosmetic differences between two spellings of the same
// remote so a comparison does not report a mismatch for a trailing ".git" or a
// scp-style SSH address. It is only used for comparison, never for rewriting a
// configured remote: an actual mismatch is treated as a supply-chain signal.
//
// Two things are deliberately preserved. The path keeps its case, because on a
// case-sensitive host "acme/Payments" and "acme/payments" are different
// repositories. And "http" is not folded into "https", because silently
// accepting a downgrade to an unauthenticated transport is exactly the
// substitution this comparison exists to catch.
func NormaliseURL(url string) string {
	trimmed := strings.TrimSpace(url)
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	trimmed = stripUserinfo(trimmed)

	scheme := "ssh"
	switch {
	case strings.HasPrefix(trimmed, "git@"):
		if host, path, ok := strings.Cut(strings.TrimPrefix(trimmed, "git@"), ":"); ok {
			trimmed = host + "/" + path
		}
	case strings.HasPrefix(trimmed, "ssh://"):
		trimmed = strings.TrimPrefix(trimmed, "ssh://")
	case strings.HasPrefix(trimmed, "https://"):
		scheme, trimmed = "https", strings.TrimPrefix(trimmed, "https://")
	case strings.HasPrefix(trimmed, "http://"):
		scheme, trimmed = "http", strings.TrimPrefix(trimmed, "http://")
	default:
		scheme = "file"
	}
	// SSH and HTTPS are two authenticated routes to the same repository, so
	// they compare equal; plain HTTP does not.
	if scheme == "ssh" {
		scheme = "https"
	}

	host, path, _ := strings.Cut(trimmed, "/")
	return scheme + "://" + strings.ToLower(host) + "/" + path
}

// stripUserinfo removes any "user:token@" prefix from the authority.
func stripUserinfo(url string) string {
	scheme, rest, ok := strings.Cut(url, "://")
	if !ok {
		return url
	}
	authority, remainder, hasPath := strings.Cut(rest, "/")
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		authority = authority[at+1:]
	}
	if hasPath {
		return scheme + "://" + authority + "/" + remainder
	}
	return scheme + "://" + authority
}

// credentialInURL matches the userinfo of any URL, wherever it appears in a
// larger string. An scp-style address ("git@host:path") is deliberately not
// matched: it carries a user name and never a secret.
var credentialInURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]*@`)

// Redact removes credentials from anything that may contain a URL.
//
// A remote can carry a token in its authority ("https://x-token:ghp_...@host").
// vat reports a remote mismatch by showing both URLs, and git's own stderr
// quotes the remote it failed to reach — neither may become the one place a
// credential is disclosed. It takes any string, not only a bare URL, because
// the strings that leak are error messages with a URL somewhere inside them.
func Redact(text string) string {
	return credentialInURL.ReplaceAllString(text, "${1}***@")
}

// WithoutCredentials returns a URL with any userinfo removed rather than
// masked, for recording rather than printing. The manifest is committed, so it
// holds identity and never access.
func WithoutCredentials(url string) string {
	return stripUserinfo(strings.TrimSpace(url))
}

// SameRemote reports whether two URLs designate the same repository.
func SameRemote(a, b string) bool { return NormaliseURL(a) == NormaliseURL(b) }
