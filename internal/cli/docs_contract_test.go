package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/lint"
	"github.com/takealook97/vat/internal/manifest"
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

func TestTheReferenceListsExactlyTheLintRulesThatExist(t *testing.T) {
	// Arrange: the rule table is the only place a user learns what `vat lint`
	// can tell them. A rule missing from it is a rule nobody knows to look for,
	// and a rule listed but absent sends them hunting for output that will
	// never appear.
	reference := readReference(t)
	documented := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^\| `+"`"+`([a-z]+/[a-z-]+)`+"`").
		FindAllStringSubmatch(reference, -1) {
		documented[match[1]] = true
	}
	if len(documented) == 0 {
		t.Fatal("no lint rules found in the reference; the table changed shape and this test stopped checking anything")
	}

	real := map[string]bool{}
	for _, name := range lint.RuleNames() {
		real[name] = true
	}

	// Act & Assert
	for name := range real {
		if !documented[name] {
			t.Errorf("lint rule %q is not in the reference's rule table", name)
		}
	}
	for name := range documented {
		if !real[name] {
			t.Errorf("the reference documents lint rule %q, which lint never reports", name)
		}
	}
}

func TestTheManifestReferenceNamesEveryFieldTheSchemaAccepts(t *testing.T) {
	// Arrange: an undocumented manifest field is one nobody sets, and vat.yaml
	// rejects unknown keys, so a field documented but absent produces a hard
	// parse failure rather than a shrug.
	content, err := os.ReadFile("../../docs/MANIFEST.md")
	if err != nil {
		t.Fatalf("read manifest reference: %v", err)
	}
	reference := string(content)

	// Act & Assert
	for _, field := range yamlFieldNames(manifest.Repo{}) {
		if !strings.Contains(reference, "`"+field+"`") {
			t.Errorf("docs/MANIFEST.md never names the repository field %q", field)
		}
	}
	for _, field := range yamlFieldNames(manifest.Workspace{}) {
		if !strings.Contains(reference, "`"+field+"`") {
			t.Errorf("docs/MANIFEST.md never names the workspace field %q", field)
		}
	}
}

// yamlFieldNames returns the on-disk names of a struct's serialised fields.
func yamlFieldNames(value any) []string {
	structType := reflect.TypeOf(value)
	names := make([]string, 0, structType.NumField())
	for i := 0; i < structType.NumField(); i++ {
		tag := structType.Field(i).Tag.Get("yaml")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestTheReadmeQuotesTheRuleCountItActuallyHas guards a figure that had already
// drifted before anyone noticed: the sample run in the README said "21 rules"
// while the tool reported 22, and the reader has no way to tell which is true.
// The README's console blocks are meant to be output the tool actually prints,
// so the one number in them that the code can verify is verified here.
func TestTheReadmeQuotesTheRuleCountItActuallyHas(t *testing.T) {
	// Arrange
	content, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	quoted := regexp.MustCompile(`across (\d+) rules`).FindAllStringSubmatch(string(content), -1)
	if len(quoted) == 0 {
		t.Fatal("no rule count found in the README; the sample changed shape and this test stopped checking anything")
	}

	// Act
	actual := strconv.Itoa(len(lint.RuleNames()))

	// Assert
	for _, match := range quoted {
		if match[1] != actual {
			t.Errorf("the README says %q but lint reports %s rules", match[0], actual)
		}
	}
}

// The same guarantee the rule table above gives `vat lint`, for the rules the
// knowledge layer reports. It had none, and twenty-two of its twenty-four rules
// had gone unnamed in any document since the first release — the exact state
// this class of test exists to make impossible.
func TestTheBrainReferenceListsExactlyTheRulesThatExist(t *testing.T) {
	// Arrange
	content, err := os.ReadFile("../../docs/BRAIN.md")
	if err != nil {
		t.Fatalf("read brain reference: %v", err)
	}
	documented := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^\| `+"`"+`(brain/[a-z-]+)`+"`").
		FindAllStringSubmatch(string(content), -1) {
		documented[match[1]] = true
	}
	if len(documented) == 0 {
		t.Fatal("no brain rules found in docs/BRAIN.md; the table changed shape and this test stopped checking anything")
	}

	real := map[string]bool{}
	for _, name := range brain.RuleNames() {
		real[name] = true
	}

	// Act & Assert
	for name := range real {
		if !documented[name] {
			t.Errorf("brain rule %q is not in the reference table in docs/BRAIN.md", name)
		}
	}
	for name := range documented {
		if !real[name] {
			t.Errorf("docs/BRAIN.md documents brain rule %q, which brain check never reports", name)
		}
	}
}

// The README tells a reader to verify a download with `gh attestation verify`,
// and the security model promises an SBOM for the platform they downloaded.
// Both are claims about a workflow that only ever runs on a tag push: if the
// steps behind them are dropped, the first person to find out is holding a
// public artefact that cannot be re-signed under a tag that cannot be moved.
// So the claims are read here, against the workflow that has to honour them.
func TestTheReleaseWorkflowProducesWhatTheDocsPromise(t *testing.T) {
	// Arrange
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}
	workflow := read("../../.github/workflows/release.yml")

	// A promise nobody makes any more needs no guard, and a guard that checks
	// an absent promise is worse than none: it reports success for silence.
	promises := []struct {
		path  string
		claim string
	}{
		{"../../README.md", "gh attestation verify"},
		{"../../docs/SECURITY_MODEL.md", "gh attestation verify"},
		{"../../docs/SECURITY_MODEL.md", "CycloneDX SBOM per platform"},
	}
	for _, promise := range promises {
		if !strings.Contains(read(promise.path), promise.claim) {
			t.Fatalf("%s no longer promises %q; this test has stopped checking anything",
				promise.path, promise.claim)
		}
	}

	// Act & Assert
	required := []struct {
		fragment string
		because  string
	}{
		{"actions/attest-build-provenance",
			"the documented `gh attestation verify` has nothing to verify against"},
		{"id-token: write",
			"the attestation step cannot obtain the identity it signs with"},
		{"attestations: write",
			"the attestation is generated and then cannot be stored"},
		{"cyclonedx-gomod",
			"no SBOM is produced for the platforms the security model says are described"},
	}
	for _, requirement := range required {
		if !strings.Contains(workflow, requirement.fragment) {
			t.Errorf("the release workflow no longer contains %q, so %s",
				requirement.fragment, requirement.because)
		}
	}

	// Binaries and SBOMs are produced by two separate loops. They are only
	// guaranteed to cover the same platforms while both walk the same list, and
	// a second hardcoded list is exactly how a target acquires a binary with no
	// SBOM — or an SBOM describing something nobody shipped.
	if declarations := strings.Count(workflow, "TARGETS:"); declarations != 1 {
		t.Errorf("the release workflow declares the target list %d times; it must be declared once", declarations)
	}
	if walks := strings.Count(workflow, "for target in $TARGETS"); walks != 3 {
		t.Errorf("%d loops walk $TARGETS; build, SBOM, and packaging must each walk it", walks)
	}
}

// A top-level command with no section is caught above. A subcommand with no
// mention was not, and that is where the surface actually grows: `vat harness`
// has had a section since the first release, so two commands could be added
// under it and documented nowhere while every contract test stayed green.
//
// The reference writes subcommands into a fenced usage block rather than as
// headings, so the check is for the invocation appearing at all. That is weaker
// than the heading rule above and still forecloses the case that matters —
// shipping a command nobody can find.
func TestEverySubcommandIsNamedInTheReference(t *testing.T) {
	// Arrange
	reference := readReference(t)

	// Act & Assert
	walkCommands(Root(), nil, func(command *Command, path []string) {
		if len(path) < 2 || command.Hidden {
			return
		}
		invocation := "vat " + strings.Join(path, " ")
		if !strings.Contains(reference, invocation) {
			t.Errorf("%s exists and %s never names it", invocation, referencePath)
		}
	})
}

// go.mod's retract block is read by the Go toolchain and by nobody else. A
// person deciding whether the version they installed is one of the unsafe ones
// reads the release notes, and every version this module has retracted — for a
// disclosed credential, for writes outside the workspace root, for a wrong
// version stamp — was absent from them.
//
// A retraction is the one release fact that cannot be corrected later: the
// version is frozen on the proxy, so the note explaining it is the whole of
// what a reader will ever get.
func TestEveryRetractedVersionIsExplainedInTheChangelog(t *testing.T) {
	// Arrange
	retracted := retractedVersions(t)
	if len(retracted) == 0 {
		t.Skip("nothing is retracted")
	}
	sections := changelogSections(t)

	// Act & Assert
	for _, version := range retracted {
		// Its own section, and that section saying so. A bare mention is not
		// enough: every retracted version was already named somewhere in this
		// file, in prose about the release that fixed it, which tells a reader
		// asking "is what I installed safe" nothing at all.
		body, released := sections[strings.TrimPrefix(version, "v")]
		if !released {
			t.Errorf("go.mod retracts %s and %s has no section for it", version, changelogPath)
			continue
		}
		if !strings.Contains(body, "Retracted") {
			t.Errorf("%s documents %s without saying it was retracted", changelogPath, version)
		}
	}
}

// changelogSections maps each released version to the body of its section.
func changelogSections(t *testing.T) map[string]string {
	t.Helper()
	content, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	sections := map[string]string{}
	heading := regexp.MustCompile(`^## \[([^\]]+)]`)
	var current string
	var body strings.Builder
	flush := func() {
		if current != "" {
			sections[current] = body.String()
		}
		body.Reset()
	}
	for _, line := range strings.Split(string(content), "\n") {
		if match := heading.FindStringSubmatch(line); match != nil {
			flush()
			current = match[1]
			continue
		}
		if current != "" {
			body.WriteString(line + "\n")
		}
	}
	flush()
	return sections
}

const changelogPath = "../../CHANGELOG.md"

// retractedVersions reads go.mod and returns every version the module retracts,
// expanding a `[low, high]` range across its patch numbers so a version named
// only by being inside one is still checked.
func retractedVersions(t *testing.T) []string {
	t.Helper()
	content, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	block := regexp.MustCompile(`(?s)retract\s*\((.*?)\n\)`).FindStringSubmatch(string(content))
	if block == nil {
		return nil
	}
	var versions []string
	ranged := regexp.MustCompile(`\[\s*v(\d+)\.(\d+)\.(\d+)\s*,\s*v(\d+)\.(\d+)\.(\d+)\s*]`)
	single := regexp.MustCompile(`^\s*(v\d+\.\d+\.\d+)\s*$`)
	for _, line := range strings.Split(block[1], "\n") {
		if bounds := ranged.FindStringSubmatch(line); bounds != nil {
			numbers := make([]int, 6)
			for i := range numbers {
				numbers[i], _ = strconv.Atoi(bounds[i+1])
			}
			// Only a patch range is expanded. A range spanning a minor version
			// cannot be enumerated from the text alone, so its endpoints stand
			// rather than a guess at what lies between them.
			if numbers[0] != numbers[3] || numbers[1] != numbers[4] {
				versions = append(versions, "v"+bounds[1]+"."+bounds[2]+"."+bounds[3],
					"v"+bounds[4]+"."+bounds[5]+"."+bounds[6])
				continue
			}
			for patch := numbers[2]; patch <= numbers[5]; patch++ {
				versions = append(versions, fmt.Sprintf("v%d.%d.%d", numbers[0], numbers[1], patch))
			}
			continue
		}
		if one := single.FindStringSubmatch(stripComment(line)); one != nil {
			versions = append(versions, one[1])
		}
	}
	return versions
}

func stripComment(line string) string {
	if at := strings.Index(line, "//"); at >= 0 {
		return line[:at]
	}
	return line
}
