// Package cli wires vat's commands together.
//
// Commands are plain structs with a Run function rather than a framework,
// keeping the dependency list at one library. The rules are uniform: a command
// that reports problems exits 1, a command misused exits 2, and every command
// that prints a table can print the same data as JSON instead.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/takealook97/vat/internal/manifest"
	"github.com/takealook97/vat/internal/ui"
	"github.com/takealook97/vat/internal/version"
	"github.com/takealook97/vat/internal/workspace"
)

// Exit codes. They are part of the interface: scripts and CI depend on the
// difference between "I found problems" and "you called me wrong".
const (
	// ExitOK means the command completed and found nothing wrong.
	ExitOK = 0
	// ExitFindings means the command ran and reported problems.
	ExitFindings = 1
	// ExitUsage means the invocation itself was wrong.
	ExitUsage = 2
)

// UsageError marks an error caused by how the command was called.
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

func usageErrorf(format string, args ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

// FindingsError marks a command that ran correctly and found problems.
type FindingsError struct{ Message string }

func (e *FindingsError) Error() string { return e.Message }

func findingsErrorf(format string, args ...any) error {
	return &FindingsError{Message: fmt.Sprintf(format, args...)}
}

// Env is the shared state a command runs with.
type Env struct {
	Printer *ui.Printer
	// JSON switches every reporting command to machine-readable output.
	JSON bool
	// Now is the reference time, injected so behaviour is testable.
	Now time.Time
	// Cwd is the directory vat was invoked from.
	Cwd string
	// Root overrides workspace discovery.
	Root string
	// Yes answers confirmation prompts affirmatively. It exists for CI, and
	// deliberately does not extend to destructive repository removal.
	Yes bool
	// ToolVersion is the version of vat this invocation reports itself as,
	// injected so the requirement in vat.yaml is testable without building a
	// stamped binary. Empty means the build's own version.
	ToolVersion string
}

// Workspace opens the workspace this invocation applies to.
//
// A workspace that requires a vat this is not is refused here rather than in
// each command, for the same reason a manifest written against a newer schema
// is: every command past this point would otherwise act on a file whose rules
// it does not implement. `vat --version` needs no workspace and still answers.
func (e *Env) Workspace() (*workspace.Workspace, error) {
	ws, err := e.open()
	if err != nil {
		return nil, err
	}
	if err := manifest.CheckToolVersion(ws.Manifest, e.version()); err != nil {
		return nil, err
	}
	return ws, nil
}

func (e *Env) open() (*workspace.Workspace, error) {
	if e.Root != "" {
		return workspace.OpenAt(e.Root)
	}
	return workspace.Open(e.Cwd)
}

func (e *Env) version() string {
	if e.ToolVersion != "" {
		return e.ToolVersion
	}
	return version.Short()
}

// Command is one verb.
type Command struct {
	Name        string
	Summary     string
	Usage       string
	Long        string
	Examples    []string
	Run         func(ctx context.Context, env *Env, args []string) error
	Subcommands []*Command
	// Hidden keeps a command out of the top-level listing.
	Hidden bool
}

// Root builds the whole command tree.
func Root() *Command {
	return &Command{
		Name:    "vat",
		Summary: "Governed workspace for many repositories and the agents working in them",
		Usage:   "vat <command> [flags]",
		Long: `vat is a control plane for a workspace of independent git repositories.

It keeps a manifest of what the workspace governs, updates the repositories
without ever discarding local work, generates the agent contracts each one
carries, and records the evidence for changes that span several of them.

Its optional knowledge layer — the brain — holds reviewed organisational facts
with the revision each was read from, and demotes a claim that nobody has
re-checked instead of letting it pass as current forever.`,
		Subcommands: []*Command{
			initCommand(),
			statusCommand(),
			syncCommand(),
			doctorCommand(),
			lintCommand(),
			repoCommand(),
			harnessCommand(),
			brainCommand(),
			changesetCommand(),
			shipCommand(),
			evidenceCommand(),
			execCommand(),
			metricsCommand(),
			fitCommand(),
			completionCommand(),
			versionCommand(),
		},
	}
}

// Execute parses arguments and runs the matching command, returning a process
// exit code.
func Execute(ctx context.Context, args []string) int {
	printer := ui.New()
	env := &Env{Printer: printer, Now: time.Now()}
	if cwd, err := os.Getwd(); err == nil {
		env.Cwd = cwd
	}

	global := flag.NewFlagSet("vat", flag.ContinueOnError)
	global.SetOutput(new(strings.Builder))
	jsonOut := global.Bool("json", false, "emit machine-readable JSON")
	noColor := global.Bool("no-color", false, "disable colour output")
	quiet := global.Bool("quiet", false, "print only warnings and failures")
	root := global.String("workspace", "", "workspace root (default: search upward for vat.yaml)")
	yes := global.Bool("yes", false, "assume yes for confirmations that are not destructive")
	showVersion := global.Bool("version", false, "print the version and exit")

	rest, err := splitGlobalFlags(global, args)
	if err != nil {
		printer.Errorf("%v", err)
		return ExitUsage
	}
	env.JSON = *jsonOut
	env.Root = *root
	env.Yes = *yes
	if *noColor {
		env.Printer = env.Printer.WithColor(false)
	}
	if *quiet {
		env.Printer = env.Printer.WithQuiet(true)
	}
	if *showVersion {
		env.Printer.Println(version.Long())
		return ExitOK
	}

	command := Root()
	if len(rest) == 0 {
		printHelp(env.Printer, command, nil)
		return ExitOK
	}
	return dispatch(ctx, env, command, rest, nil)
}

// splitGlobalFlags pulls the flags vat itself understands out of the argument
// list, wherever they appear, and returns the remainder for the subcommand.
func splitGlobalFlags(set *flag.FlagSet, args []string) ([]string, error) {
	known := map[string]bool{}
	set.VisitAll(func(f *flag.Flag) { known[f.Name] = true })

	var globals, rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name := strings.TrimLeft(arg, "-")
		if key, _, found := strings.Cut(name, "="); found {
			name = key
		}
		if strings.HasPrefix(arg, "-") && known[name] {
			globals = append(globals, arg)
			// A global flag that takes a value must carry it along, or the
			// value is left behind and read as a subcommand.
			if !strings.Contains(arg, "=") && takesValue(set, name) && i+1 < len(args) {
				i++
				globals = append(globals, args[i])
			}
			continue
		}
		rest = append(rest, arg)
	}
	if err := set.Parse(globals); err != nil {
		return nil, err
	}
	return rest, nil
}

func dispatch(ctx context.Context, env *Env, command *Command, args []string, path []string) int {
	path = append(path, command.Name)

	if len(command.Subcommands) > 0 && len(args) > 0 {
		name := args[0]
		if name == "help" || name == "--help" || name == "-h" {
			printHelp(env.Printer, command, path)
			return ExitOK
		}
		for _, sub := range command.Subcommands {
			if sub.Name == name {
				return dispatch(ctx, env, sub, args[1:], path)
			}
		}
		if command.Run == nil {
			// A flag is not a command. Telling somebody their option is not one
			// sends them looking through the command list for something they
			// never typed, and the suggestion below would offer them a verb.
			if strings.HasPrefix(name, "-") {
				env.Printer.Errorf("unknown flag %q", name)
				env.Printer.Hint("Run `%s --help` for the flags and commands that exist.",
					strings.Join(path, " "))
				return ExitUsage
			}
			full := strings.Join(append(path[1:], name), " ")
			env.Printer.Errorf("unknown command %q", full)
			suggest(env.Printer, command, name)
			env.Printer.Hint("Run `%s --help` for the commands that exist.",
				strings.Join(path, " "))
			return ExitUsage
		}
	}
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		printHelp(env.Printer, command, path)
		return ExitOK
	}
	if command.Run == nil {
		printHelp(env.Printer, command, path)
		if len(command.Subcommands) > 0 {
			return ExitUsage
		}
		return ExitOK
	}

	err := command.Run(ctx, env, args)
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, ErrHelpRequested):
		printHelp(env.Printer, command, path)
		return ExitOK
	case isUsageError(err):
		env.Printer.Errorf("%v", err)
		env.Printer.ErrorHint("%s", usageLine(command))
		return ExitUsage
	case isFindingsError(err):
		// The command already printed its findings; this is only the exit code.
		if message := err.Error(); message != "" {
			env.Printer.Hint("\n%s", message)
		}
		return ExitFindings
	case errors.Is(err, manifest.ErrToolTooNew):
		env.Printer.Errorf("%v", err)
		// Naming the upgrade here would name the one action that cannot help:
		// this vat is already past the range, and every newer one is further
		// from it.
		env.Printer.ErrorHint(
			"Widen `requires.vat` in vat.yaml to admit this version, or install the one it names.")
		return ExitFindings
	case errors.Is(err, manifest.ErrToolTooOld):
		env.Printer.Errorf("%v", err)
		env.Printer.ErrorHint(
			"Upgrade vat, or change `requires.vat` in vat.yaml if the requirement is wrong.")
		return ExitFindings
	case errors.Is(err, workspace.ErrNoWorkspace), errors.Is(err, manifest.ErrNotFound):
		env.Printer.Errorf("%v", err)
		env.Printer.ErrorHint("Run `vat init` here, or `cd` into a workspace.")
		return ExitUsage
	default:
		env.Printer.Errorf("%v", err)
		return ExitFindings
	}
}

func isUsageError(err error) bool {
	var target *UsageError
	return errors.As(err, &target)
}

func isFindingsError(err error) bool {
	var target *FindingsError
	return errors.As(err, &target)
}

func usageLine(command *Command) string {
	if command.Usage != "" {
		return "usage: " + command.Usage
	}
	return "usage: vat " + command.Name
}

func printHelp(printer *ui.Printer, command *Command, path []string) {
	name := command.Name
	if len(path) > 1 {
		name = strings.Join(path, " ")
	}
	if command.Long != "" {
		printer.Println(command.Long)
	} else if command.Summary != "" {
		printer.Println(command.Summary)
	}
	printer.Println()
	if command.Usage != "" {
		printer.Printf("Usage:\n  %s\n", command.Usage)
	} else {
		printer.Printf("Usage:\n  %s\n", name)
	}

	if len(command.Subcommands) > 0 {
		visible := make([]*Command, 0, len(command.Subcommands))
		for _, sub := range command.Subcommands {
			if !sub.Hidden {
				visible = append(visible, sub)
			}
		}
		sort.SliceStable(visible, func(i, j int) bool { return false })
		printer.Println()
		printer.Println("Commands:")
		width := 0
		for _, sub := range visible {
			if len(sub.Name) > width {
				width = len(sub.Name)
			}
		}
		for _, sub := range visible {
			printer.Printf("  %-*s  %s\n", width, sub.Name, sub.Summary)
		}
	}
	if len(command.Examples) > 0 {
		printer.Println()
		printer.Println("Examples:")
		for _, example := range command.Examples {
			printer.Printf("  %s\n", example)
		}
	}
	if len(path) <= 1 {
		printer.Println()
		printer.Println("Global flags:")
		printer.Println("  --workspace <dir>  workspace root (default: search upward for vat.yaml)")
		printer.Println("  --json             machine-readable output")
		printer.Println("  --quiet            print only warnings and failures")
		printer.Println("  --no-color         disable colour")
		printer.Println("  --yes              assume yes for non-destructive confirmations")
		printer.Println()
		printer.Println("Run `vat <command> --help` for detail on any command.")
	}
}

func suggest(printer *ui.Printer, command *Command, unknown string) {
	var close []string
	for _, sub := range command.Subcommands {
		if strings.HasPrefix(sub.Name, unknown[:min(len(unknown), 2)]) {
			close = append(close, sub.Name)
		}
	}
	if len(close) > 0 {
		printer.Hint("Did you mean: %s", strings.Join(close, ", "))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// newFlagSet builds a flag set that reports errors through vat's own printer
// rather than dumping Go's default usage block.
func newFlagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(new(strings.Builder))
	set.Usage = func() {}
	return set
}

// ErrHelpRequested signals that the caller asked for help rather than misusing
// the command.
var ErrHelpRequested = errors.New("help requested")

// onFlagsParsed, when set, is handed every flag set a command builds. It exists
// so a test can compare the flags a command actually registers against the ones
// its usage line advertises, without a second list to keep in step. Production
// leaves it nil and pays one comparison.
var onFlagsParsed func(set *flag.FlagSet)

func parseFlags(set *flag.FlagSet, args []string) error {
	if onFlagsParsed != nil {
		onFlagsParsed(set)
	}
	if err := set.Parse(permute(set, args)); err != nil {
		// A command nested three levels deep reaches its own flag set before
		// the dispatcher sees --help, and Go's package reports that as the
		// error string "flag: help requested". Left alone it surfaced as a
		// usage failure with an internal message.
		if errors.Is(err, flag.ErrHelp) {
			return ErrHelpRequested
		}
		return usageErrorf("%v", err)
	}
	return nil
}

// permute moves flags ahead of positional arguments.
//
// Go's flag package stops parsing at the first non-flag argument, which would
// make `vat brain new decision --title x` silently treat --title as a
// positional. Every other command-line tool accepts flags in any position, so
// vat reorders rather than surprising the user.
func permute(set *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			// Everything after a bare -- is positional by definition; `vat exec
			// -- git log --oneline` must not have --oneline read as vat's flag.
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if _, _, hasValue := strings.Cut(name, "="); hasValue {
			// "--flag=value" is self-contained; it never consumes the next
			// argument.
			flags = append(flags, arg)
			continue
		}
		flags = append(flags, arg)
		if takesValue(set, name) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

// takesValue reports whether a flag consumes the argument after it. Boolean
// flags do not, so `--dry-run status` must not swallow "status".
func takesValue(set *flag.FlagSet, name string) bool {
	found := set.Lookup(name)
	if found == nil {
		return false
	}
	boolFlag, ok := found.Value.(interface{ IsBoolFlag() bool })
	return !ok || !boolFlag.IsBoolFlag()
}
