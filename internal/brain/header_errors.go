package brain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlErrorLine matches the position yaml.v3 puts at the front of each
// message, with or without its "yaml: " prefix.
var yamlErrorLine = regexp.MustCompile(`^(?:yaml: )?line (\d+):`)

// headerProblem restates a front matter parse error without its text.
//
// yaml.v3 quotes what it could not read — "cannot unmarshal !!str `ghp_...`
// into bool", "mapping key "..." already defined" — and that text is author
// content, so a record holding a credential in the wrong place had the
// credential printed by the very finding that reported the record. Only the
// line numbers and the class of problem survive.
func headerProblem(err error) error {
	var typed *yaml.TypeError
	var messages []string
	what := "is not valid YAML"
	if errors.As(err, &typed) {
		messages = typed.Errors
		what = "holds a value that does not fit its field"
		for _, message := range typed.Errors {
			if strings.Contains(message, "already defined") {
				what = "gives a key more than once, or a value that does not fit its field"
				break
			}
		}
	} else {
		messages = []string{strings.TrimPrefix(err.Error(), "parse front matter: ")}
	}
	lines := headerErrorLines(messages)
	if len(lines) == 0 {
		return fmt.Errorf("parse front matter: the header %s", what)
	}
	label := "line"
	if len(lines) > 1 {
		label = "lines"
	}
	return fmt.Errorf("parse front matter: the header %s (%s %s)", what, label, strings.Join(lines, ", "))
}

// headerErrorLines returns the record file lines named in yaml.v3 messages,
// which count from the first line of the header.
func headerErrorLines(messages []string) []string {
	seen := map[int]bool{}
	var numbers []int
	for _, message := range messages {
		match := yamlErrorLine.FindStringSubmatch(strings.TrimSpace(message))
		if match == nil {
			continue
		}
		number, err := strconv.Atoi(match[1])
		if err != nil || seen[number] {
			continue
		}
		seen[number] = true
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	lines := make([]string, 0, len(numbers))
	for _, number := range numbers {
		lines = append(lines, strconv.Itoa(number+1))
	}
	return lines
}
