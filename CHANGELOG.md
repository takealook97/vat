# Changelog

Notable changes to `vat`. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `vat harness adopt`, which moves a runtime's hand-written agent files into
  `.agents/` and generates the adapters from them. Nobody starts empty: anybody
  who would benefit from a harness that keeps one body per definition already
  has agent files, written by hand into one runtime's directory, and copying
  them across is where adoption stopped. It reports first and writes nothing
  until `--apply`. A file vat generated is skipped, and so is one whose
  canonical definition already exists — that is drift, and the canonical copy is
  the one file this tool will not overwrite. An adopted role records the runtime
  it came from and no other, because a bare `model` is honoured only by a role
  targeting one runtime, and it grants no write access, because adoption must
  not hand a definition a capability it never stated.
- `harness/adapter-orphaned`, for a generated adapter left behind by a deleted
  definition. Drift compares an adapter with the definition it came from and
  cannot see the case where there is none left to compare against: delete
  `.agents/roles/planner.md` and the Claude and Codex adapters stay, the runtime
  keeps advertising the role, and the session that opens it is told to read a
  file that is gone — while `vat harness check` reported that every contract
  matched. The rule reads the generated marker rather than the directory, so an
  agent file somebody wrote by hand is left alone. It is not fixable: the repair
  is a deletion, and the adapter may be the only remaining copy of a definition
  removed by accident.
- `vat harness skills` and `vat harness skill new`, the counterparts roles have
  had since skills existed. A format vat specifies, generates, and lints four
  ways had no command that produced one, so the only way to add a skill was to
  know the on-disk layout and write the directory by hand. `skill new` says at
  creation what lint would say on the next run: that `--runtimes codex` selects
  an adapter which does not exist, and that a skill with no description is one
  no runtime can offer. The description is left empty rather than filled with a
  placeholder, because an invented one would satisfy `harness/skill-metadata`
  while telling the runtime nothing.
- `vat init` seeds two procedures, `before-cross-repo-work` and
  `consult-the-brain-first`. A workspace arrived with contracts and no
  procedures: the generated `AGENTS.md` states boundaries, and when to open a
  changeset or consult the knowledge layer is a sequence rather than a boundary.
  Both seeds describe vat's own command sequences and nothing else, because a
  procedure vat writes once and never maintains is the second source of truth
  this tool exists to remove — and a test holds every command they name to
  resolving in the command tree, matched whole rather than by prefix. A seeded
  file is canonical from the moment it lands: an existing one is never
  rewritten, and removing one is without consequence. Both are specified in
  `docs/SPEC.md` and both are tested.
- vat's own first three skills, under `.agents/skills/`. The tool generated
  skill adapters, linted them four ways, and specified them normatively while
  keeping none of its own, which is a poor argument from a tool whose whole
  claim is that a contract nobody uses drifts. `add-a-lint-rule` and
  `change-a-command-contract` were conditional prose in `AGENTS.md`;
  `cut-a-release` was written down nowhere and carries the fact that costs most
  to rediscover — a published tag is frozen, because the Go module proxy caches
  a version on first fetch, so a bad release ships as the next patch rather than
  as a moved tag.
- `harness/runtime-unknown`, for a `runtimes:` value no adapter is generated
  for. A typo produced silence: no adapter meant no drift, the description was
  present so role-metadata passed, and a bare model bound to nothing so
  model-ambiguous passed. The definition sat on disk, inert, while every
  diagnostic reported the harness healthy.
- `docs/SPEC.md`, the file formats stated normatively, so that reading a vat
  workspace does not require being vat. A knowledge layer whose whole claim is
  that it outlives the tool that wrote it has to be implementable by somebody
  else, and until now the contract lived in Go structs and in prose that
  described a program rather than specifying a format. Tests compare its
  enumerations — roles, record statuses, claim kinds, changeset statuses, schema
  versions — against the packages that own them, because a specification nobody
  checks is a description of a program that used to exist.
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

- `.claude/agents/<name>.md` carried no "generated" marker while the Codex
  adapter and the Claude skill adapter both did, which left the file people open
  most often as the only generated file that never said it was generated — and
  left it invisible to the rule above.
- The self-contract test kept vat's own **role** adapters in step with their
  definitions and did not do the same for skills. vat has no `vat.yaml` and runs
  no lint on itself, so that test is the only thing standing where every other
  workspace has `vat lint`, and a skill adapter could have drifted from its
  canonical procedure with the suite green. It now also makes the two checks
  `harness/skill-metadata` and `harness/runtime-unknown` would make elsewhere: a
  skill with no description, and one that renders no adapter at all.
- The three roles this repository defines and the three skills it now defines
  did not know about each other, so `rule-designer` decided whether a rule
  should exist while the steps for building one sat in a skill it never named.
  A role is who is running and a skill is how a job is done; the pairs that
  overlap now point at each other.
- `docs/ADOPTION.md` told a reader adopting vat to commit four files by name,
  which stopped being every file `vat init` writes the moment init began seeding
  procedures. Its stage 2 also defined roles and never mentioned skills, so the
  walkthrough for adopting the harness taught half of it.
- `docs/METHODOLOGY.md` listed `.agents/roles/` in the harness inventory and
  stopped, and the README's harness section did not contain the word "skill" at
  all. A format that is normative in `docs/SPEC.md` and absent from the two
  documents that introduce the model is a format nobody adopts.

- **`vat harness roles` reports the role files it could not read, and fails
  again.** Returning malformed definitions separately so the sound ones still
  render meant the command discarded them: a broken role file disappeared from
  the listing with exit 0, where the previous release failed loudly. A listing
  that omits a role in silence is how somebody concludes it was deleted. The
  `--json` payload stays the array every listing command emits — what did not
  load is on the error stream and in the exit code, and reported properly by
  `vat lint`. `vat harness role new` mentions them without failing on them.
- **A `SKILL.md` that cannot be stat'ed for any reason other than absence is
  recorded.** A permission denial or a symlink loop was stepped past as though
  the directory simply held no skill — the same "could not read, so said
  nothing" this batch was fixing everywhere else.
- **Whether a load stops is a property of the error, not of the loop that meets
  it.** `ErrRefused` marks the errors that must halt, so roles and skills cannot
  drift on the question, and a future error that should halt is handled in one
  place rather than remembered in two. Both loaders now share the classify-and-
  accumulate step.
- **`Malformed.Path` uses forward slashes** — matching what `internal/brain`
  records and what `docs/SPEC.md` specifies as a canonical format — **and
  `Problem` no longer repeats the path** it is printed beside, in absolute form,
  into issues and CI logs.

- **One unreadable role or skill no longer withdraws the adapters of every
  definition beside it.** The load aborted on the first bad file, so a typo in
  one skill left every other skill's adapter unwritten and reported only that
  first problem — a second typo stayed invisible until the first was fixed.
  Unreadable files are now reported by `harness/definition-malformed` and the
  rest render. A name that could *escape* the adapter directories remains a
  refusal rather than a finding: that is not a file vat failed to read, it is a
  file asking to be written somewhere it must not be.
- **A skill's name error says "skill".** It reported "invalid role name" for a
  file under `.agents/skills`, sending the reader to the wrong directory.

- **A changeset identifier is validated before it becomes a path.** `Load` took
  it from a command-line argument and `Save` took it from the `id:` field of a
  file on disk, and neither checked it: `vat changeset abandon` on a record
  declaring `id: ../../../escaped` wrote outside the workspace root. That is the
  defect class three releases were retracted for, still live. Both paths now
  refuse anything that is not `CS-NNNN`, and a file whose identifier disagrees
  with its own name is refused too — it would have been read as one changeset
  and written back over another.
- **A remote name reaches git as a value, not as an option.** `gitx.Fetch`
  passed it positionally with no `--`, so `vat ship --remote "--upload-pack=..."`
  made git run that program. `merge-base` got the same separator.
- **`vat ship` no longer erases landing evidence when it merely fails to look.**
  The previous answer was cleared before any observation was attempted, so a run
  with no network — or one where a repository was briefly not cloned — deleted
  the record of a change that really had shipped, and no later run could restore
  it. Only an observation that contradicts the record now clears it.
- **A missing tracking ref is reported as a missing ref.** `IsAncestor` returned
  a clean negative when the branch it compares against was absent, so
  `--remote upstream` in a clone that has only `origin` reported every
  repository as "verified but not landed" — a claim about the branch, when the
  truth was about the ref.
- **`changeset/closed-unlanded` keys on a recorded waiver.** Keying on absent
  landing evidence reported every changeset closed before landing was recorded
  at all: the entire history of every upgrading workspace, with a fix line
  naming `vat ship`, which refuses a closed changeset. A rule nobody can satisfy
  pointing at a command that will not run is the exact defect this batch fixed
  elsewhere.
- **`vat doctor` no longer reads a stash error as "no stashes".** Eleven lines
  after saying that answer must never be given by accident, it gave it. It also
  no longer vouches for repositories it never inspected.
- **Two roles or skills claiming one name are refused.** The name decides the
  adapter path, so a duplicate rendered one file from two definitions, never
  converged, and picked its winner unstably.
- **An unparseable brain schema is reported.** It returned the same value as no
  schema line at all and was read as a brain predating versioning — silence in
  the one case the field exists for.
- **`brain/unreferenced` looks deeper than the workspace root, and says so when
  it cannot look.** It scanned only immediate children while the command it
  guards writes wherever it is pointed, and a directory it could not read made
  the rule vanish from the report.
- **`vat ship` refuses to write after a cancelled run,** which had been filing
  the resulting instant failures as findings.

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

## [0.1.7] - 2026-08-26

### Retracted

Every version before this one. `proxy.golang.org` freezes a version's content on
first fetch and offers no deletion, so `go install` would keep serving code with
a disclosed credential or a workspace escape in it forever. Retraction is the
only thing that stops the toolchain offering them: `go get` refuses a retracted
version, `go list -m -versions` hides it, and `@latest` skips past it. A
retraction only takes effect once published in a version above the ones it
names, which is why it ships here rather than in the release that fixed each
defect.

Upgrade to 0.1.7 or later. The reason for each is recorded under it below.

## [0.1.6] - 2026-08-26

### Fixed

- `vat repo new --remote https://user:token@host/x.git` never checked the flag.
  It created the directory, scaffolded it, committed, wrote the
  credential-bearing URL into `.git/config`, pushed to it over the network, and
  printed it on success — and only then had the manifest refuse. The check now
  runs before the first directory is created, and the URL read back from
  `.git/config` is redacted before it is printed. `workspace.remote_template`
  was checked for `{name}` and nothing else, so a credential in it reached the
  manifest too.
- A credential in `--origin` exited 1, "found errors", when a credential typed
  on the command line means the command was called wrong, which is 2. CI
  branches on that.
- Every workspace's committed `.gitignore` carried generated text with three
  apostrophes in it, and a repository name had no length limit while role names
  and record ids both capped at 64 — `vat repo new` accepted a 200-character
  name and left the failure to the filesystem, which reports it as something
  else entirely. The cap is 100, matching what a git host accepts rather than a
  convention vat picked for its own artefacts.
- Ctrl-C during `vat exec` printed "exit status -1" per repository, Go's
  rendering of a process killed by a signal and a number that tells the reader
  nothing about what happened. An interrupted run now says it was interrupted.

## [0.1.5] - 2026-08-26

### Retracted

Discloses a credential. `vat repo new --remote https://user:token@host/x.git`
wrote the URL into `.git/config`, pushed to it, and printed it before the
manifest refused. Fixed in 0.1.6.

### Fixed

- Six findings from an external safety review, all real. `vat brain init
  ../../outside` joined a user-supplied path to the root with no containment
  check and scaffolded fourteen files outside the workspace. `repo add --origin`
  stored a token verbatim into the committed `vat.yaml`, and adopt recorded
  whatever the git remote held; the manifest now rejects an origin carrying
  userinfo, and adopt strips it — recording what it found rather than what
  somebody typed. Three paths bypassed redaction: the push failure in
  `repo new`, git's own stderr in sync, and `CommandError.Error`.
- A `remote_template` with no `{name}` is not a template: every repository
  created from it was given the same origin, and they fetch and push over each
  other. The manifest already refused two repositories resolving to one
  directory and said nothing about two resolving to one remote, which is worse.
- `vat status` printed "1 repositories", and suggested `vat sync` for a diverged
  repository, which sync refuses outright.

## [0.1.4] - 2026-08-26

### Retracted

Writes outside the workspace root, in the command 0.1.3 did not reach. Fixed in
0.1.5.

### Fixed

- A malformed identifier exited 1 rather than 2. The check lives in the packages
  that own the files, where no caller can bypass it, so it surfaced as a plain
  error when what it means is that the command was called wrong.

## [0.1.3] - 2026-08-26

### Retracted

Writes outside the workspace root. This release closed the name-to-path gaps in
`repo new`, `harness role new`, `brain new --id`, and `evidence new`, and left
`brain init` open; that one was not closed until 0.1.5.

### Fixed

- `vat harness role new ../../../pwned` wrote the role body outside the
  workspace and exited 0. The check that would have caught it,
  `harness.ValidRoleName`, already existed and was already exported for exactly
  this reason; the command that creates the file was the one caller that never
  asked. `vat repo new ../escaped` initialised a git repository outside the
  workspace, scaffolded it, committed it, and only then failed validation on
  save, leaving everything it had written behind.
- `vat brain new --id ../../../pwned` and `vat evidence new ../../../pwned` each
  wrote a file outside the workspace root and exited 0. The check now lives in
  `brain.ValidateID` and `evidence.ValidateID`, called from Create, Save, and
  Load rather than from the commands, so no caller can write a record to a path
  of its own choosing.
- `vat repo adopt` on a symlink pointing at a repository outside the workspace
  adopted it and wrote a generated contract into the target. Textual containment
  cannot see that; `strictlyBelow` already resolved both sides through symlinks,
  and adopt was the command that never called it.
- `vat evidence check EV-9999` printed nothing and exited 0 when no packet had
  that id, so a merge gated on a packet that was never written passed.

## [0.1.2] - 2026-08-26

### Retracted

Writes outside the workspace root, in five commands that turned an argument into
a path without checking it first. Fixed across 0.1.3 and 0.1.5.

### Fixed

- Raising coverage from 67% to 80% turned up four defects. `changeset list
  --open` applied the filter inside the table-rendering loop, so `--json`
  returned every changeset ever closed — a CI job asking for open work was
  handed the whole history with no way to tell. `changeset add` did not check
  the status, so a repository could be enrolled into a closed changeset,
  rewriting what had been verified together. `vat fit` had no json tags and
  alone emitted Go field names. And `--dirty` covers unpushed commits and
  stashes, not only uncommitted changes; its own help text said otherwise.

## [0.1.1]

### Retracted

Frozen before the build stamps were right, so it reports its own version
incorrectly. Its tag no longer exists on the forge, but the version had already
been published to the module proxy and cannot be withdrawn from it — which is
the whole reason retraction exists.

## [0.1.0] - 2026-08-25

The first release.

### Retracted

Frozen before the build stamps were right: it reports its own version as `dev`.

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
