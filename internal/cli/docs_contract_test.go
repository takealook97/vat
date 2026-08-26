package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/ui"
)

// vat exists because a rule that is only written down is a hope — and its own
// command reference was written down and checked by nobody. Independent QA
// found six commands accepting flags their help text never mentioned, and a
// reference section for a command that had no section at all.
//
// These tests read the documentation and the flag registrations themselves and
// compare them against the command tree, so the three cannot drift apart
// without the suite going red.

const referencePath = "../../docs/COMMANDS.md"

func readReference(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("read %s: %v", referencePath, err)
	}
	return string(content)
}

// walkCommands visits every command in the tree with its full path.
func walkCommands(command *Command, path []string, visit func(*Command, []string)) {
	if len(path) > 0 {
		visit(command, path)
	}
	for _, sub := range command.Subcommands {
		walkCommands(sub, append(append([]string{}, path...), sub.Name), visit)
	}
}

func TestEveryTopLevelCommandHasASectionInTheReference(t *testing.T) {
	// Arrange
	reference := readReference(t)

	// Act & Assert
	for _, sub := range Root().Subcommands {
		if sub.Hidden {
			continue
		}
		heading := "## vat " + sub.Name
		if !strings.Contains(reference, heading) {
			t.Errorf("%s has no %q section; a command nobody documented is a command nobody finds",
				referencePath, heading)
		}
	}
}

func TestTheReferenceDocumentsNoCommandThatDoesNotExist(t *testing.T) {
	// Arrange
	reference := readReference(t)
	real := map[string]bool{}
	for _, sub := range Root().Subcommands {
		real[sub.Name] = true
	}

	// Act
	headings := regexp.MustCompile(`(?m)^## vat ([a-z-]+)`).FindAllStringSubmatch(reference, -1)

	// Assert
	if len(headings) == 0 {
		t.Fatal("no command sections found; the document changed shape and this test stopped checking anything")
	}
	for _, heading := range headings {
		if !real[heading[1]] {
			t.Errorf("%s documents `vat %s`, which does not exist", referencePath, heading[1])
		}
	}
}

var flagInUsage = regexp.MustCompile(`--([a-z][a-z-]*)`)

// registeredFlags returns the flags a command actually registers, observed by
// invoking it and capturing the flag set it built.
//
// Reading the source instead would mean inferring which registrations belong to
// which command, and the shared helpers make that inference wrong. Observing
// the real set cannot be.
//
// The probe passes a flag no command defines: every Run registers its flags and
// then parses, so the hook fires with a complete set and parsing fails
// immediately afterwards, before the command does anything.
func registeredFlags(t *testing.T, path []string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	onFlagsParsed = func(set *flag.FlagSet) {
		set.VisitAll(func(f *flag.Flag) { names[f.Name] = true })
	}
	defer func() { onFlagsParsed = nil }()

	var out, errOut bytes.Buffer
	env := &Env{
		Printer: ui.NewWith(&out, &errOut, false),
		Now:     testNow,
		Cwd:     t.TempDir(),
		Root:    t.TempDir(),
	}
	dispatch(context.Background(), env, Root(),
		append(append([]string{}, path...), "--vat-probe-not-a-flag"), nil)
	return names
}

func TestEveryFlagACommandRegistersAppearsInItsUsageLine(t *testing.T) {
	// Arrange: an undiscoverable flag is one nobody uses. This is the direction
	// that had actually rotted — six commands accepted flags their own help
	// never mentioned.
	walkCommands(Root(), nil, func(command *Command, path []string) {
		if command.Usage == "" || command.Run == nil {
			return
		}
		// Act
		registered := registeredFlags(t, path)
		if len(registered) == 0 {
			return
		}
		advertised := map[string]bool{}
		for _, match := range flagInUsage.FindAllStringSubmatch(command.Usage, -1) {
			advertised[match[1]] = true
		}

		// Assert
		var missing []string
		for name := range registered {
			if !advertised[name] {
				missing = append(missing, "--"+name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("`vat %s` registers %s but its usage line does not mention them",
				strings.Join(path, " "), strings.Join(missing, ", "))
		}
	})
}

func TestEveryUsageLineAdvertisesOnlyFlagsThatExist(t *testing.T) {
	// Arrange: a usage line naming a flag the binary rejects sends the reader
	// to an error message instead of to the thing they wanted.
	globals := map[string]bool{
		"json": true, "quiet": true, "no-color": true, "workspace": true,
		"yes": true, "version": true, "help": true,
	}

	walkCommands(Root(), nil, func(command *Command, path []string) {
		if command.Usage == "" || command.Run == nil {
			return
		}
		// Act
		registered := registeredFlags(t, path)

		// Assert
		for _, match := range flagInUsage.FindAllStringSubmatch(command.Usage, -1) {
			name := match[1]
			if globals[name] || registered[name] {
				continue
			}
			t.Errorf("`vat %s` usage advertises --%s, which it never registers",
				strings.Join(path, " "), name)
		}
	})
}

func TestTheReferenceStatesTheExitCodesTheCodeUses(t *testing.T) {
	// Arrange: exit codes are part of the interface — CI branches on them.
	reference := readReference(t)

	// Act & Assert
	for _, expected := range []string{"`0`", "`1`", "`2`"} {
		if !strings.Contains(reference, expected) {
			t.Errorf("%s does not state exit code %s", referencePath, expected)
		}
	}
	if ExitOK != 0 || ExitFindings != 1 || ExitUsage != 2 {
		t.Errorf("exit codes changed (%d/%d/%d) without the reference following",
			ExitOK, ExitFindings, ExitUsage)
	}
}
