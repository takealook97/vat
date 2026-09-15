package cli

import (
	"strings"
	"testing"
)

// vat is installed several ways and each one owns the binary differently. A
// tool that overwrites itself under a package manager's prefix leaves that
// manager describing a file it no longer wrote, so the upgrade has to be
// performed by whoever installed it — which means recognising who that was.
func TestTheUpgradeCommandFollowsHowTheBinaryWasInstalled(t *testing.T) {
	for _, testCase := range []struct {
		name string
		path string
		want string
	}{
		{"homebrew cellar", "/opt/homebrew/Cellar/vat/0.6.2/bin/vat", "brew"},
		{"homebrew intel prefix", "/usr/local/Cellar/vat/0.6.2/bin/vat", "brew"},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/Cellar/vat/0.6.2/bin/vat", "brew"},
		{"go install", "/Users/someone/go/bin/vat", "go"},
		{"unpacked archive", "/Users/someone/Downloads/vat_darwin_arm64/vat", ""},
		{"system path", "/usr/bin/vat", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// Act
			name, args, ok := upgradeCommandFor(testCase.path)

			// Assert
			if testCase.want == "" {
				if ok {
					t.Errorf("an install vat cannot account for was answered with %q %v", name, args)
				}
				return
			}
			if !ok {
				t.Fatalf("%s was not recognised", testCase.path)
			}
			if name != testCase.want {
				t.Errorf("command = %q, want %q", name, testCase.want)
			}
			if len(args) == 0 {
				t.Error("the command carries no arguments, so it would upgrade nothing")
			}
		})
	}
}

// `brew upgrade vat` resolves against the tap only while it is tapped. Naming
// it in full costs nothing and removes the one way this upgrades a different
// formula that happens to share the name.
func TestTheBrewUpgradeNamesTheTapInFull(t *testing.T) {
	// Act
	_, args, ok := upgradeCommandFor("/opt/homebrew/Cellar/vat/0.6.2/bin/vat")

	// Assert
	if !ok {
		t.Fatal("a Homebrew install was not recognised")
	}
	if !strings.Contains(strings.Join(args, " "), "takealook97/tap/vat") {
		t.Errorf("args = %v; they do not name the tap the formula comes from", args)
	}
}
