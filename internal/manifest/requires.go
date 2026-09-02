package manifest

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Requires is what a workspace demands of the tool operating it.
//
// The schema version says which file format vat is reading. It says nothing
// about which commands exist, what they print, or what they refuse to write,
// and those are what a workspace built on vat actually depends on: a
// repository whose adoption relies on a projection being left alone cannot
// express that with `version: 1`, because the vat that overwrote it read the
// same `version: 1`.
type Requires struct {
	// Vat is a constraint on the version of vat operating this workspace,
	// written as space-separated terms that must all hold: ">=0.3.0 <0.4.0".
	Vat string `yaml:"vat,omitempty" json:"vat,omitempty"`
}

// Constraint is a parsed version requirement.
type Constraint struct {
	terms []term
	text  string
}

// String returns the constraint as it was written.
func (c Constraint) String() string { return c.text }

// Empty reports whether the constraint demands nothing.
func (c Constraint) Empty() bool { return len(c.terms) == 0 }

type term struct {
	operator string
	version  semver
}

// operators are listed longest first so that ">=" is never read as ">".
var operators = []string{">=", "<=", "==", "=", ">", "<"}

// ParseConstraint reads a version requirement, or reports why it is not one.
//
// The grammar is deliberately small: comparison terms, all of which must hold.
// Caret and tilde ranges are npm's and mean subtly different things in every
// ecosystem that borrowed them; accepting the text while ignoring the meaning
// is how a workspace ends up believing it pinned something.
func ParseConstraint(text string) (Constraint, error) {
	constraint := Constraint{text: strings.TrimSpace(text)}
	if constraint.text == "" {
		return constraint, nil
	}
	for _, field := range strings.Fields(constraint.text) {
		parsed, err := parseTerm(field)
		if err != nil {
			return Constraint{}, err
		}
		constraint.terms = append(constraint.terms, parsed)
	}
	return constraint, nil
}

func parseTerm(field string) (term, error) {
	for _, operator := range operators {
		rest, found := strings.CutPrefix(field, operator)
		if !found {
			continue
		}
		version, err := parseSemver(rest)
		if err != nil {
			return term{}, fmt.Errorf("%q: %w", field, err)
		}
		if operator == "==" {
			operator = "="
		}
		return term{operator: operator, version: version}, nil
	}
	return term{}, fmt.Errorf(
		"%q has no comparison; write one of >=, >, <=, <, = followed by a version, and separate terms with spaces",
		field)
}

// Allows reports whether a version satisfies every term.
func (c Constraint) Allows(version string) (bool, error) {
	if c.Empty() {
		return true, nil
	}
	parsed, err := parseSemver(version)
	if err != nil {
		return false, err
	}
	for _, t := range c.terms {
		if !t.holds(parsed) {
			return false, nil
		}
	}
	return true, nil
}

func (t term) holds(version semver) bool {
	comparison := version.compare(t.version)
	switch t.operator {
	case ">=":
		return comparison >= 0
	case ">":
		return comparison > 0
	case "<=":
		return comparison <= 0
	case "<":
		return comparison < 0
	case "=":
		return comparison == 0
	default:
		return false
	}
}

// ErrToolTooOld is returned when the running vat does not satisfy the
// workspace's requirement and a newer one could.
var ErrToolTooOld = errors.New("this workspace requires a different vat")

// ErrToolTooNew is returned when the running vat is past every bound the
// workspace failed, so no upgrade can satisfy it.
//
// The two are separated because the remedy is opposite and one of them was
// being given for both. Pinning `>=0.4.0 <0.5.0` is how `requires.vat` is meant
// to be used, so every workspace that pins a minor range meets this on the
// release that follows — and telling that person to upgrade names the one
// action that moves them further from the range.
var ErrToolTooNew = errors.New("this workspace requires a different vat")

// excludesAbove reports whether every term the version fails is an upper bound
// it has passed.
//
// A constraint can fail at both ends at once — `>=9.0.0 <0.1.0` is satisfiable
// by nothing — and there the lower bound is the reachable one, so it is not
// reported as too new.
func (c Constraint) excludesAbove(version semver) bool {
	failed := 0
	for _, t := range c.terms {
		if t.holds(version) {
			continue
		}
		failed++
		if !t.boundsAbove(version) {
			return false
		}
	}
	return failed > 0
}

// boundsAbove reports whether this failing term is one the version is past
// rather than short of.
func (t term) boundsAbove(version semver) bool {
	switch t.operator {
	case "<", "<=":
		return true
	case ">", ">=":
		return false
	default:
		// `=`, which fails in both directions; the comparison says which.
		return version.compare(t.version) > 0
	}
}

// CheckToolVersion reports whether the running vat may operate this workspace.
//
// A version it cannot read — "dev", which is what every `go build` binary calls
// itself — is not a violation. Refusing there would break every contributor's
// checkout to enforce a constraint that cannot be evaluated in either
// direction, and the workspace is no worse off than before the field existed.
func CheckToolVersion(m Manifest, running string) error {
	constraint, err := ParseConstraint(m.Requires.Vat)
	if err != nil || constraint.Empty() {
		// A malformed constraint is Validate's finding, reported against the
		// file for everyone rather than against one machine's version.
		return nil //nolint:nilerr // reported by Validate, at the file
	}
	allowed, err := constraint.Allows(running)
	if err != nil {
		return nil //nolint:nilerr // an unreadable running version judges nothing
	}
	if allowed {
		return nil
	}
	sentinel := ErrToolTooOld
	if parsed, err := parseSemver(running); err == nil && constraint.excludesAbove(parsed) {
		sentinel = ErrToolTooNew
	}
	return fmt.Errorf("%w: %s requires vat %s and this is %s",
		sentinel, FileName, constraint, running)
}

// describeSuffix matches what `git describe --tags --dirty` appends to the
// release it counts from: a commit count and hash, a dirty marker, or both.
var describeSuffix = regexp.MustCompile(`^(dirty|[0-9]+-g[0-9a-f]{7,40}(-dirty)?)$`)

// semver is the part of a version this comparison needs.
type semver struct {
	major, minor, patch int
	prerelease          string
}

func parseSemver(text string) (semver, error) {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "v")
	if trimmed == "" {
		return semver{}, errors.New("no version")
	}
	// Build metadata takes no part in the ordering, by the semver spec.
	if plus := strings.IndexByte(trimmed, '+'); plus >= 0 {
		trimmed = trimmed[:plus]
	}
	var prerelease string
	if dash := strings.IndexByte(trimmed, '-'); dash >= 0 {
		prerelease = trimmed[dash+1:]
		trimmed = trimmed[:dash]
		if describeSuffix.MatchString(prerelease) {
			// `git describe` names a build by the release *behind* it:
			// v0.3.0-4-g2ad652e is four commits after v0.3.0, not a candidate
			// for it. Reading it as a prerelease would sort it below 0.3.0 and
			// refuse the exact build somebody is running from main, which is
			// how this field would first be met.
			prerelease = ""
		}
	}
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("%q is not a version of the form 1.2.3", text)
	}
	numbers := [3]int{}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return semver{}, fmt.Errorf("%q is not a version of the form 1.2.3", text)
		}
		numbers[i] = value
	}
	return semver{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: prerelease}, nil
}

// compare returns -1, 0, or 1. A prerelease sorts before the release it leads
// to: 0.3.0-rc1 is not 0.3.0, and a floor written for the finished version must
// not be satisfied by a candidate for it.
func (s semver) compare(other semver) int {
	for _, pair := range [][2]int{
		{s.major, other.major}, {s.minor, other.minor}, {s.patch, other.patch},
	} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case s.prerelease == other.prerelease:
		return 0
	case s.prerelease == "":
		return 1
	case other.prerelease == "":
		return -1
	case s.prerelease < other.prerelease:
		return -1
	default:
		return 1
	}
}
