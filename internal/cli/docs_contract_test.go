package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/fit"
	"github.com/takealook97/vat/internal/harness"
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

// The demo at the top of the README is the first thing anybody sees, and its own
// comment says to regenerate it after changing any of the output it shows.
// Nothing checked that. `vat init` began seeding procedures and the demo went on
// showing a session that no longer happens — in the one asset a reader takes as
// evidence the tool does what the page claims.
//
// Checked against the files init writes unconditionally rather than against the
// whole transcript: revisions and repository names in the recording are a
// scenario, and asserting those would fail for reasons nobody should have to fix.
func TestTheDemoShowsTheFilesInitAlwaysWrites(t *testing.T) {
	// Arrange: every file that shows an init transcript, not only the recording.
	// The README carries one of its own, and the guard that covered the demo
	// alone let that one go stale for exactly as long as nobody read it.
	candidates, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	candidates = append(candidates, "../../README.md", demoPath)
	transcripts := map[string]string{}
	for _, path := range candidates {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// An init transcript, not a mention of the command: the line only the
		// command itself prints.
		if strings.Contains(string(content), "OK    vat.yaml") {
			transcripts[path] = string(content)
		}
	}
	if len(transcripts) == 0 {
		t.Fatal("no `vat init` transcript found anywhere; they changed shape and this test stopped checking anything")
	}

	unconditional := []string{"vat.yaml", ".gitignore", "AGENTS.md", "CLAUDE.md"}
	starters := harness.StarterSkills()
	written := make([]string, 0, len(unconditional)+2*len(starters))
	written = append(written, unconditional...)
	for _, skill := range starters {
		written = append(written,
			harness.SkillsDir+"/"+skill.Name+"/"+harness.SkillFile,
			harness.ClaudeSkillDir+"/"+skill.Name+"/"+harness.SkillFile)
	}

	// Act & Assert
	for source, transcript := range transcripts {
		for _, path := range written {
			if !strings.Contains(transcript, path) {
				t.Errorf("`vat init` writes %s and the transcript in %s does not show it; re-record it",
					path, source)
			}
		}
	}
}

const demoPath = "../../docs/assets/demo.svg"

// Changing what a summary line prints silently invalidates every sample of it,
// and the samples are what a reader takes as evidence the page describes the
// real thing. The sync summary gained a bucket and the README and the demo went
// on showing the old shape.
//
// The buckets are checked, not the numbers: the numbers belong to a scenario,
// and asserting those would fail for reasons nobody should have to fix.
func TestEverySampleOfTheSyncSummaryNamesTheBucketsItPrints(t *testing.T) {
	// Arrange
	buckets := []string{"advanced", "already current", "left alone on purpose"}
	summary := regexp.MustCompile(`\d+ advanced[^<\n]*`)

	// Act & Assert
	for _, path := range []string{"../../README.md", demoPath} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		samples := summary.FindAllString(string(content), -1)
		if len(samples) == 0 {
			continue
		}
		for _, sample := range samples {
			for _, bucket := range buckets {
				if !strings.Contains(sample, bucket) {
					t.Errorf("%s shows %q, which does not name %q; re-record it",
						path, sample, bucket)
				}
			}
		}
	}
}

// AGENTS.md opens by telling a session to read the architecture map, and the map
// is a claim about what exists that nothing checked. A package missing from it
// sends every session looking in the wrong place, and a line naming a package
// that was deleted sends them looking for nothing at all.
func TestTheArchitectureMapNamesExactlyThePackagesThatExist(t *testing.T) {
	// Arrange
	entries, err := os.ReadDir("../../internal")
	if err != nil {
		t.Fatalf("read internal: %v", err)
	}
	contract, err := os.ReadFile("../../AGENTS.md")
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	mapped := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^internal/(\w+)`).FindAllStringSubmatch(string(contract), -1) {
		mapped[match[1]] = true
	}
	if len(mapped) == 0 {
		t.Fatal("no architecture map found in AGENTS.md; it changed shape and this test stopped checking anything")
	}

	// Act & Assert
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !mapped[entry.Name()] {
			t.Errorf("internal/%s exists and the map in AGENTS.md does not name it", entry.Name())
		}
		delete(mapped, entry.Name())
	}
	for name := range mapped {
		t.Errorf("AGENTS.md maps internal/%s, which does not exist", name)
	}
}

// An anchor that resolves to nothing sends a reader to the top of the page and
// looks like the page is broken. There is no build step over this Markdown, so
// nothing else would ever say.
func TestEveryInternalLinkInTheDocsResolvesToAHeading(t *testing.T) {
	// Arrange
	anchor := regexp.MustCompile(`\]\(#([a-z0-9-]+)\)`)
	heading := regexp.MustCompile(`(?m)^#{1,6} +(.+?)\s*$`)
	paths, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	paths = append(paths, "../../README.md", "../../CONTRIBUTING.md", "../../AGENTS.md")

	// Act & Assert
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		headings := map[string]bool{}
		for _, match := range heading.FindAllStringSubmatch(string(content), -1) {
			headings[slugify(match[1])] = true
		}
		for _, match := range anchor.FindAllStringSubmatch(string(content), -1) {
			if !headings[match[1]] {
				t.Errorf("%s links to #%s, which is not a heading in it", path, match[1])
			}
		}
	}
}

// slugify renders a heading the way a forge does when it builds an anchor:
// lower case, punctuation dropped, spaces to hyphens.
func slugify(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-':
			b.WriteRune('-')
		}
	}
	return b.String()
}

// The thresholds `vat fit` prints are the sentence a reader decides on, and the
// README's sample of them had drifted from the code — "agents work across more
// than one repository" where the tool says "coding agents work in this code at
// all", which is a different recommendation to a different person.
//
// The thresholds are checked and the reasons are not: a reason names the
// reader's own numbers, and holding a sample to those would fail for reasons
// nobody should have to fix.
func TestEverySampleThresholdIsOneTheAdvisorPrints(t *testing.T) {
	// Arrange
	printed := map[string]bool{}
	for _, verdict := range fit.Assess(fit.Signals{}) {
		printed[verdict.Threshold] = true
	}
	if len(printed) == 0 {
		t.Fatal("the advisor produced no thresholds; it changed shape and this test stopped checking anything")
	}
	line := regexp.MustCompile(`(?m)^ +threshold: (.+?)\s*$`)

	// Act & Assert
	for _, path := range []string{"../../README.md", "../../docs/ADOPTION.md", "../../docs/FAQ.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, match := range line.FindAllStringSubmatch(string(content), -1) {
			if !printed[match[1]] {
				t.Errorf("%s shows the threshold %q, which `vat fit` does not print", path, match[1])
			}
		}
	}
}

// A sample is what a reader types. The reference is held to the command tree in
// both directions; every other document showing an invocation was not, so a
// renamed or removed command could sit in the README indefinitely with nothing
// to say.
//
// Only the verb is checked. A sample's later words are arguments — a repository
// name, a query term, a record kind — and nothing in their shape separates them
// from a subcommand, so asserting them either passes everything or reports
// somebody's example query as a missing command. Deeper paths are covered where
// they can be: the reference is compared against the tree both ways, and the
// seeded procedures are held to full invocations because those are vat's own
// words rather than a reader's.
func TestEveryCommandShownInTheDocsExists(t *testing.T) {
	// Arrange
	top := map[string]bool{}
	for _, sub := range Root().Subcommands {
		top[sub.Name] = true
	}
	if len(top) == 0 {
		t.Fatal("the command tree is empty; it changed shape and this test stopped checking anything")
	}
	// Anchored on the two forms that are an invocation rather than prose about
	// the tool: inside a code span, or after a shell prompt. "vat reads the
	// manifest" is a sentence, and holding sentences to the command tree is how
	// a guard earns being switched off.
	invocation := regexp.MustCompile("(?m)(?:`|^\\$ )vat ([a-z][a-z-]*)")
	paths, err := filepath.Glob("../../docs/*.md")
	if err != nil {
		t.Fatalf("glob docs: %v", err)
	}
	paths = append(paths, "../../README.md", "../../CONTRIBUTING.md", "../../AGENTS.md")

	// Act & Assert
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, match := range invocation.FindAllStringSubmatch(string(content), -1) {
			if !top[match[1]] {
				t.Errorf("%s shows `vat %s`, which is not a command this binary has",
					path, match[1])
			}
		}
	}
}
