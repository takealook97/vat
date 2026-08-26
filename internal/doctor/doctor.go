// Package doctor judges an environment. It never repairs one.
//
// The separation matters: a tool that silently fixes what it finds teaches you
// nothing about why it broke, and on a machine holding credentials and
// unpushed work, "fixing" is how data is lost. doctor reports; you decide.
//
// Nothing here ever prints a secret. Findings about credentials are limited to
// existence, file permissions, and age.
package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/workspace"
)

// Sections group findings in the order doctor reports them.
const (
	sectionTools        = "tools"
	sectionWorkspace    = "workspace"
	sectionRepositories = "repositories"
	sectionCredentials  = "credentials"
	sectionBrain        = "brain"
	sectionChangesets   = "changesets"
	sectionNetwork      = "network"
)

// Status is the outcome of one check.
type Status string

const (
	// StatusOK means the check passed.
	StatusOK Status = "ok"
	// StatusWarn means something needs a human's attention but nothing is broken.
	StatusWarn Status = "warn"
	// StatusFail means the environment cannot be relied on.
	StatusFail Status = "fail"
)

// Finding is one observation.
type Finding struct {
	Section string `json:"section"`
	Subject string `json:"subject"`
	Status  Status `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

// Report is a whole diagnosis.
type Report struct {
	Findings []Finding `json:"findings"`
	Failures int       `json:"failures"`
	Warnings int       `json:"warnings"`
}

// Options configure a run.
type Options struct {
	// Network permits read-only reachability checks.
	Network bool
	// Now is the reference time, injected for deterministic tests.
	Now time.Time
	// SecretMaxAgeDays reports credential material older than this. Static
	// long-lived secrets that are never rotated stop being an asset and become
	// a liability, and nothing else in a workspace tracks their age.
	SecretMaxAgeDays int
}

// Run diagnoses the workspace and the machine it sits on.
func Run(ctx context.Context, ws *workspace.Workspace, opts Options) Report {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	var report Report
	add := func(findings ...Finding) { report.Findings = append(report.Findings, findings...) }

	add(checkTools(ctx)...)
	add(checkWorkspace(ws)...)
	add(checkRepos(ctx, ws)...)
	add(checkSecrets(ws, now, opts.SecretMaxAgeDays)...)
	add(checkBrain(ws, now)...)
	add(checkChangesets(ws, now)...)
	if opts.Network {
		add(checkNetwork(ctx)...)
	}

	for _, finding := range report.Findings {
		switch finding.Status {
		case StatusFail:
			report.Failures++
		case StatusWarn:
			report.Warnings++
		}
	}
	return report
}

func checkTools(ctx context.Context) []Finding {
	findings := []Finding{}
	if !gitx.Available() {
		return append(findings, Finding{
			Section: sectionTools, Subject: "git", Status: StatusFail,
			Detail: "not found on PATH; vat cannot do anything without it",
		})
	}
	version, err := gitx.Version(ctx)
	if err != nil {
		findings = append(findings, Finding{
			Section: sectionTools, Subject: "git", Status: StatusWarn, Detail: err.Error(),
		})
	} else {
		findings = append(findings, Finding{
			Section: sectionTools, Subject: "git", Status: StatusOK, Detail: version,
		})
	}
	for _, tool := range []struct{ name, why string }{
		{"gh", "needed by `vat repo new` to create a remote repository"},
		{"sops", "needed to verify an encrypted credential repository"},
		{"age", "needed to decrypt credential material"},
	} {
		if _, err := exec.LookPath(tool.name); err != nil {
			findings = append(findings, Finding{
				Section: sectionTools, Subject: tool.name, Status: StatusWarn,
				Detail: "not installed — " + tool.why,
			})
			continue
		}
		findings = append(findings, Finding{
			Section: sectionTools, Subject: tool.name, Status: StatusOK,
		})
	}
	return findings
}

func checkWorkspace(ws *workspace.Workspace) []Finding {
	findings := []Finding{
		{Section: sectionWorkspace, Subject: manifest.FileName, Status: StatusOK,
			Detail: fmt.Sprintf("%d repositories, %d archived",
				len(ws.Manifest.Active()), len(ws.Manifest.Repos)-len(ws.Manifest.Active()))},
	}
	if !gitx.IsRepository(ws.Root) {
		findings = append(findings, Finding{
			Section: sectionWorkspace, Subject: "root repository", Status: StatusWarn,
			Detail: "the workspace root is not versioned, so the manifest has no history",
			Fix:    "git init",
		})
	}
	missing, err := ws.GitignoreDrift(ws.Manifest)
	switch {
	case err != nil:
		findings = append(findings, Finding{
			Section: sectionWorkspace, Subject: ".gitignore", Status: StatusWarn, Detail: err.Error(),
		})
	case len(missing) > 0:
		findings = append(findings, Finding{
			Section: sectionWorkspace, Subject: ".gitignore", Status: StatusFail,
			Detail: fmt.Sprintf("%s not excluded; a root commit would absorb them",
				strings.Join(missing, ", ")),
			Fix: "vat lint --fix",
		})
	default:
		findings = append(findings, Finding{
			Section: sectionWorkspace, Subject: ".gitignore", Status: StatusOK,
		})
	}
	return findings
}

func checkRepos(ctx context.Context, ws *workspace.Workspace) []Finding {
	var findings []Finding
	for _, repo := range ws.Manifest.Active() {
		dir := ws.RepoPath(repo)
		if !fsx.IsDir(dir) {
			status := StatusWarn
			if repo.Required {
				status = StatusFail
			}
			findings = append(findings, Finding{
				Section: sectionRepositories, Subject: repo.Name, Status: status,
				Detail: "not cloned", Fix: "vat sync",
			})
			continue
		}
		if !gitx.IsRepository(dir) {
			findings = append(findings, Finding{
				Section: sectionRepositories, Subject: repo.Name, Status: StatusFail,
				Detail: "directory exists but holds no git repository",
			})
			continue
		}
		actual, err := gitx.RemoteURL(ctx, dir, "origin")
		if err != nil || !gitx.SameRemote(actual, repo.Origin) {
			findings = append(findings, Finding{
				Section: sectionRepositories, Subject: repo.Name, Status: StatusFail,
				Detail: fmt.Sprintf("origin is %q, manifest says %q",
					gitx.Redact(actual), gitx.Redact(repo.Origin)),
			})
			continue
		}
		branch, err := gitx.CurrentBranch(ctx, dir)
		if err != nil {
			// A repository git cannot answer for is a finding, not a clean one.
			findings = append(findings, Finding{
				Section: sectionRepositories, Subject: repo.Name, Status: StatusFail,
				Detail: "git cannot read this repository",
			})
			continue
		}
		dirty, err := gitx.IsDirty(ctx, dir)
		if err != nil {
			findings = append(findings, Finding{
				Section: sectionRepositories, Subject: repo.Name, Status: StatusFail,
				Detail: "git cannot read the working tree state",
			})
			continue
		}
		expected := repo.Branch(ws.Manifest.Workspace.DefaultBranch)
		detail := branch
		if branch == "" {
			detail = "detached HEAD"
		}
		status := StatusOK
		if branch != expected {
			status = StatusWarn
			detail += fmt.Sprintf(" (default is %s)", expected)
		}
		if dirty {
			status = StatusWarn
			detail += ", uncommitted changes"
		}
		findings = append(findings, Finding{
			Section: sectionRepositories, Subject: repo.Name, Status: status, Detail: detail,
		})
	}
	return findings
}

// checkSecrets inspects the credential repository without ever reading a value.
func checkSecrets(ws *workspace.Workspace, now time.Time, maxAgeDays int) []Finding {
	var credential *manifest.Repo
	for _, repo := range ws.Manifest.Active() {
		if repo.Role == manifest.RoleCredential {
			copied := repo
			credential = &copied
			break
		}
	}
	if credential == nil {
		return nil
	}
	dir := ws.RepoPath(*credential)
	if !fsx.IsDir(dir) {
		return []Finding{{
			Section: sectionCredentials, Subject: credential.Name, Status: StatusWarn,
			Detail: "not cloned", Fix: "vat sync",
		}}
	}

	findings := []Finding{}
	plaintext, encrypted, oldest := scanCredentialRepo(dir)

	if len(plaintext) > 0 {
		sort.Strings(plaintext)
		shown := plaintext
		if len(shown) > 5 {
			shown = shown[:5]
		}
		findings = append(findings, Finding{
			Section: sectionCredentials, Subject: credential.Name, Status: StatusFail,
			Detail: fmt.Sprintf("%d file(s) look like unencrypted secrets: %s",
				len(plaintext), strings.Join(shown, ", ")),
			Fix: "encrypt them before committing; a private repository is not encryption",
		})
	}
	findings = append(findings, Finding{
		Section: sectionCredentials, Subject: "encrypted files", Status: StatusOK,
		Detail: fmt.Sprintf("%d tracked", encrypted),
	})
	findings = append(findings, checkCredentialPermissions(dir, credential.Name)...)

	if maxAgeDays > 0 && !oldest.IsZero() {
		age := int(now.Sub(oldest).Hours() / 24)
		if age > maxAgeDays {
			// Recovery procedures are usually documented; rotation almost never
			// is. An unrotated long-lived secret is the quiet half of a
			// credential system.
			findings = append(findings, Finding{
				Section: sectionCredentials, Subject: "rotation", Status: StatusWarn,
				Detail: fmt.Sprintf("oldest encrypted file last changed %d days ago (limit %d)",
					age, maxAgeDays),
				Fix: "rotate it, then re-encrypt; record the date so this check resets",
			})
		} else {
			findings = append(findings, Finding{
				Section: sectionCredentials, Subject: "rotation", Status: StatusOK,
				Detail: fmt.Sprintf("oldest change %d days ago", age),
			})
		}
	}
	return findings
}

// checkCredentialPermissions reports credential material any other user on the
// machine can read.
//
// Encryption is the primary defence and this is the second one: a decryption
// key or an unencrypted leftover sitting at 0644 is readable by every account
// on a shared machine. Only the mode is reported, never a file's contents.
func checkCredentialPermissions(dir, name string) []Finding {
	if runtime.GOOS == "windows" {
		// Windows has no POSIX permission bits; os.Chmod there toggles a
		// read-only attribute, so a mode would describe an attribute rather
		// than an access control. docs/SECURITY_MODEL.md says so.
		return nil
	}
	var exposed []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil //nolint:nilerr // an unreadable entry is reported elsewhere
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode().Perm()&0o077 == 0 {
			return nil
		}
		if !looksLikeKeyMaterial(info.Name()) {
			return nil
		}
		relative, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			relative = info.Name()
		}
		exposed = append(exposed, fmt.Sprintf("%s (%o)", relative, info.Mode().Perm()))
		return nil
	})
	if len(exposed) == 0 {
		return []Finding{{
			Section: sectionCredentials, Subject: "permissions", Status: StatusOK,
			Detail: "no key material is readable by other users",
		}}
	}
	sort.Strings(exposed)
	shown := exposed
	if len(shown) > 5 {
		shown = shown[:5]
	}
	return []Finding{{
		Section: sectionCredentials, Subject: "permissions", Status: StatusFail,
		Detail: fmt.Sprintf("%d file(s) readable by other users: %s",
			len(exposed), strings.Join(shown, ", ")),
		Fix: fmt.Sprintf("chmod 600 the files above, inside %s", name),
	}}
}

// keyMaterialNames are the files whose exposure actually matters: private keys
// and anything holding a decrypted value. Ciphertext at 0644 is not a finding —
// that is what encryption is for.
var keyMaterialNames = []string{
	".key", ".pem", "id_rsa", "id_ed25519", "id_ecdsa", ".age", "keys.txt",
	".env", "credentials.json", "service-account.json",
}

func looksLikeKeyMaterial(name string) bool {
	for _, marker := range encryptedMarkers {
		if strings.Contains(name, marker) {
			return false
		}
	}
	for _, marker := range keyMaterialNames {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

// plaintextSecretNames are file names that should never appear in a credential
// repository, because their conventional content is an unencrypted secret.
var plaintextSecretNames = []string{
	".env", ".env.local", ".env.production", "id_rsa", "id_ed25519",
	"credentials.json", "service-account.json", ".npmrc", ".pypirc",
}

var encryptedMarkers = []string{".sops.", ".enc.", ".age", ".gpg", ".asc"}

func scanCredentialRepo(dir string) (plaintext []string, encrypted int, oldest time.Time) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil //nolint:nilerr // an unreadable entry is reported by other checks
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		isEncrypted := false
		for _, marker := range encryptedMarkers {
			if strings.Contains(name, marker) {
				isEncrypted = true
				break
			}
		}
		if isEncrypted {
			encrypted++
			if oldest.IsZero() || info.ModTime().Before(oldest) {
				oldest = info.ModTime()
			}
			return nil
		}
		for _, suspicious := range plaintextSecretNames {
			if name == suspicious || strings.HasPrefix(name, suspicious+".") {
				rel, _ := filepath.Rel(dir, path)
				plaintext = append(plaintext, rel)
				return nil
			}
		}
		return nil
	})
	return plaintext, encrypted, oldest
}

func checkBrain(ws *workspace.Workspace, now time.Time) []Finding {
	root, ok := ws.BrainPath()
	if !ok || !fsx.IsDir(root) {
		return nil
	}
	store, err := brain.Load(root)
	if err != nil {
		return []Finding{{
			Section: sectionBrain, Subject: "records", Status: StatusFail, Detail: err.Error(),
		}}
	}
	policy := brain.CheckPolicy{
		StaleAfterDays: ws.Manifest.Policy.Brain.StaleAfterDays,
		ReviewSLADays:  ws.Manifest.Policy.Brain.ReviewSLADays,
	}
	findings := []Finding{{
		Section: sectionBrain, Subject: "records", Status: StatusOK,
		Detail: fmt.Sprintf("%d total, %d citable", len(store.Records), len(store.Answerable())),
	}}

	queue := brain.ReviewQueue(store, policy, now)
	overdue := 0
	for _, item := range queue {
		if item.Overdue {
			overdue++
		}
	}
	switch {
	case overdue > 0:
		findings = append(findings, Finding{
			Section: sectionBrain, Subject: "review queue", Status: StatusWarn,
			Detail: fmt.Sprintf("%d of %d past the %d-day window",
				overdue, len(queue), policy.ReviewSLADays),
			Fix: "vat brain review",
		})
	case len(queue) > 0:
		findings = append(findings, Finding{
			Section: sectionBrain, Subject: "review queue", Status: StatusOK,
			Detail: fmt.Sprintf("%d items, none overdue", len(queue)),
		})
	default:
		findings = append(findings, Finding{
			Section: sectionBrain, Subject: "review queue", Status: StatusOK, Detail: "empty",
		})
	}

	drifted, err := brain.Drift(store, now)
	if err == nil && len(drifted) > 0 {
		findings = append(findings, Finding{
			Section: sectionBrain, Subject: "generated files", Status: StatusWarn,
			Detail: strings.Join(drifted, ", ") + " out of date",
			Fix:    "vat brain build",
		})
	}
	return findings
}

func checkChangesets(ws *workspace.Workspace, now time.Time) []Finding {
	sets, err := changeset.LoadAll(ws.Root)
	if err != nil {
		return []Finding{{
			Section: sectionChangesets, Subject: "records", Status: StatusWarn, Detail: err.Error(),
		}}
	}
	if len(sets) == 0 {
		return nil
	}
	open, stale := 0, 0
	limit := ws.Manifest.Policy.Changeset.MaxOpenDays
	for _, set := range sets {
		if !set.Status.Open() {
			continue
		}
		open++
		if limit > 0 && set.AgeDays(now) > limit {
			stale++
		}
	}
	status := StatusOK
	detail := fmt.Sprintf("%d open of %d", open, len(sets))
	if stale > 0 {
		status = StatusWarn
		detail += fmt.Sprintf(", %d past the %d-day limit", stale, limit)
	}
	return []Finding{{
		Section: sectionChangesets, Subject: "open work", Status: status, Detail: detail,
	}}
}

func checkNetwork(ctx context.Context) []Finding {
	findings := []Finding{}
	if _, err := exec.LookPath("gh"); err == nil {
		cmd := exec.CommandContext(ctx, "gh", "auth", "status")
		if err := cmd.Run(); err != nil {
			findings = append(findings, Finding{
				Section: sectionNetwork, Subject: "github auth", Status: StatusWarn,
				Detail: "not authenticated", Fix: "gh auth login",
			})
		} else {
			findings = append(findings, Finding{
				Section: sectionNetwork, Subject: "github auth", Status: StatusOK,
			})
		}
	}
	findings = append(findings, Finding{
		Section: sectionNetwork, Subject: "platform", Status: StatusOK,
		Detail: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	})
	return findings
}
