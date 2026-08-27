package brain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A rule the code reports and the list does not name cannot be selected with
// --only and does not appear in any document, so nobody knows to look for it.
// That is not hypothetical here: twenty-two of these rules had been reported
// since the first release and named nowhere, because nothing held the list and
// nothing compared it with what the code does.
//
// The source is read rather than the behaviour exercised, so a rule added in a
// path no test happens to reach is still caught the moment it is written.
func TestEveryRuleTheCodeReportsIsListedInRuleNames(t *testing.T) {
	// Arrange
	listed := map[string]bool{}
	for _, name := range RuleNames() {
		listed[name] = true
	}

	// Act
	reported := rulesInSource(t)

	// Assert
	if len(reported) == 0 {
		t.Fatal("no rules found in the source; this test stopped checking anything")
	}
	for _, rule := range reported {
		if !listed[rule] {
			t.Errorf("Check reports %q, which RuleNames does not list", rule)
		}
	}
	for _, rule := range RuleNames() {
		if !contains(reported, rule) {
			t.Errorf("RuleNames lists %q, which nothing in this package reports", rule)
		}
	}
	if !sort.StringsAreSorted(RuleNames()) {
		t.Error("RuleNames is not sorted, so a new rule has no obvious place to go")
	}
}

// rulesInSource collects every brain/* rule name the package's own files use as
// a Finding's Rule.
func rulesInSource(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	seen := map[string]bool{}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err == nil && strings.HasPrefix(value, "brain/") {
				seen[value] = true
			}
			return true
		})
	}
	names := make([]string, 0, len(seen))
	for rule := range seen {
		names = append(names, rule)
	}
	sort.Strings(names)
	return names
}

// A record identifier becomes a filename. `con` and `nul` are device names on
// Windows and `D-0001.` is stripped to `D-0001` there, so a knowledge layer
// carrying either is one only its author can check out — which is the opposite
// of what this layer is for.
func TestARecordIdentifierIsHeldToWhatEveryPlatformCanWrite(t *testing.T) {
	// Act & Assert
	for _, id := range []string{"con", "NUL", "aux.note", "D-0001.", "com1"} {
		if err := ValidateID(id); err == nil {
			t.Errorf("%q was accepted as a record identifier", id)
		}
	}
	for _, id := range []string{"D-0001", "G-0014", "console-notes", "com10"} {
		if err := ValidateID(id); err != nil {
			t.Errorf("%q is a usable identifier and was refused: %v", id, err)
		}
	}
}
