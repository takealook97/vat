package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/gitx"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/workspace"
)

func initCommand() *Command {
	return &Command{
		Name:    "init",
		Summary: "Create a workspace manifest here",
		Usage:   "vat init [--name <name>] [--adopt] [--from-tsv <file>]",
		Long: `Create a vat.yaml in the current directory.

With --adopt, every git repository already sitting beside it is enrolled, with
its existing origin and current branch recorded as-is. Nothing is moved, cloned,
or reconfigured: init describes what is here, it does not rearrange it.`,
		Examples: []string{
			"vat init --adopt                    # enrol the repositories already here",
			"vat init --name acme                # start empty",
			"vat init --from-tsv config/repos.tsv  # migrate a name<TAB>origin manifest",
		},
		Run: runInit,
	}
}

func runInit(ctx context.Context, env *Env, args []string) error {
	set := newFlagSet("init")
	name := set.String("name", "", "workspace name (default: the directory name)")
	adopt := set.Bool("adopt", false, "enrol git repositories already present")
	fromTSV := set.String("from-tsv", "", "import a legacy name<TAB>origin manifest")
	if err := parseFlags(set, args); err != nil {
		return err
	}

	root := env.Cwd
	if env.Root != "" {
		root = env.Root
	}
	path := filepath.Join(root, manifest.FileName)
	if fsx.Exists(path) {
		return usageErrorf("%s already exists in %s", manifest.FileName, root)
	}

	workspaceName := *name
	if workspaceName == "" {
		workspaceName = filepath.Base(root)
	}
	built := manifest.Default(workspaceName)

	var enrolled []manifest.Repo
	switch {
	case *fromTSV != "":
		imported, err := importTSV(*fromTSV)
		if err != nil {
			return err
		}
		enrolled = imported
	case *adopt:
		discovered, err := discoverRepos(ctx, root)
		if err != nil {
			return err
		}
		enrolled = discovered
	}
	for _, repo := range enrolled {
		built = manifest.WithRepo(built, repo)
	}
	// A single repository named cortex or brain is almost always the knowledge
	// layer; wiring the policy now saves a confusing "brain commands do
	// nothing" first experience.
	for _, repo := range built.Repos {
		if repo.Role == manifest.RoleBrain {
			built.Policy.Brain.Repo = repo.Name
			break
		}
	}

	if err := manifest.Save(path, built); err != nil {
		return err
	}
	ws, err := workspace.OpenAt(root)
	if err != nil {
		return err
	}
	if _, err := ws.SyncGitignore(built); err != nil {
		return err
	}
	rendered, err := lint.RenderHarness(ws)
	if err != nil {
		return err
	}

	printer := env.Printer
	printer.Status(ui.LevelOK, manifest.FileName, fmt.Sprintf("%s enrolled", pluraliseCount(len(built.Repos), "repository", "repositories")))
	printer.Status(ui.LevelOK, ".gitignore", "governed repositories excluded from the root history")
	for _, file := range rendered {
		printer.Status(ui.LevelOK, file, "generated")
	}
	for _, repo := range built.Repos {
		printer.Status(ui.LevelInfo, repo.Name, fmt.Sprintf("%s · %s", repo.Role, gitx.Redact(repo.Origin)))
	}

	if len(rendered) > 0 {
		// Without this, the very first `vat status` a new user runs shows every
		// repository dirty and nothing explains why.
		printer.Hint("\nThe generated contracts above are uncommitted, so `vat status` will")
		printer.Hint("show those repositories as dirty until you commit them.")
	}

	printer.Heading("Next")
	printer.Println("  vat status        see where every repository stands")
	printer.Println("  vat doctor        judge the environment")
	printer.Println("  vat fit           decide which layers are worth adopting yet")
	if len(built.Repos) == 0 {
		printer.Println("  vat repo add <name> --origin <url>")
	}
	return nil
}

// discoverRepos enrols the git repositories already sitting beside the
// manifest. Their origin and branch are read, never changed.
func discoverRepos(ctx context.Context, root string) ([]manifest.Repo, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var repos []manifest.Repo
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if repo, ok := describeRepo(ctx, root, entry.Name()); ok {
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

func importTSV(path string) ([]manifest.Repo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var repos []manifest.Repo
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(trimmed, "\t")
		if len(fields) < 2 {
			fields = strings.Fields(trimmed)
		}
		if len(fields) < 2 {
			return nil, fmt.Errorf("%s: cannot read %q as name<TAB>origin", path, trimmed)
		}
		name := strings.TrimSpace(fields[0])
		repos = append(repos, manifest.Repo{
			Name:     name,
			Origin:   strings.TrimSpace(fields[1]),
			Role:     inferRole(name),
			Required: true,
		})
	}
	return repos, nil
}

// inferRole guesses a repository's role from its name. The guess is written
// into the manifest where it is visible and easy to correct, rather than being
// re-derived silently on every run.
func inferRole(name string) manifest.Role {
	lower := strings.ToLower(name)
	switch {
	case lower == "brain" || lower == "cortex" || lower == "knowledge":
		return manifest.RoleBrain
	case strings.Contains(lower, "credential") || strings.Contains(lower, "secret") ||
		lower == "vault" || lower == "sops":
		return manifest.RoleCredential
	case strings.Contains(lower, "docs") || strings.Contains(lower, "homepage") ||
		strings.Contains(lower, "website"):
		return manifest.RoleDocs
	case strings.Contains(lower, "infra") || strings.Contains(lower, "terraform"):
		return manifest.RoleInfra
	default:
		return manifest.RoleProduct
	}
}
