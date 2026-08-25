# Changelog

Notable changes to `vat`. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
- JSON output on every reporting command, and exit codes that distinguish
  "found problems" from "called wrong".
