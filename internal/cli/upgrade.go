package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/version"
)

// latestReleaseURL is the forge's answer to "what is published". It is read for
// one field, so there is no client library and no dependency: a release feed is
// not worth widening the import graph of a tool whose dependency count is a
// security property.
const latestReleaseURL = "https://api.github.com/repos/takealook97/vat/releases/latest"

// lookupTimeout bounds the one network call. A command that hangs is worse than
// one that says it could not find out.
const lookupTimeout = 5 * time.Second

// upgradeCommand is separate from `vat doctor` because diagnosis and repair are
// separate commands here. Doctor reports that a newer release exists; this acts
// on it, and somebody has to ask for it.
//
// It does not replace its own binary. Every install path vat supports is owned
// by something that already tracks what it put there — Homebrew keeps a
// manifest of the files in its Cellar, `go install` records the module version
// — and a binary that overwrites itself leaves both describing a file they did
// not write. So this runs the upgrade the installer would run, and when it
// cannot tell which that is, it says so rather than guessing.
func upgradeCommand() *Command {
	return &Command{
		Name:    "upgrade",
		Summary: "Install the newest published vat, the way this one was installed",
		Usage:   "vat upgrade [--check]",
		Long: `Compare this binary against the newest published release and, when it is
behind, run the upgrade for however it was installed.

vat replaces nothing itself. A Homebrew install is upgraded by Homebrew and a
` + "`go install`" + ` by the Go toolchain, because each keeps its own record of what it
put on disk. An install vat cannot account for — an unpacked release archive —
is reported with the release to fetch, and nothing is run.`,
		Examples: []string{
			"vat upgrade           # upgrade if a newer release is published",
			"vat upgrade --check   # say what would happen; run nothing",
		},
		Run: func(ctx context.Context, env *Env, args []string) error {
			set := newFlagSet("upgrade")
			check := set.Bool("check", false, "report what would be done and run nothing")
			if err := parseFlags(set, args); err != nil {
				return err
			}

			running := env.ToolVersion
			if running == "" {
				running = version.Short()
			}
			latest, err := latestRelease(ctx)
			if err != nil {
				return fmt.Errorf("find the newest release: %w", err)
			}

			if env.JSON {
				return emitJSON(env, map[string]any{
					"running": running,
					"latest":  latest,
					"current": isAtLeast(running, latest),
				})
			}

			if isAtLeast(running, latest) {
				env.Printer.Status(ui.LevelOK, "vat", running+" is the newest release")
				return nil
			}
			env.Printer.Status(ui.LevelWarn, "vat",
				fmt.Sprintf("running %s; %s is published", running, latest))

			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate this binary: %w", err)
			}
			name, arguments, ok := upgradeCommandFor(executable)
			if !ok {
				env.Printer.Status(ui.LevelInfo, executable,
					"vat cannot tell how this was installed, so it will not guess")
				env.Printer.Hint("\nFetch %s from", latest)
				env.Printer.Hint("  https://github.com/takealook97/vat/releases/tag/%s", latest)
				return findingsErrorf("")
			}

			line := name + " " + strings.Join(arguments, " ")
			if *check {
				env.Printer.Status(ui.LevelInfo, "would run", line)
				return nil
			}
			env.Printer.Status(ui.LevelInfo, "running", line)
			// Captured rather than streamed. An installer's output is somebody
			// else's bytes arriving on this terminal, and everything vat prints
			// goes through internal/ui so a control character in them is
			// rendered rather than executed. The cost is that progress appears
			// at the end instead of as it happens.
			upgrade := exec.CommandContext(ctx, name, arguments...)
			output, runErr := upgrade.CombinedOutput()
			for _, printed := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
				if printed != "" {
					env.Printer.Println("  " + printed)
				}
			}
			if runErr != nil {
				return fmt.Errorf("%s: %w", line, runErr)
			}
			return nil
		},
	}
}

// upgradeCommandFor names the command that owns this binary, or reports that
// nothing does. The decision is made on the path alone so it can be tested
// without installing vat several ways.
func upgradeCommandFor(executable string) (string, []string, bool) {
	path := strings.ReplaceAll(executable, "\\", "/")
	switch {
	// Every Homebrew prefix keeps its formulae under a Cellar, so the segment
	// identifies the installer without hard-coding /opt/homebrew, /usr/local,
	// and whatever prefix a linuxbrew user chose.
	case strings.Contains(path, "/Cellar/"):
		return "brew", []string{"upgrade", "takealook97/tap/vat"}, true
	case strings.Contains(path, "/go/bin/"):
		return "go", []string{"install", "github.com/takealook97/vat/cmd/vat@latest"}, true
	default:
		return "", nil, false
	}
}

// latestRelease reads the tag of the newest published release.
func latestRelease(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", latestReleaseURL, response.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.TagName == "" {
		return "", fmt.Errorf("%s named no tag", latestReleaseURL)
	}
	return body.TagName, nil
}

// isAtLeast answers whether the running version is the newest or newer. A
// version that cannot be parsed — a development build — is treated as current,
// because telling its owner to upgrade names an action that does not apply to
// how they got it.
func isAtLeast(running, latest string) bool {
	constraint, err := manifest.ParseConstraint(">=" + latest)
	if err != nil {
		return true
	}
	ok, err := constraint.Allows(running)
	return err != nil || ok
}
