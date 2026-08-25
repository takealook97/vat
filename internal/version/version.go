// Package version exposes build metadata stamped in at link time.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// These are overridden with -ldflags at release time.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Short returns the bare semantic version.
func Short() string { return Version }

// Long returns a human-readable one-line build identity.
func Long() string {
	commit := Commit
	if commit == "" {
		commit = vcsRevision()
	}
	if commit == "" {
		commit = "unknown"
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	date := Date
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("vat %s (commit %s, built %s, %s/%s, %s)",
		Version, commit, date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}
