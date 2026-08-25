// Package version exposes build metadata stamped in at link time.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// These are overridden with -ldflags at release time. A binary produced by
// `go install` has none of them, so every accessor falls back to what the Go
// toolchain records in the build info.
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

// Short returns the semantic version, preferring the linker stamp and falling
// back to the module version `go install` records.
func Short() string {
	if Version != "" {
		return Version
	}
	if module := moduleVersion(); module != "" {
		return module
	}
	return "dev"
}

// Revision returns the commit the binary was built from.
func Revision() string {
	if Commit != "" {
		return Commit
	}
	return buildSetting("vcs.revision")
}

// BuildDate returns when the binary was built.
func BuildDate() string {
	if Date != "" {
		return Date
	}
	return buildSetting("vcs.time")
}

// Long returns a human-readable one-line build identity.
func Long() string {
	commit := Revision()
	if commit == "" {
		commit = "unknown"
	}
	if len(commit) > 12 {
		commit = commit[:12]
	}
	date := BuildDate()
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("vat %s (commit %s, built %s, %s/%s, %s)",
		Short(), commit, date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// moduleVersion returns the version the module proxy served, which is how a
// `go install module@v1.2.3` binary knows what it is.
func moduleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	// "(devel)" is what a local build reports; it is less informative than
	// saying "dev" plainly.
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	return info.Main.Version
}

func buildSetting(key string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}
