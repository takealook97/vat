package manifest

// The direction of a failed requirement decides which remedy is printed, and
// the two remedies are opposite. These are in-package because the decision is
// not exported: only the hint it produces is, and a table that reached it
// through the hint would be testing the wording rather than the judgement.

import "testing"

// Every operator, at and around the boundary, in both directions.
func TestExcludesAboveAcrossEveryOperator(t *testing.T) {
	cases := []struct {
		constraint string
		running    string
		wantAbove  bool
		why        string
	}{
		{">=0.4.0 <0.5.0", "0.5.0", true, "the boundary case that motivated this"},
		{">=0.4.0 <0.5.0", "0.9.9", true, "well past the upper bound"},
		{">=0.4.0 <0.5.0", "0.3.0", false, "below the lower bound"},
		{">=9.0.0", "0.5.0", false, "only a lower bound, unmet"},
		{"<0.1.0", "0.5.0", true, "only an upper bound, passed"},
		{"<=0.5.0", "0.5.1", true, "inclusive upper bound, just past"},
		{">0.5.0", "0.5.0", false, "exclusive lower bound, exactly at it"},
		{"=0.4.0", "0.5.0", true, "equality failed from above"},
		{"=0.6.0", "0.5.0", false, "equality failed from below"},
		{"==0.4.0", "0.5.0", true, "== normalises to = and still reads from above"},
		{">=9.0.0 <0.1.0", "0.5.0", false, "unsatisfiable; the lower bound is the reachable end"},
		{">=0.4.0 <0.9.0", "0.5.0", false, "satisfied, so nothing failed"},
		{"", "0.5.0", false, "an empty constraint fails nothing"},
	}
	for _, c := range cases {
		constraint, err := ParseConstraint(c.constraint)
		if err != nil {
			t.Fatalf("ParseConstraint(%q): %v", c.constraint, err)
		}
		version, err := parseSemver(c.running)
		if err != nil {
			t.Fatalf("parseSemver(%q): %v", c.running, err)
		}
		if got := constraint.excludesAbove(version); got != c.wantAbove {
			t.Errorf("excludesAbove(%q, %q) = %v, want %v — %s",
				c.constraint, c.running, got, c.wantAbove, c.why)
		}
	}
}

// An unreadable running version must not be reported as too new; the check
// already declines to judge it at all.
func TestAnUnreadableRunningVersionIsNotCalledTooNew(t *testing.T) {
	m := Manifest{Requires: Requires{Vat: ">=0.4.0 <0.5.0"}}
	if err := CheckToolVersion(m, "dev"); err != nil {
		t.Errorf("CheckToolVersion with an unreadable version = %v, want nil", err)
	}
}
