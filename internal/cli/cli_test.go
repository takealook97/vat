package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/takealook97/vat/internal/ui"
)

func TestFlagsAreAcceptedAfterAPositionalArgument(t *testing.T) {
	// Arrange: Go's flag package stops at the first non-flag argument, which
	// would silently treat --title as a positional here.
	set := newFlagSet("test")
	title := set.String("title", "", "")
	dryRun := set.Bool("dry-run", false, "")

	// Act
	err := parseFlags(set, []string{"decision", "--title", "Something", "--dry-run"})

	// Assert
	if err != nil {
		t.Fatalf("parseFlags returned an error: %v", err)
	}
	if *title != "Something" {
		t.Errorf("title = %q, want Something", *title)
	}
	if !*dryRun {
		t.Error("--dry-run after a positional was not applied")
	}
	if set.NArg() != 1 || set.Arg(0) != "decision" {
		t.Errorf("positional args = %v, want [decision]", set.Args())
	}
}

func TestABooleanFlagDoesNotSwallowTheNextArgument(t *testing.T) {
	// Arrange
	set := newFlagSet("test")
	dryRun := set.Bool("dry-run", false, "")

	// Act
	if err := parseFlags(set, []string{"--dry-run", "payments"}); err != nil {
		t.Fatalf("parseFlags returned an error: %v", err)
	}

	// Assert
	if !*dryRun {
		t.Error("--dry-run was not applied")
	}
	if set.NArg() != 1 || set.Arg(0) != "payments" {
		t.Errorf("positional args = %v, want [payments]; a bool flag consumed one", set.Args())
	}
}

func TestEverythingAfterADoubleDashStaysPositional(t *testing.T) {
	// Arrange: `vat exec -- git log --oneline` must not read --oneline as ours.
	set := newFlagSet("exec")
	group := set.String("group", "", "")

	// Act
	if err := parseFlags(set, []string{"--group", "backend", "--", "git", "log", "--oneline"}); err != nil {
		t.Fatalf("parseFlags returned an error: %v", err)
	}

	// Assert
	if *group != "backend" {
		t.Errorf("group = %q, want backend", *group)
	}
	joined := strings.Join(set.Args(), " ")
	if joined != "git log --oneline" {
		t.Errorf("command = %q, want `git log --oneline`", joined)
	}
}

func TestGlobalFlagsAreRecognisedWhereverTheyAppear(t *testing.T) {
	// Arrange
	global := newFlagSet("vat")
	jsonOut := global.Bool("json", false, "")

	// Act
	rest, err := splitGlobalFlags(global, []string{"status", "--json", "--dirty"})

	// Assert
	if err != nil {
		t.Fatalf("splitGlobalFlags returned an error: %v", err)
	}
	if !*jsonOut {
		t.Error("--json after the subcommand was not recognised")
	}
	if strings.Join(rest, " ") != "status --dirty" {
		t.Errorf("remaining args = %v, want [status --dirty]", rest)
	}
}

func TestSplitListAcceptsCommaSeparatedValuesAndIgnoresBlanks(t *testing.T) {
	// Act
	got := splitList(" a , ,b,, c ")

	// Assert
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunningOutsideAWorkspaceExitsWithAUsageCodeAndAHint(t *testing.T) {
	// Arrange
	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     time.Now(),
		Cwd:     t.TempDir(),
	}
	t.Setenv("VAT_WORKSPACE", "")

	// Act
	code := dispatch(context.Background(), env, statusCommand(), nil, []string{"vat"})

	// Assert
	if code != ExitUsage {
		t.Errorf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(out.String()+errOut.String(), "vat init") {
		t.Errorf("no hint about creating a workspace:\n%s%s", out.String(), errOut.String())
	}
}

func TestEveryCommandDeclaresASummaryAndAWayToRunOrBranch(t *testing.T) {
	// Arrange
	var walk func(command *Command, path string)
	walk = func(command *Command, path string) {
		if command.Summary == "" {
			t.Errorf("%s has no summary", path)
		}
		if command.Run == nil && len(command.Subcommands) == 0 {
			t.Errorf("%s neither runs nor branches", path)
		}
		for _, sub := range command.Subcommands {
			walk(sub, path+" "+sub.Name)
		}
	}

	// Act & Assert
	walk(Root(), "vat")
}

func TestCompletionScriptsCoverEveryTopLevelCommand(t *testing.T) {
	// Arrange
	root := Root()

	for _, shell := range []string{"bash", "zsh", "fish"} {
		// Act
		script, err := completionScript(shell)

		// Assert
		if err != nil {
			t.Fatalf("%s: completionScript returned an error: %v", shell, err)
		}
		for _, sub := range root.Subcommands {
			if !strings.Contains(script, sub.Name) {
				t.Errorf("%s completion omits %q", shell, sub.Name)
			}
		}
	}
}

func TestCompletionRejectsAnUnknownShell(t *testing.T) {
	// Act
	_, err := completionScript("powershell")

	// Assert
	if err == nil {
		t.Fatal("an unknown shell was accepted")
	}
}

// `vat --nope status` answered `unknown command "--nope"`. It is a flag, and
// telling somebody their flag is not a command sends them looking through the
// command list for something they never typed.
func TestAnUnknownFlagIsNotReportedAsAnUnknownCommand(t *testing.T) {
	// Arrange & Act
	var out bytes.Buffer
	env := &Env{Printer: ui.NewWith(&out, &out, false), Cwd: t.TempDir()}
	code := dispatch(t.Context(), env, Root(), []string{"--nope", "status"}, nil)

	// Assert
	if code == ExitOK {
		t.Fatalf("an unknown flag was accepted:\n%s", out.String())
	}
	if strings.Contains(out.String(), "unknown command") {
		t.Errorf("a flag was reported as a command:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--nope") {
		t.Errorf("the refusal does not name what was passed:\n%s", out.String())
	}
}
