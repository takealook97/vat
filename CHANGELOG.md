# Changelog

Notable changes to `vat`. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `vat ship`, which judges whether a changeset's verified revisions have
  actually landed on the branch each repository ships from. Verifying proves the
  combination works; it says nothing about whether those revisions ever reached
  anybody else, and a workspace that only asks the first question accumulates
  changesets closed on work still sitting on a branch. The gate is
  `merge-base --is-ancestor` and deliberately nothing more: a pull request is a
  forge's own idea, gating on one would buy a dependency and tie the record to a
  vendor, and an open pull request is precisely the state of not having landed.
  A review URL is recorded beside the revision as evidence and is never the
  gate. `vat changeset close` now refuses a changeset that has not landed, and
  `changeset/closed-unlanded` reports the gap when `--force` waives it, so a
  waiver stays visible instead of becoming indistinguishable from a change that
  shipped.
- Skill adapters. `.agents/skills/<name>/SKILL.md` is canonical and
  `vat harness render` generates the per-runtime pointer, the same way it
  already did for roles. `ClaudeSkillDir` and `SkillsDir` had been declared
  since the first release and used by nothing: the layer was designed and never
  wired up, so a workspace keeping skills had to maintain every copy by hand —
  which is the drift the harness exists to prevent. `harness/skill-metadata`
  reports a skill no runtime can advertise.
- `models:` on a role, mapping each runtime to the model it should use, and
  `harness/model-ambiguous` for a role that names one model for several. A model
  name lives in a vendor's namespace, and vat was writing `model = "opus"` into
  Codex TOML files — a name Codex cannot resolve — from the very tool whose job
  is to stop one role behaving differently per runtime. A bare `model` is now
  honoured only when the role targets a single runtime, because writing a name a
  runtime has never heard of is worse than writing nothing.
- A schema version in the brain marker, and `brain/schema-newer`. A knowledge
  layer whose whole claim is that it outlives the tool that wrote it will
  eventually be handed to an older tool; reading it quietly and reporting it
  clean is the worst available outcome, because the records look sound while
  half of what governs them is invisible. `docs/BRAIN.md` now documents the
  on-disk contract another tool may rely on.
- A `recovery` section in `vat doctor`, answering the one question a backup
  exists to answer: if this machine stopped working now, what would be gone. For
  a git repository that has a precise form — does its history exist anywhere but
  here — and unpushed commits and stashes are the two states where the answer is
  no. `vat` takes no backups and will not: where an archive goes and who holds
  the key are organisational facts it owns none of, and writing outside the
  workspace root is the boundary everything else rests on.
- `brain/unreferenced`, for a scaffolded brain the manifest never adopted.
  `brain/not-initialised` fires only for a repository already declared as the
  brain, so a complete brain layout that no `vat brain` command could reach was
  reported by nothing at all.
- A test that renders vat's own role adapters and compares them with what is
  committed. vat generates the contracts it asks every other workspace to
  generate, and until now nothing checked that its own were in step — which is
  exactly how the Codex model name survived to a release.

- `brain.RuleNames()`, `vat brain check --list`, and `--only`, with the rule
  table in BRAIN.md. `vat brain check` reported twenty-four rules and two of
  them were named in any document; the other twenty-two had gone undocumented
  since the first release, because nothing held the list and nothing compared it
  with what the code reports. Two tests now do: one reads the package's own
  source and fails on a rule the list omits, the other fails when the table and
  the list disagree. This is the guarantee `vat lint` already had, and the
  reason AGENTS.md gives for it applied here the whole time.
- `vat brain quarantine`, `revoke`, and `resolve`. These three states carried
  check rules and review-queue weights from the first release and had no
  command, so reaching one meant hand-editing the YAML of the record whose
  trustworthiness was already in doubt. A quarantine or a revocation must state
  a reason; `resolved` is refused for anything that is not a gap; and an end
  state is never reopened, by these commands or by `promote`.
- `vat brain archive`, which moves superseded, revoked, and resolved records
  into `archive/`. `history/` and `archive/` were created by `init` and written
  to by nothing, so terminal records stayed in the working directories forever.
  Two things depended on that separation: an entry point cannot be a fixed-size
  place to start while it lists every record ever written, and an external search
  index cannot cheaply exclude withdrawn claims that sit in the same directory as
  the current ones. Nothing is deleted, an archived record is still loaded so its
  supersession chain is still checked from both ends, and the relative links
  inside a moved record are repointed.
- `brain/record-malformed`, reported for a file that cannot be read as a record.
  Previously one unparseable header — a merge conflict marker is the common
  case — aborted the whole load and took `check`, `query`, `sweep`, `build`,
  `doctor`, and `lint` down together, so the layer said nothing at all about the
  records that were fine.
- `brain/record-secret-suspected`, reported for a record that appears to carry a
  credential. "A record holds no secret" was the one rule in this layer with
  nothing checking it, which by this project's first rule makes it not a rule.
  The finding names the line and the kind of credential and never the value.
- `vat brain promote --reverified`, and the refusal that makes it necessary.

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

- `make cover` gates each package as well as the total. The total alone was
  hiding what it was meant to expose: at 80.5% overall, the three packages
  holding nearly all the logic were each below the stated line, floated there by
  small pure packages scoring in the nineties. An average over unequal packages
  is not a floor. The per-package minimum is set below where every package
  stands today, because it is a ratchet against sliding rather than a target to
  grind toward.

- `vat brain promote` no longer moves a claim's observation date forward on
  request alone. When the owning repository is still at the revision the claim
  was pinned to, nothing about the evidence has changed and the date moves
  freely; when it has moved, or vat cannot read the repository, the date only
  moves with `--reverified`, which re-pins `source_ref` to the revision the
  reviewer actually read. Stamping "observed today" without re-reading anything
  turned a four-hundred-day-old sentence into a verified one with a single
  keystroke. `promote` also refuses to revive a terminal record, and requires
  `--reviewer` when `policy.gates.brain_promote` is `manual`.
- `vat brain supersede` leaves the replacement `provisional` when
  `policy.brain.require_promotion_gate` is set. Promoting it on the way past was
  the one path by which a record became canonical without anyone reviewing it —
  the gate the policy declared, unenforced.
- `CURRENT.md` lists at most fifteen records per section and then says how many
  remain and where they are. It was documented as a fixed-size entry point and
  grew a row per record forever; the fifteen kept are the ones most cited, the
  same measure the review queue uses to decide what costs most to ignore.
  Truncating by identifier would have kept whatever was written first.
- `vat brain query` discounts document length when ranking. Counting raw
  occurrences is arithmetic rather than relevance: a long record repeating one
  query word out-scored a short record that answered all three, and the long
  record is usually the sprawling one nobody has split up yet.
- `vat brain new memory` opens with the headings a reusable observation needs —
  trigger, lesson, evidence, scope, reuse condition — instead of a blank prompt
  that invited a session summary. They are a convention, not a schema: a field
  becomes checked here only once there is a rule worth enforcing on it.
- `vat doctor` and `vat metrics` count the working set rather than every record
  that has ever existed, and `doctor` reports records it could not read.
- The containment check both the commands and the new lint rule depend on moved
  to `workspace.Contains`, so an entry-point guard and an audit cannot disagree
  about what "inside the workspace" means.

### Removed

- `workspace.OpenCurrent` and `brain.MemoryMonths`, which nothing called. These
  are `internal` packages, so no consumer outside this module can exist and an
  uncalled exported function has no possible user.

### Fixed

- `vat brain init <directory>` advertised `vat brain new` after scaffolding, and
  that command then answered "this workspace has no brain repository". Every
  other `vat brain` command resolves the brain through the manifest, and `init`
  does not put it there, so the tool created a state its own commands could not
  reach and named the one command guaranteed to fail. The hint now says what
  will actually work, and `brain/unreferenced` reports the state itself.

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
