module github.com/takealook97/vat

go 1.25

require gopkg.in/yaml.v3 v3.0.1

// A published version cannot be withdrawn from proxy.golang.org — its content
// is frozen on first fetch and there is no deletion. Retracting is the only
// thing that keeps `go get` and `go list -m -versions` from offering these.
retract (
	// Loses a manifest write with no report, and writes outside the workspace.
	// Two commands running at once each read vat.yaml and each wrote it back
	// whole, so one repository silently vanished from the manifest. A directory
	// inside the workspace that is a symlink out of it satisfied every check,
	// so `repo add` and `lint --fix` rendered AGENTS.md into a repository vat
	// does not govern. `lint --only <typo>` matched no rule, checked nothing,
	// and exited 0 — the flag this project's own guide puts in CI.
	[v0.1.6, v0.1.7]
	// Discloses a credential: `repo new --remote https://user:token@host/x.git`
	// wrote the URL into .git/config, pushed to it, and printed it before the
	// manifest refused.
	v0.1.5
	// Writes outside the workspace root. `repo new`, `harness role new`,
	// `brain init`, `brain new --id`, and `evidence new` each accepted a
	// traversing name and created files beyond the directory vat governs.
	[v0.1.2, v0.1.4]
	// Frozen before the build stamps were right: reports its own version as
	// "dev", and v0.1.1's tag no longer exists on GitHub at all.
	[v0.1.0, v0.1.1]
)
