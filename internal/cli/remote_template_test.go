package cli

import "testing"

// expandRemoteTemplate builds the origin of every repository `vat repo new`
// creates. It had no test, which for the one function that assembles a URL from
// user-supplied parts is the wrong place to have none.
func TestExpandRemoteTemplateSubstitutesEveryOccurrenceAndNothingElse(t *testing.T) {
	// Arrange & Act & Assert
	cases := []struct {
		name     string
		template string
		repo     string
		want     string
	}{
		{"no template is not a URL", "", "payments", ""},
		{"the usual case", "git@github.com:acme/{name}.git", "payments", "git@github.com:acme/payments.git"},
		{"a name appearing twice", "https://h/{name}/{name}.git", "a", "https://h/a/a.git"},
		{"a template with no placeholder is left alone", "https://h/fixed.git", "payments", "https://h/fixed.git"},
		{"an empty name does not corrupt the rest", "https://h/acme/{name}.git", "", "https://h/acme/.git"},
	}

	for _, testCase := range cases {
		if got := expandRemoteTemplate(testCase.template, testCase.repo); got != testCase.want {
			t.Errorf("%s: expandRemoteTemplate(%q, %q) = %q, want %q",
				testCase.name, testCase.template, testCase.repo, got, testCase.want)
		}
	}
}
