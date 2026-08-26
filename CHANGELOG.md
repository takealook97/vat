# Changelog

Notable changes to `vat`. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `brain/record-malformed`, reported for a file that cannot be read as a record.
  Previously one unparseable header — a merge conflict marker is the common
  case — aborted the whole load and took `check`, `query`, `sweep`, `build`,
  `doctor`, and `lint` down together, so the layer said nothing at all about the
  records that were fine.
- Two lint rules that audit the state a workspace is already in, rather than the
  invocation that creates it. `repo/credential-in-remote` reports a token left in
  a clone's `.git/config` — the state v0.1.5 produced, and one the remote
  comparison cannot see because it strips userinfo before comparing, so a
  token-bearing remote matched the plain manifest origin exactly and nothing
  reported it. `repo/outside-workspace` reports a governed directory that
  resolves, through a symlink, outside the root — the class v0.1.2 through
  v0.1.4 produced, previously diagnosed as "exists but holds no git repository".
  Neither is repairable: `vat` does not rewrite a remote, and where a repository
  lives is a decision.
- `make cover` fails below `COVERAGE_MIN` (80) instead of only printing the
  figure, and CI runs that target so the threshold lives in one place.

### Changed

- The containment check both the commands and the new lint rule depend on moved
  to `workspace.Contains`, so an entry-point guard and an audit cannot disagree
  about what "inside the workspace" means.

### Removed

- `workspace.OpenCurrent` and `brain.MemoryMonths`, which nothing called. These
  are `internal` packages, so no consumer outside this module can exist and an
  uncalled exported function has no possible user.

### Fixed

- A lifecycle transition no longer deletes the parts of a record's header that
  vat's own schema does not model. `sweep --apply` re-rendered the typed struct,
  so a workspace's own field and any comment explaining a value disappeared the
  day a claim aged out — an unattended command, no error, nothing in a diff
  anyone reads. Rewrites now merge into the header that was there, keeping
  unknown keys in place with their comments, while a field the schema does model
  and clears is still removed.
- Ten doc comments were separated from the functions they document by a blank
  line, which hides them from `go doc` and from an editor's hover entirely. The
  reasoning a comment records is the only thing that earns it a place, and this
  made it invisible. A test now walks every Go file in the repository and fails
  on any comment that opens with a function's own name but is not attached to
  it — neither `gofmt` nor any linter this project runs reports the pattern, so
  it had recurred three times.

## [0.1.0] - 2026-08-25

The first release.

### Added

- `vat init`, `status`, `sync`, `doctor`, `exec` — the workspace control plane.
  `sync` implements a fail-closed state machine that never discards local work:
  dirty trees, feature branches, and local-ahead branches are reported and left
  alone, and diverged history and remote mismatches fail the run.
- `vat lint` — twenty-one rules that enforce mechanically what a methodology
  document can only state, including manifest-to-`.gitignore` drift, generated
  contract drift, runtime adapter drift, the root contract size budget, and
  knowledge-claim provenance. `--fix` regenerates what is generated.
- `vat repo add|new|adopt|remove|archive|unarchive|rename|list` — repository
  lifecycle. Every mutation moves the manifest, the `.gitignore` exclusion, and
  the generated harness together. Removal refuses to proceed while uncommitted
  changes, unpushed commits, or stashes exist.
- `vat harness render|check|roles|role new` — one runtime-neutral role body per
  role, with generated adapters for Claude Code and Codex, and drift detection
  between them.
- `vat brain` — the reviewed-knowledge layer. Atomic records with provenance, a
  six-state lifecycle, automatic demotion of aged claims, a review queue ordered
  by citation count and age, a promotion gate that refuses claims without
  evidence, bidirectional supersession, deterministic projections, and a bounded
  search surface.
- `vat changeset` — multi-repository completion records: the revision bundle,
  the canonical checks that passed on each revision, the integration acceptance
  required to close, and a generated return plan in reverse enrolment order.
- `vat evidence` — the contract a worker is given before it starts, with release
  authority withheld unless explicitly granted.
- `vat metrics` — measurement of whether the discipline is holding, with a local
  ledger for trends.
- `vat fit` — a per-layer break-even advisor that recommends adopting nothing
  until the problem each layer solves is real.
- `vat completion` for bash, zsh, and fish.
- A timed-out command has its whole process group signalled, so a hanging check
  cannot leave children running after the job is reported as failed.
- `vat doctor` reports key material other users on the machine can read,
  ignoring ciphertext because that is what encryption is for.
- `vat exec` executes your command directly rather than re-parsing it through a
  shell, so quoting survives; manifest checks, which are shell fragments by
  contract, still run through one.
- 22 lint rules, including `repo/remote-missing` for a repository that can never
  be fetched or pushed.
- JSON output on every reporting command, with lists always rendered as arrays
  and field names matching the manifest, and exit codes that distinguish
  "found problems" from "called wrong".
- Installation through Homebrew (`brew install takealook97/tap/vat`), `go
  install`, or a release archive that unpacks to a runnable binary with the
  licence and shell completions beside it.
