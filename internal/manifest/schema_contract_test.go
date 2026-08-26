package manifest_test

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/manifest"
)

// docs/SPEC.md says these formats are specified so vat need not be the only
// reader. A published JSON Schema is what makes that true for a reader that is
// not a person — an editor, a CI check, an implementation in another language.
//
// It is also the easiest document in the repository to leave behind: nothing
// breaks when a field is added to the Go struct and not to the schema, and the
// tools trusting it simply start rejecting valid manifests, or accepting
// invalid ones. So the schema is read here and held against the types it
// describes, field by field.
const schemaPath = "../../schemas/vat-manifest-v1.schema.json"

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(content, &schema); err != nil {
		t.Fatalf("the published schema is not valid JSON: %v", err)
	}
	return schema
}

// node walks a path of object keys, failing rather than panicking so a renamed
// section reports where it was expected instead of a nil dereference.
func node(t *testing.T, schema map[string]any, path ...string) map[string]any {
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

func schemaProperties(t *testing.T, properties map[string]any) []string {
	t.Helper()
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestThePublishedSchemaDescribesEveryFieldTheManifestHas is the guard that
// matters: a field added to the Go struct and forgotten in the schema makes
// every tool validating against the schema reject a manifest vat itself wrote.
func TestThePublishedSchemaDescribesEveryFieldTheManifestHas(t *testing.T) {
	// Arrange
	schema := loadSchema(t)
	cases := []struct {
		what   string
		sample any
		path   []string
	}{
		{"the manifest", manifest.Manifest{}, []string{"properties"}},
		{"workspace", manifest.Workspace{}, []string{"properties", "workspace", "properties"}},
		{"policy", manifest.Policy{}, []string{"properties", "policy", "properties"}},
		{"policy.sync", manifest.SyncPolicy{}, []string{"properties", "policy", "properties", "sync", "properties"}},
		{"policy.trust", manifest.TrustPolicy{}, []string{"properties", "policy", "properties", "trust", "properties"}},
		{"policy.brain", manifest.BrainPolicy{}, []string{"properties", "policy", "properties", "brain", "properties"}},
		{"policy.changeset", manifest.ChangesetPolicy{}, []string{"properties", "policy", "properties", "changeset", "properties"}},
		{"policy.gates", manifest.GatePolicy{}, []string{"properties", "policy", "properties", "gates", "properties"}},
		{"a repository", manifest.Repo{}, []string{"$defs", "repo", "properties"}},
	}

	// Act & Assert
	for _, testCase := range cases {
		described := schemaProperties(t, node(t, schema, testCase.path...))
		serialised := yamlFields(t, testCase.sample)
		if !reflect.DeepEqual(described, serialised) {
			t.Errorf("%s: the schema describes %v, the type serialises %v",
				testCase.what, described, serialised)
		}
	}
}

// An enum is a copy of a list that lives in Go. Copies drift, and a role vat
// accepts but the schema rejects makes the schema report a working manifest as
// invalid — which is worse than having published no schema at all.
func TestThePublishedSchemaEnumsMatchTheValuesVatAccepts(t *testing.T) {
	// Arrange
	schema := loadSchema(t)
	enumOf := func(path ...string) []string {
		raw, ok := node(t, schema, path...)["enum"].([]any)
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

	roles := make([]string, 0)
	for _, role := range manifest.Roles() {
		roles = append(roles, string(role))
	}
	sort.Strings(roles)
	gates := []string{manifest.GateAuto, manifest.GateManual}
	sort.Strings(gates)

	// Act & Assert
	if got := enumOf("$defs", "role"); !reflect.DeepEqual(got, roles) {
		t.Errorf("the schema allows roles %v, vat accepts %v", got, roles)
	}
	if got := enumOf("$defs", "gate"); !reflect.DeepEqual(got, gates) {
		t.Errorf("the schema allows gates %v, vat accepts %v", got, gates)
	}
}

// The version bound is the one number that decides whether another
// implementation reading this schema will accept a file vat wrote.
func TestThePublishedSchemaPinsTheVersionVatWrites(t *testing.T) {
	// Arrange
	schema := loadSchema(t)
	version := node(t, schema, "properties", "version")

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

// The name cap is not decoration: it is the longest name a git host accepts,
// and without it `repo new` left the failure to the filesystem.
func TestThePublishedSchemaCapsRepositoryNamesWhereVatDoes(t *testing.T) {
	// Arrange
	schema := loadSchema(t)
	name := node(t, schema, "$defs", "repo", "properties", "name")

	// Act
	maxLength, ok := name["maxLength"].(float64)

	// Assert
	if !ok {
		t.Fatal("the schema puts no cap on a repository name")
	}
	longest := strings.Repeat("a", int(maxLength))
	if err := manifest.ValidateRepoName(longest); err != nil {
		t.Errorf("the schema allows a %d-character name that vat refuses: %v", int(maxLength), err)
	}
	if err := manifest.ValidateRepoName(longest + "a"); err == nil {
		t.Errorf("vat accepts a %d-character name that the schema refuses", int(maxLength)+1)
	}
}

// The modeline is a promise made in every manifest vat writes: that a schema
// exists at that URL and describes this file. Three things have to agree for it
// to hold — the constant, the file in this repository, and the $id inside it —
// and nothing else would notice any one of them moving. An editor would simply
// stop validating, quietly, which is the failure mode a modeline exists to
// prevent in the first place.
func TestASavedManifestPointsAtTheSchemaThisRepositoryPublishes(t *testing.T) {
	// Arrange
	schema := loadSchema(t)
	saved, err := manifest.Marshal(manifest.Manifest{
		Version:   manifest.SchemaVersion,
		Workspace: manifest.Workspace{Name: "acme"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Act
	modeline := "# yaml-language-server: $schema=" + manifest.SchemaURL

	// Assert
	if !strings.Contains(string(saved), modeline) {
		t.Errorf("a saved manifest does not carry the schema modeline:\n%s", saved)
	}
	// The published file is what the URL resolves to, so its name has to be the
	// tail of the URL. A schema renamed and a URL left behind is a 404 in
	// everybody's editor and a green test suite here.
	name := schemaPath[strings.LastIndex(schemaPath, "/")+1:]
	if !strings.HasSuffix(manifest.SchemaURL, "/"+name) {
		t.Errorf("SchemaURL ends %q, but the schema in this repository is named %q",
			manifest.SchemaURL, name)
	}
	if id, _ := schema["$id"].(string); id != manifest.SchemaURL {
		t.Errorf("the schema calls itself %q, manifests point at %q", id, manifest.SchemaURL)
	}
}
