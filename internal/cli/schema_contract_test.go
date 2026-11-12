package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/takealook97/vat/internal/brain"
	"github.com/takealook97/vat/internal/changeset"
	"github.com/takealook97/vat/internal/fsx"
	"github.com/takealook97/vat/internal/manifest"
)

// docs/SPEC.md says these formats are specified so vat need not be the only
// reader. A published JSON Schema is what makes that true for a reader that is
// not a person — an editor, a CI check, an implementation in another language.
//
// A schema is also the easiest document here to leave behind: nothing breaks
// when a field is added to a Go type and not to the schema, and the tools
// trusting it simply begin rejecting files vat itself wrote. That failure is
// silent at both ends, so the schemas are read here and held against the types
// they describe, field by field and value by value.
//
// They live beside the other contract tests rather than in each format's own
// package because one set of helpers reads every schema, and a copy of a
// helper per format is how the checks stop agreeing on what they check.

const schemaDir = "../../schemas/"

// schemaBase is the prefix every schema's $id shares. Published manifests and
// records point at these URLs, so a file renamed without its $id following is
// a 404 in somebody's editor and a green suite here.
const schemaBase = "https://raw.githubusercontent.com/takealook97/vat/main/schemas/"

const (
	manifestSchema  = "vat-manifest-v1.schema.json"
	changesetSchema = "vat-changeset-v1.schema.json"
	brainSchema     = "vat-brain-record-1.schema.json"
	markerSchema    = "vat-brain-marker-1.schema.json"
)

func loadSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(schemaDir + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatalf("%s is not valid JSON: %v", name, err)
	}
	return schema
}

// schemaNode walks a path of object keys, failing rather than panicking so a
// renamed section reports where it was expected instead of a nil dereference.
func schemaNode(t *testing.T, schema map[string]any, path ...string) map[string]any {
	t.Helper()
	current := schema
	for i, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("the schema has no object at %q", strings.Join(path[:i+1], "."))
		}
		current = next
	}
	return current
}

// yamlFields returns the wire names a struct actually serialises to.
func yamlFields(t *testing.T, sample any) []string {
	t.Helper()
	typ := reflect.TypeOf(sample)
	if typ.Kind() != reflect.Struct {
		t.Fatalf("%T is not a struct", sample)
	}
	names := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		tag := typ.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		names = append(names, strings.Split(tag, ",")[0])
	}
	sort.Strings(names)
	return names
}

func schemaPropertyNames(properties map[string]any) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func schemaEnum(t *testing.T, schema map[string]any, path ...string) []string {
	t.Helper()
	raw, ok := schemaNode(t, schema, path...)["enum"].([]any)
	if !ok {
		t.Fatalf("no enum at %s", strings.Join(path, "."))
	}
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		values = append(values, value.(string))
	}
	sort.Strings(values)
	return values
}

func sortedStrings(values ...string) []string {
	sorted := append([]string{}, values...)
	sort.Strings(sorted)
	return sorted
}

// TestPublishedSchemasDescribeEveryFieldTheTypesHave is the guard that matters:
// a field added to a Go type and forgotten in the schema makes every tool
// validating against that schema reject a file vat wrote.
func TestPublishedSchemasDescribeEveryFieldTheTypesHave(t *testing.T) {
	// Arrange
	cases := []struct {
		schema string
		what   string
		sample any
		path   []string
	}{
		{manifestSchema, "the manifest", manifest.Manifest{}, []string{"properties"}},
		{manifestSchema, "workspace", manifest.Workspace{}, []string{"properties", "workspace", "properties"}},
		{manifestSchema, "policy", manifest.Policy{}, []string{"properties", "policy", "properties"}},
		{manifestSchema, "policy.sync", manifest.SyncPolicy{}, []string{"properties", "policy", "properties", "sync", "properties"}},
		{manifestSchema, "policy.trust", manifest.TrustPolicy{}, []string{"properties", "policy", "properties", "trust", "properties"}},
		{manifestSchema, "policy.brain", manifest.BrainPolicy{}, []string{"properties", "policy", "properties", "brain", "properties"}},
		{manifestSchema, "policy.changeset", manifest.ChangesetPolicy{}, []string{"properties", "policy", "properties", "changeset", "properties"}},
		{manifestSchema, "policy.gates", manifest.GatePolicy{}, []string{"properties", "policy", "properties", "gates", "properties"}},
		{manifestSchema, "a repository", manifest.Repo{}, []string{"$defs", "repo", "properties"}},

		{changesetSchema, "the record", changeset.Changeset{}, []string{"properties"}},
		{changesetSchema, "a participant", changeset.Participant{}, []string{"$defs", "participant", "properties"}},
		{changesetSchema, "a check run", changeset.CheckRun{}, []string{"$defs", "check", "properties"}},

		{brainSchema, "record front matter", brain.Metadata{}, []string{"properties"}},
	}

	// Act & Assert
	for _, testCase := range cases {
		schema := loadSchema(t, testCase.schema)
		described := schemaPropertyNames(schemaNode(t, schema, testCase.path...))
		serialised := yamlFields(t, testCase.sample)
		if !reflect.DeepEqual(described, serialised) {
			t.Errorf("%s (%s): the schema describes %v, the type serialises %v",
				testCase.what, testCase.schema, described, serialised)
		}
	}
}

// An enum is a copy of a list that lives in Go, and copies drift. A value vat
// accepts but the schema rejects makes the schema report a working file as
// invalid, which is worse than having published no schema at all.
func TestPublishedSchemaEnumsMatchTheValuesVatAccepts(t *testing.T) {
	// Arrange
	roles := make([]string, 0, len(manifest.Roles()))
	for _, role := range manifest.Roles() {
		roles = append(roles, string(role))
	}
	sort.Strings(roles)

	cases := []struct {
		schema string
		what   string
		path   []string
		accept []string
	}{
		{manifestSchema, "roles", []string{"$defs", "role"}, roles},
		{manifestSchema, "gates", []string{"$defs", "gate"},
			sortedStrings(manifest.GateManual, manifest.GateAuto)},
		{changesetSchema, "record statuses", []string{"$defs", "status"},
			sortedStrings(string(changeset.StatusOpen), string(changeset.StatusVerified),
				string(changeset.StatusClosed), string(changeset.StatusRolledBack),
				string(changeset.StatusAbandoned))},
		{brainSchema, "record statuses", []string{"$defs", "status"},
			sortedStrings(string(brain.StatusProvisional), string(brain.StatusActive),
				string(brain.StatusStale), string(brain.StatusQuarantined),
				string(brain.StatusSuperseded), string(brain.StatusRevoked),
				string(brain.StatusResolved))},
		{brainSchema, "claim kinds", []string{"$defs", "claimKind"},
			sortedStrings(string(brain.ClaimCurrentState), string(brain.ClaimHistorical),
				string(brain.ClaimIntent))},
		{brainSchema, "promotion gates", []string{"$defs", "promotionGate"},
			sortedStrings(brain.PromotionGates()...)},
	}

	// Act & Assert
	for _, testCase := range cases {
		schema := loadSchema(t, testCase.schema)
		if got := schemaEnum(t, schema, testCase.path...); !reflect.DeepEqual(got, testCase.accept) {
			t.Errorf("%s in %s: the schema allows %v, vat accepts %v",
				testCase.what, testCase.schema, got, testCase.accept)
		}
	}
}

// The version bound is the one number deciding whether another implementation
// reading this schema accepts a manifest vat wrote.
func TestThePublishedManifestSchemaPinsTheVersionVatWrites(t *testing.T) {
	// Arrange
	version := schemaNode(t, loadSchema(t, manifestSchema), "properties", "version")

	// Act
	maximum, ok := version["maximum"].(float64)

	// Assert
	if !ok {
		t.Fatal("the schema puts no upper bound on version, so it describes versions that do not exist yet")
	}
	if int(maximum) != manifest.SchemaVersion {
		t.Errorf("the schema tops out at version %d, vat writes %d", int(maximum), manifest.SchemaVersion)
	}
}

// A schema is a promise to other tools. Where it says a key is required, vat
// must refuse a file without it — otherwise anyone validating vat.yaml against
// vat's own schema gets a different answer from vat, and the file that passes
// one fails the other.
func TestVatRefusesWhatItsOwnSchemaCallsRequired(t *testing.T) {
	// Arrange
	schema := loadSchema(t, manifestSchema)
	required, ok := schema["required"].([]any)
	if !ok || len(required) == 0 {
		t.Fatal("the manifest schema requires nothing at all")
	}
	valid := manifest.Default("acme")

	for _, key := range required {
		name, _ := key.(string)
		t.Run(name, func(t *testing.T) {
			// Act: clear that key and nothing else.
			missing := valid
			switch name {
			case "version":
				missing.Version = 0
			case "workspace":
				missing.Workspace.Name = ""
			default:
				t.Skipf("no case for required key %q; add one when the schema grows it", name)
			}
			err := manifest.Validate(missing)

			// Assert
			if err == nil {
				t.Errorf("the schema requires %q but vat accepts a manifest without it", name)
			}
		})
	}
}

// The caps are not decoration. Each identifier is pasted into a path, and
// without a bound the failure was left to the filesystem.
func TestPublishedSchemasCapIdentifiersWhereVatDoes(t *testing.T) {
	// Arrange
	cases := []struct {
		schema   string
		what     string
		path     []string
		validate func(string) error
	}{
		{manifestSchema, "a repository name", []string{"$defs", "repo", "properties", "name"},
			manifest.ValidateRepoName},
		{brainSchema, "a record identifier", []string{"properties", "id"},
			brain.ValidateID},
	}

	// Act & Assert
	for _, testCase := range cases {
		schema := loadSchema(t, testCase.schema)
		maxLength, ok := schemaNode(t, schema, testCase.path...)["maxLength"].(float64)
		if !ok {
			t.Errorf("%s: the schema puts no cap on %s", testCase.schema, testCase.what)
			continue
		}
		longest := strings.Repeat("a", int(maxLength))
		if err := testCase.validate(longest); err != nil {
			t.Errorf("%s: the schema allows a %d-character %s that vat refuses: %v",
				testCase.schema, int(maxLength), testCase.what, err)
		}
		if err := testCase.validate(longest + "a"); err == nil {
			t.Errorf("%s: vat accepts a %d-character %s that the schema refuses",
				testCase.schema, int(maxLength)+1, testCase.what)
		}
	}
}

// Every schema names itself, and files in the wild point at that name. Three
// things have to agree for a modeline to keep working — the constant vat
// writes, the file in this repository, and the $id inside it — and nothing
// else would notice any one of them moving. An editor would simply stop
// validating, quietly, which is the failure a modeline exists to prevent.
func TestPublishedSchemasAreNamedByWhatPointsAtThem(t *testing.T) {
	// Arrange
	cases := []struct {
		schema string
		// url is what vat writes into the files it saves; empty where the
		// format carries no modeline, as Markdown front matter cannot.
		url string
	}{
		{manifestSchema, manifest.SchemaURL},
		{changesetSchema, changeset.SchemaURL},
		{brainSchema, ""},
		{markerSchema, ""},
	}

	// Act & Assert
	for _, testCase := range cases {
		id, _ := loadSchema(t, testCase.schema)["$id"].(string)
		if want := schemaBase + testCase.schema; id != want {
			t.Errorf("%s calls itself %q, want %q", testCase.schema, id, want)
		}
		if testCase.url == "" {
			continue
		}
		if testCase.url != id {
			t.Errorf("vat writes %q into saved files, but %s calls itself %q",
				testCase.url, testCase.schema, id)
		}
	}
}

// The modelines themselves, checked against what the writers actually emit.
func TestSavedFilesCarryTheSchemaModeline(t *testing.T) {
	// Arrange
	saved, err := manifest.Marshal(manifest.Manifest{
		Version:   manifest.SchemaVersion,
		Workspace: manifest.Workspace{Name: "acme"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Act & Assert
	if want := "# yaml-language-server: $schema=" + manifest.SchemaURL; !strings.Contains(string(saved), want) {
		t.Errorf("a saved manifest does not carry the schema modeline:\n%s", saved)
	}
}

// examples/workspace is the layout the documentation points at and the one
// people copy, and until now nothing read it. It was written by hand, so it
// had drifted from what vat writes in the way hand-written YAML always does:
// `opened_at: 2026-08-11` unquoted.
//
// vat reads that correctly, which is what hid it — yaml.v3 coerces the scalar
// because the field it lands in is a string. Every other reader resolves the
// same scalar to a timestamp, so a conforming implementation reading these
// files gets a date where docs/SPEC.md §3 says a YYYY-MM-DD string, and §1
// promises implementations can read each other's files. None of these formats
// has a field that is genuinely a timestamp, so a !!timestamp anywhere in one
// is that bug and no other.
func TestPublishedExamplesHoldNothingAnotherReaderWouldTypeDifferently(t *testing.T) {
	// Arrange
	files := []string{
		"../../examples/workspace/vat.yaml",
		"../../examples/workspace/changesets/CS-0001.yaml",
	}
	for _, dir := range []string{"goals", "gaps", "decisions"} {
		matches, err := filepath.Glob("../../examples/workspace/brain/" + dir + "/*.md")
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		files = append(files, matches...)
	}
	if len(files) < 3 {
		t.Fatal("the example workspace lost its files, and this test stopped checking anything")
	}

	// Act & Assert
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		body := content
		if strings.HasSuffix(path, ".md") {
			front, _, found := strings.Cut(strings.TrimPrefix(string(content), "---\n"), "\n---")
			if !found {
				t.Errorf("%s has no front matter", path)
				continue
			}
			body = []byte(front)
		}
		var document yaml.Node
		if err := yaml.Unmarshal(body, &document); err != nil {
			t.Errorf("%s is not valid YAML: %v", path, err)
			continue
		}
		for _, where := range scalarsTypedAsSomethingElse(&document, "") {
			t.Errorf("%s: %s would be read as a %s by another implementation; quote it",
				path, where.path, where.tag)
		}
	}
}

type mistypedScalar struct {
	path string
	tag  string
}

// scalarsTypedAsSomethingElse returns every scalar a generic YAML reader would
// resolve to a timestamp. Booleans and numbers are left alone: these formats do
// have genuine boolean and integer fields, and a timestamp field they do not.
func scalarsTypedAsSomethingElse(node *yaml.Node, path string) []mistypedScalar {
	if node.Kind == yaml.ScalarNode {
		if node.Tag == "!!timestamp" {
			return []mistypedScalar{{path: path, tag: "timestamp"}}
		}
		return nil
	}
	var found []mistypedScalar
	for i, child := range node.Content {
		where := fmt.Sprintf("%s[%d]", path, i)
		if node.Kind == yaml.MappingNode {
			if i%2 == 0 {
				continue
			}
			where = strings.TrimPrefix(path+"."+node.Content[i-1].Value, ".")
		}
		found = append(found, scalarsTypedAsSomethingElse(child, where)...)
	}
	return found
}

// The published example is the only worked workspace a reader can copy, and
// nothing checked what vat writes into it. A change to `graph.json` left the
// example behind — it gained two fields and the example kept the old shape — so
// the first thing a reader who copied it saw was `vat lint` failing on drift in
// a file they had not touched. The schema tests beside this one check the
// example's hand-written files; this checks the one vat generates.
func TestThePublishedExampleCarriesTheGraphThisBuildWrites(t *testing.T) {
	// Arrange
	root := "../../examples/workspace/brain"
	store, err := brain.Load(root)
	if err != nil {
		t.Fatalf("load the example brain: %v", err)
	}
	if len(store.Records) == 0 {
		t.Fatal("the example brain lost its records, and this test stopped checking anything")
	}
	// The window comes from the example's own manifest: a hard-coded one would
	// keep passing after someone edits vat.yaml, while `vat lint` on the example
	// reports drift.
	example, err := manifest.Load("../../examples/workspace/vat.yaml")
	if err != nil {
		t.Fatalf("load the example manifest: %v", err)
	}
	policy := brain.CheckPolicy{StaleAfterDays: example.Policy.Brain.StaleAfterDays}
	committed, err := os.ReadFile(filepath.Join(root, brain.GraphFile))
	if err != nil {
		t.Fatalf("read the example %s: %v", brain.GraphFile, err)
	}

	// Act
	rendered, err := brain.RenderGraph(store, policy)
	if err != nil {
		t.Fatalf("RenderGraph: %v", err)
	}

	// Assert
	if fsx.NormaliseNewlines(string(committed)) != string(rendered) {
		t.Errorf("the published example's %s differs from this build", brain.GraphFile)
	}
	current, err := os.ReadFile(filepath.Join(root, brain.CurrentFile))
	if err != nil {
		t.Fatal(err)
	}
	if fsx.NormaliseNewlines(string(current)) != brain.RenderCurrent(store, policy) {
		t.Error("the published example's CURRENT.md differs from this build")
	}
}

// schemaAnnotations carry no constraint, so the validator below may pass over
// them. Every other keyword it does not implement fails the test: a validator
// that silently skips a keyword reports a document valid against a schema it
// never fully read.
var schemaAnnotations = map[string]bool{
	"$schema": true, "$id": true, "$defs": true, "title": true, "description": true,
}

// validateAgainstSchema checks a decoded document against the keywords this
// project's schemas use. It exists because vat keeps one runtime dependency;
// it is a test helper, not a validator anyone should reuse.
func validateAgainstSchema(t *testing.T, root, node map[string]any, value any, at string) []string {
	t.Helper()
	if ref, ok := node["$ref"].(string); ok {
		target := strings.TrimPrefix(ref, "#/")
		if target == ref {
			t.Fatalf("%s: only local $ref is supported, got %q", at, ref)
		}
		return validateAgainstSchema(t, root, schemaNode(t, root, strings.Split(target, "/")...), value, at)
	}
	var problems []string
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if schemaAnnotations[key] {
			continue
		}
		switch key {
		case "type":
			if !schemaTypeMatches(node[key].(string), value) {
				problems = append(problems, fmt.Sprintf("%s: want %s, got %T", at, node[key], value))
			}
		case "enum":
			if !containsValue(node[key].([]any), value) {
				problems = append(problems, fmt.Sprintf("%s: %v is not one of %v", at, value, node[key]))
			}
		case "required":
			object, _ := value.(map[string]any)
			for _, name := range node[key].([]any) {
				if _, ok := object[name.(string)]; !ok {
					problems = append(problems, fmt.Sprintf("%s: missing %s", at, name))
				}
			}
		case "properties":
			problems = append(problems, validateObject(t, root, node, value, at)...)
		case "additionalProperties":
			// Applied together with properties, so an object is walked once.
			if _, ok := node["properties"]; !ok {
				problems = append(problems, validateObject(t, root, node, value, at)...)
			}
		case "items":
			list, _ := value.([]any)
			for i, item := range list {
				problems = append(problems, validateAgainstSchema(t, root, node[key].(map[string]any), item, fmt.Sprintf("%s[%d]", at, i))...)
			}
		case "minimum":
			if number, ok := value.(float64); ok && number < node[key].(float64) {
				problems = append(problems, fmt.Sprintf("%s: %v is below %v", at, value, node[key]))
			}
		case "minLength", "maxLength", "pattern":
			// Length and pattern bounds are held against vat's own validators
			// by the identifier tests above; these fixtures stay inside them.
		default:
			t.Fatalf("%s: the schema uses %q, which this validator does not implement", at, key)
		}
	}
	return problems
}

// validateObject applies properties and additionalProperties to one object.
func validateObject(t *testing.T, root, node map[string]any, value any, at string) []string {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	properties, _ := node["properties"].(map[string]any)
	additional, declared := node["additionalProperties"].(bool)
	var problems []string
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if property, ok := properties[name].(map[string]any); ok {
			problems = append(problems, validateAgainstSchema(t, root, property, object[name], at+"."+name)...)
		} else if declared && !additional {
			problems = append(problems, fmt.Sprintf("%s: %s is not allowed", at, name))
		}
	}
	return problems
}

func schemaTypeMatches(want string, value any) bool {
	switch want {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer", "number":
		_, ok := value.(float64)
		return ok
	}
	return false
}

func containsValue(values []any, value any) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// frontMatterAsJSON decodes YAML the way a JSON Schema validator sees it: a
// timestamp is a string, not a time value.
func frontMatterAsJSON(t *testing.T, header string) any {
	t.Helper()
	var decoded map[string]any
	if err := yaml.Unmarshal([]byte(header), &decoded); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

// docs/SPEC.md lets a record carry keys vat does not define and requires them
// preserved. A schema that rejects them made every externally validated
// record a publishing job wrote invalid, while vat itself accepted it.
func TestBrainRecordSchemaAcceptsExtensionKeys(t *testing.T) {
	// Arrange
	schema := loadSchema(t, brainSchema)
	base := "id: M-0001\nstatus: active\ndate: 2026-11-01\nclaim_kind: historical\n" +
		"source_external: true\nobserved_at: 2026-11-02\nrefs: [D-0001]\n"
	extensions := "project: demo-project\nrun_id: run-0001\nmodel_family: demo-model\n" +
		"origin: chat:DEMO/0001\nchannel: demo-channel\ncaptured_at: 2026-11-01T09:00:00Z\n" +
		"source_url: https://example.com/thread/0001\n"
	cases := []struct {
		name   string
		header string
		valid  bool
	}{
		{"defined fields", base, true},
		{"defined fields and extension keys", base + extensions, true},
		{"an unknown status", strings.Replace(base, "status: active", "status: approved", 1) + extensions, false},
		{"a defined field of the wrong type", strings.Replace(base, "source_external: true", "source_external: yes-please", 1), false},
		{"no id", strings.Replace(base, "id: M-0001\n", "", 1) + extensions, false},
		{"a promotion gate", base + "promoted_via: auto\n", true},
		{"an unknown promotion gate", base + "promoted_via: bot\n", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// Act
			problems := validateAgainstSchema(t, schema, schema, frontMatterAsJSON(t, testCase.header), "$")

			// Assert
			if testCase.valid && len(problems) > 0 {
				t.Errorf("rejected a record the specification accepts: %v", problems)
			}
			if !testCase.valid && len(problems) == 0 {
				t.Error("accepted a record whose defined fields are wrong")
			}
		})
	}
}

// The marker's policy block is read by ReadMarkerPolicy and refused key by key.
// A schema that lists a key vat refuses, or misses one vat reads, makes an
// editor disagree with the tool about the one file a standalone brain is
// governed by.
func TestBrainMarkerSchemaDescribesThePolicyVatReads(t *testing.T) {
	// Arrange
	schema := loadSchema(t, markerSchema)
	policyKeys := sortedStrings(brain.PolicyKeys()...)
	tagged := make([]string, 0, len(policyKeys))
	typ := reflect.TypeOf(brain.MarkerPolicy{})
	for i := range typ.NumField() {
		tagged = append(tagged, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
	}
	sort.Strings(tagged)
	root := t.TempDir()
	defaults := brain.DefaultMarkerPolicy()
	if _, err := brain.InitWithPolicy(root, testNow, &defaults); err != nil {
		t.Fatal(err)
	}
	written := readFile(t, filepath.Join(root, brain.MarkerFile))

	// Act & Assert: the declared keys, in both directions.
	described := schemaPropertyNames(schemaNode(t, schema, "properties", "policy", "properties"))
	if !reflect.DeepEqual(described, policyKeys) || !reflect.DeepEqual(tagged, policyKeys) {
		t.Errorf("the schema describes %v, vat reads %v, MarkerPolicy carries %v", described, policyKeys, tagged)
	}
	if got := schemaEnum(t, schema, "properties", "policy", "properties", brain.PolicyPromotionGate); !reflect.DeepEqual(got, sortedStrings(brain.PromotionGates()...)) {
		t.Errorf("promotion_gate: the schema allows %v", got)
	}
	versions, _ := schemaNode(t, schema, "properties", "schema")["enum"].([]any)
	if len(versions) != 1 || versions[0] != float64(brain.SchemaVersion) {
		t.Errorf("the schema pins marker schema %v, vat writes %d", versions, brain.SchemaVersion)
	}

	// Act & Assert: what vat writes, and what it refuses.
	cases := []struct {
		name   string
		marker string
		valid  bool
	}{
		{"the marker vat writes with a policy", written, true},
		{"a marker without a policy", "schema: 1\n", true},
		{"a marker that predates versioning", "# a brain\n", true},
		{"an unknown policy key", written + "  require_everything: true\n", false},
		{"an unknown gate", strings.Replace(written, "promotion_gate: manual", "promotion_gate: sometimes", 1), false},
		{"zero days", strings.Replace(written, "stale_after_days: 90", "stale_after_days: 0", 1), false},
		{"a boolean spelled as text", strings.Replace(written, "require_external_evidence: false", "require_external_evidence: \"no\"", 1), false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var document any = map[string]any{}
			if decoded := frontMatterAsJSON(t, testCase.marker); decoded != nil {
				document = decoded
			}
			problems := validateAgainstSchema(t, schema, schema, document, "$")
			if testCase.valid && len(problems) > 0 {
				t.Errorf("rejected a marker vat accepts: %v", problems)
			}
			if !testCase.valid && len(problems) == 0 {
				t.Error("accepted a marker vat refuses")
			}
		})
	}
}
