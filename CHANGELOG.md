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
  not hand a definition a capability it never stated. A header key the canonical
  format has no field for is reported rather than discarded in silence, before
  `--apply` as well as after: the runtime that file was written for may well
  honour it, and rewriting somebody's file while dropping half its header is the
  quiet kind of data loss.
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
- The generated workspace contract points at where procedures live. It named
  the boundary, the precedence order, the trust tiers, and the commands, and
  never said that procedures exist — so an agent knew what it may not do and
  nothing about how a job is done, which is the gap the skills half of the
  harness was built to close. A pointer, never the steps: the root file is
  always in context and has a byte budget, and a procedure copied into it is
  both a second copy to drift and a paragraph every session pays for whether or
  not the job ever comes up.
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

- `gitx.SetRemoteURL`. Nothing called it, and what it did is the one operation
  this tool promises never to perform: rewriting a remote turns a possible
  supply-chain problem into a silent redirection of every future fetch. A
  guarantee is worth more as a missing capability than as a discipline, and this
  one was sitting in the package one merge away from having a caller.
- `workspace.OpenCurrent` and `brain.MemoryMonths`, which nothing called. These
  are `internal` packages, so no consumer outside this module can exist and an
  uncalled exported function has no possible user.

### Fixed

- Brain record and evidence identifiers are held to the same rule, from the same
  definition. Both become filenames, so a knowledge layer carrying `con` or
  `D-0001.` is one only its author can check out — the opposite of what a layer
  whose whole claim is that it outlives the tool exists to be. The definition
  moved to `internal/fsx`, which every package that joins a name to a path
  already imports, because `internal/brain` deliberately imports neither
  `manifest` nor `gitx` and that seam outranks putting the rule where the first
  caller happened to be.
- Two role or skill definitions whose names differ only in case are refused at
  load. They are one file on macOS and on Windows, so a pair authored on Linux
  reaches a colleague's checkout as one silently overwriting the other, and the
  filesystem noticing is a coincidence of where the definition was typed rather
  than a rule anybody can rely on.
- A name that cannot become a directory on Windows is refused on every
  platform: the device names `con`, `prn`, `aux`, `nul`, `com0`–`com9`,
  `lpt0`–`lpt9` — with or without an extension, because Windows matches the
  device before the first dot — and any name ending in `.` or a space, which
  Windows strips silently so the directory is not the one the manifest names.
  Role and skill names are held to the same rule from the same definition:
  `.claude/agents/con.md` cannot exist there either, and the definition is
  committed for everybody. vat ships Windows binaries and runs Windows CI, so a
  workspace that only its author can clone is a defect and not a preference.
- `vat repo add` and `vat repo adopt` validate the name before doing anything,
  rather than reaching the same check through Save and reporting "vat.yaml is
  invalid" for a command that was called wrong. `vat repo new` has asked first
  since a name that escaped the workspace left files behind.
- A role or skill refusal says which rule was broken. Reporting "use letters,
  digits, '-', and '_' only" for the name `con` tells somebody their name
  violates a rule it satisfies, and leaves them retyping it.
- Two repositories whose directories differ only in case are refused. On macOS
  and on Windows they are one directory, so both manifest entries governed the
  same tree: `vat status` counted two, every rule fired twice, the generated
  `AGENTS.md` was written twice with different names so one was always drifted,
  and `vat repo remove --delete` on either would have taken the other's working
  tree with it. `vat repo new Payments` beside an existing `payments` advised
  `vat repo adopt Payments`, which is exactly how a workspace reached that
  state. Refused on every platform, because a manifest that validates on the
  author's Linux box and destroys a colleague's checkout on macOS is not the
  shared truth a workspace is built on — and `new`, `add`, and `adopt` all say
  so before building anything, rather than failing validation afterwards.
- The scan for abandoned adapters read every file under a runtime directory in
  full. A skill directory holds references and scripts beside its procedure, so
  a large asset was read end to end on every lint to decide it was not a
  generated pointer; the marker is in the first lines by construction.
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
- Four of the eight rules `AGENTS.md` says this code holds itself to are checked
  against the source: never silently modify a working tree, never rewrite a
  remote, every write is atomic, and the seam that keeps `internal/brain` clear
  of `manifest` and `gitx`. The file calls breaking one a defect regardless of
  what the change achieves, and the tool exists because a rule only written down
  is a hope. They were hopes.
- The generated workspace contract stays inside the byte budget its own lint
  rule enforces. A workspace of 120 repositories rendered 15,195 bytes and
  tripped `harness/workspace-oversized` — every byte of it written by vat, none
  of it removable, and `vat lint --fix` could not help. A rule that fires on a
  correct workspace is a rule that gets turned off, and this one guards the
  thing that matters most: past the budget a runtime stops loading the
  per-repository contracts below it. The roster now lists what fits and says how
  many it did not, because an agent handed a partial list and no count concludes
  it is the whole one.
- A manifest is never silently overwritten by a command that did not see the
  change it lands on. Eight `vat repo add` calls started together left a
  manifest holding two of them and reported eight successes: each read the whole
  file, added its entry to what it had read, and wrote the whole file back, so
  six additions were overwritten by commands that had never seen them. Silently
  losing work is the one outcome this tool exists to prevent, and it was doing
  it to its own manifest. The six are refused and told to re-run now — merging
  would be a guess about whether the other change belongs beside this one, and a
  state that cannot be resolved safely is reported. Sequential use is unchanged;
  the lock is held for one read and one write, so a clone or a commit elsewhere
  in a command never blocks anybody, and one left behind by something that died
  inside that window is taken after a minute rather than locking the workspace
  until somebody knows to delete a file.
- A filesystem error is reported with its path once. Twenty places wrapped an
  error that already carried the path with the path again, and two layers doing
  it printed it three times: a directory where a `SKILL.md` belongs reported
  `.agents/skills/weird/SKILL.md: read .agents/skills/weird/SKILL.md: read
  .agents/skills/weird/SKILL.md: is a directory`. That is the first thing
  somebody reads when their workspace is in a state they did not expect, and a
  message that cannot say a path once does not invite reading the rest. A source
  check keeps it from coming back.
- Internal links in the documentation are checked against the headings they
  point at. There is no build step over this Markdown, so an anchor that
  resolves to nothing sends a reader to the top of the page and looks like the
  page is broken, and nothing else would ever say.
- The architecture map in `AGENTS.md` is checked against the packages that
  exist, in both directions. It is the first thing that file tells a session to
  read, and it was a claim about the codebase that nothing verified: a package
  missing from it sends every session looking in the wrong place, and a line
  naming a package that was deleted sends them looking for nothing at all.
- Every sample of the `vat sync` summary is held to naming the buckets the code
  prints. Changing a summary line silently invalidates every sample of it, and
  the samples are what a reader takes as evidence the page describes the real
  thing — the buckets are checked and not the numbers, because the numbers
  belong to a scenario and asserting those would fail for reasons nobody should
  have to fix.
- The README and the adoption guide say how a colleague gets the workspace.
  `git clone` and then `vat sync` reconstructs every repository the manifest
  names, on the branch it records, with the contracts already in place — the
  single most common question a multi-repository tool is asked, and it was
  answered nowhere.
- The README's own `vat init` transcript showed a run that no longer happens.
  The guard added for the demo covered the recording alone, so the sample twenty
  lines below it went stale for exactly as long as nobody read it; every file
  carrying an init transcript is held to it now.
- The demo at the top of the README showed a `vat init` that no longer happens.
  Its own comment says to regenerate it after changing any of the output it
  shows, and nothing checked that — in the one asset a reader takes as evidence
  the tool does what the page claims. A contract test now holds it to naming
  every file `vat init` writes unconditionally, the seeded procedures included.
- `vat metrics` prints `—` for a measurement with nothing to measure. A median
  over no claims read as "0 days since the typical claim was verified" and a
  rate over no checks read as "0% failed" — the most flattering possible reading
  of a workspace that has verified nothing, in the numbers somebody quotes to
  justify the tool. `--json` gains `claims_measured` and `checks_recorded` so a
  consumer can tell an empty population from a genuine zero.
- `vat brain check` reports a record whose date cannot be read
  (`brain/date-unreadable`). `brain/claim-stale` and `brain/review-overdue` both
  ask the record how old it is and skip it when it cannot say, so one unreadable
  line exempted a record from both — the two rules that stop this layer filling
  with statements nobody has re-checked. An honest old record was reported and
  the unreadable one was silent.
- An `opened_at` a changeset record cannot be read as a date is reported. The age
  is computed by parsing it, and a parse failure returned zero — which reads as
  "opened today" and hid the record from `changeset/open-too-long`, the rule
  that finds cross-repo work somebody abandoned. One malformed line was enough,
  and so was an ISO timestamp, which the published schema did not forbid. The
  schema now pins the shape and SPEC §6 states it.
- A `vat.yaml` with no `version:` is refused instead of silently read as
  version 1. SPEC §4 lists the key as required and the published schema puts it
  in `required` with `minimum: 1`, so anyone validating a manifest against vat's
  own schema got a different answer from vat — out of a contract other tools
  build against. A test now drives every key the schema calls required through
  the validator.
- `vat lint` reports a submodule a repository declares but has never checked out
  (`repo/submodule-uninitialised`). vat clones without recursing, so the
  directory is empty, every build reads it as a missing dependency, and until now
  `vat sync` reported `CURRENT` and `vat status` reported clean while the
  canonical checks failed for a reason nothing in the tool named.
- Every command that renders a repository's `AGENTS.md` says to commit it, once,
  and stop once it is. The per-repository contract is the working permit for a
  session opened inside that repository alone, so it only works once it travels
  with the clone — and nothing anywhere said so, in the output or in the
  reference, while `harness/repo-missing` and `changeset verify` both depended
  on it. `vat lint --fix` is the command the tool itself advises for that
  finding, so it is the most travelled path to a rendered contract.
- `vat changeset verify` names the files that make a working tree dirty instead
  of only announcing that one is. The answer on a fresh workspace is the
  `AGENTS.md` vat rendered, and the refusal said nothing that would let anyone
  reach that conclusion.
- `vat status` separates `untracked` from `dirty`, and `vat doctor` no longer
  warns about untracked files. Three commands read the same working tree, so on
  a freshly enrolled repository they gave three answers: sync said `CURRENT`,
  status said `dirty`, doctor warned "uncommitted changes" — about the AGENTS.md
  vat had rendered. `--json` gains an `untracked` field, and `dirty` now means
  tracked files differing from HEAD or the index.
- Untracked files alone no longer make `vat sync` report `DIRTY`. Rendering the
  per-repository contract leaves an untracked `AGENTS.md` behind, so the very
  first sync of a new workspace reported every repository as dirty and advanced
  nothing — the whole first-run loop, blocked by a file vat had just written. A
  fast-forward cannot destroy an untracked file: git refuses the merge and
  errors, which sync reports. The commands that delete or move a tree still
  count untracked files, because there the untracked file is what is at risk.
- `vat repo add` refuses a directory that resolves outside the workspace, and
  the harness render writes nothing through one. A symlink inside the workspace
  pointing out of it satisfies every string comparison vat makes about the path.
  `repo new`, `repo adopt`, and `repo rename` already resolved the link; `repo
  add` reached an existing directory and rendered a contract into it, and
  `vat lint --fix` rendered through the same link in the run that reported it as
  the hazard.
- `vat lint` reports a duplicated managed region in `.gitignore`
  (`workspace/ignore-region-duplicated`). The same exposure `harness/region-duplicated`
  covers for `AGENTS.md` is worse here, because the last matching pattern in a
  `.gitignore` decides: vat maintains the first region and never looks past it,
  so a frozen second copy overrides it. `vat repo remove` reported success,
  dropped the directory from the region it maintains, and the abandoned copy
  below kept that tree invisible to git — against a command whose whole purpose
  is checking that no work is lost.
- `vat sync --dry-run` summarises the plan rather than an outcome. A repository
  it would clone was counted as "already current", which says the opposite of
  what the row means, in the line somebody reads to decide whether to run it for
  real.
- The summary under `vat sync` counted what advanced, what was left alone, and
  what needed attention, and nothing else — so a workspace where every
  repository was already current printed three zeros under a table of rows. A
  summary whose numbers do not add up to the table it sits under is the state
  this tool reports in other people's workspaces, and it read that way exactly
  when the run had gone perfectly.
- `vat exec` stated the outcome twice, as a summary and as an error line
  restating it inversely, both on the same stream. The rows above already name
  every failure and the exit code carries the verdict.
- `vat lint --only <typo>` and `vat brain check --only <typo>` reported "0 rules
  checked, nothing to report" and exited 0. `vat lint --only harness` in CI is
  what this project's own adoption guide recommends, so a mistyped selector
  bought a green build that checked nothing for as long as nobody looked — the
  exact failure this tool exists to prevent, committed by the tool. The match is
  a substring on purpose, so selecting a family still works; a value matching no
  rule at all is refused and told where the real names are.
- `vat sync` reported the branch the manifest declares rather than the one the
  repository is on, for a repository with no remote and for a fresh clone. That
  made this table and `vat status` disagree about the same repository at the
  same moment, and the row that was wrong is the one somebody would have used to
  conclude the manifest was right. A clone lands on the remote's own HEAD, which
  is not always what the manifest names.
- `vat status` and `vat sync` name an operation git was left part of the way
  through — a merge, rebase, cherry-pick, revert, or bisect. Every one of those
  leaves an ordinary dirty tree, and reporting it as only that invites the
  obvious reaction: commit the changes to clear the way. What gets committed is
  a file full of conflict markers, or a rebase abandoned halfway. `vat status`
  says it in the note and in `interrupted` under `--json`; `vat sync` says it in
  the detail beside `DIRTY`.
- `NO_REMOTE`, for a repository neither the clone nor the manifest names a
  remote for. `vat repo new --no-remote` produces exactly that, and `vat init
  --adopt` records it for any repository git has no origin for — and both
  `vat sync` and `vat doctor` then compared the placeholder they had written
  against the empty string git reports and called it a remote mismatch, which
  means something specific and dangerous. Every run failed forever, over a value
  vat wrote itself. `vat lint` had drawn the distinction from the start and said
  in a comment why. A clone that has lost the remote its manifest names is still
  reported: nobody can sync it, and the manifest says they should be able to.
- The `local:` convention had one definition per package that used it. It has
  one now, so reading it cannot drift from writing it.
- `repo/remote-missing` handed the placeholder back as a URL, so following its
  fix line created a remote that can never be fetched — the state the rule was
  reporting.
- `vat sync` counted no repository at all for a state it reported and left alone
  on purpose.
- `vat changeset verify` counted every condition that stops a repository being
  entered — no canonical checks, a dirty working tree, a clone that is not
  there, a participant no longer in the manifest — as a failed check, and
  summarised them as "N check(s) failed; recorded against the revisions they ran
  on" when nothing had run. That phrase is the evidentiary claim the whole
  record rests on, so a summary that makes it about checks which never executed
  is not a wording problem. A check that ran and failed is counted apart from a
  repository nothing could be run in; the specific reason stays beside each one.
- `vat changeset show` reported "1 checks passed".
- Every count this tool prints reads correctly at one. `vat doctor` said "1
  file(s) look like unencrypted secrets", "1 repositories, 0 archived", and "1
  commits exist only on this machine" — the last in the recovery section, which
  is the most alarming thing it prints; `vat harness render` said "1 files
  updated"; `vat brain adopt` said "1 records need attention". A report that
  cannot count to one is a report people stop reading closely, and the whole
  value of these sections is that somebody reads them closely enough to act.
- `vat doctor` could be made to certify a knowledge layer that does not exist,
  by following its own advice. On a repository declared as the brain and never
  initialised — the state every workspace with a directory called `brain` starts
  in — it reported an empty review queue and the projections as "out of date",
  and pointed at `vat brain build`. Running that wrote an index into a directory
  with no marker and no records, after which doctor reported the whole section
  healthy while `vat lint` still called it uninitialised. Two commands described
  one state differently and only one of them was right.
- Every `vat brain` command checked that the brain directory existed and never
  that it was a brain. The refusal now lives where all ten pass through.
- `vat brain adopt` reported "adopted as the brain repository" and left the
  directory without the marker, so `vat lint` answered "run vat brain init"
  about the repository just adopted. It writes the marker now, and builds the
  projections whose absence that marker turns into drift — and nothing else,
  because the command's whole promise is that an existing repository is brought
  under the rules gradually rather than scaffolded in one pass.
- The `.gitignore` managed region announced "Every repository below is an
  independent git repository" and then listed none, in a workspace that governs
  none. That text is committed into the repository of everybody adopting the
  harness on its own, where it reads as a defect in the generated file rather
  than as the state it describes.
- A workspace whose files a checkout rewrote is driven end to end, because four
  packages each answered the drift question and one of them getting it wrong was
  enough to keep the workspace permanently red.
- Drift is decided on content, not on line endings. Under git's default
  `core.autocrlf` on Windows every generated file comes back with CRLF, so a
  byte comparison reported the workspace contract, every repository contract,
  and every runtime adapter as drifted — on every run, on files nobody had
  touched. `vat lint --fix` rewrote them with LF, git converted them straight
  back, and the findings returned; the region repair also spliced an LF region
  into a CRLF document and left a file holding both. This repository pins its
  own tree in `.gitattributes` and its self-contract test already normalised for
  exactly this reason. A user's workspace has no such file, and the product did
  not. The knowledge layer's projections and the `.gitignore` managed region had
  the same comparison and the same outcome: `vat brain check` permanently red,
  `vat brain build` rewriting both files every run, and every command that
  touches the manifest reporting ".gitignore updated" on a file nobody had
  changed.
- A description cannot break the roster it is rendered into. The generated
  contract's table is what tells a session which repository owns what and which
  branch it ships from, and a description carrying a pipe split the row into six
  cells — so the branch column showed the tail of somebody's sentence and an
  agent reading it was told the wrong branch. A newline split the row across two
  lines and ended the table there. The trust table, whose cells come from lists a
  user edits, had the same hole in the row that states what untrusted content may
  do.
- A generated adapter's front matter survives whatever a description contains.
  Quoting without escaping produced `description: "a back\slash"`, and `\s` is
  not a YAML escape — the header did not parse, so the role was invisible to the
  runtime it was generated for. A newline folded to a space and a leading space
  was eaten, both silently. vat never noticed any of it: nothing reads an adapter
  back except a comparison against the string it just rendered, which agreed with
  itself. The Codex adapter had escaped correctly all along.
- A file saved with a UTF-8 byte order mark is read. The mark sits in front of
  the opening delimiter, so the header stopped being one — and nothing errored:
  the whole file became body, every declared field was lost, and a role that
  plainly carried a description was then reported as having none, sending
  somebody to fix a file that was already right. Roles, skills, and brain
  records all arrive through the one function that now strips it.
- `repo/nested`, for a governed repository inside another that does not exclude
  it. That is the harm `workspace/gitignore-drift` names, one level down: a
  commit in the outer repository swallows the inner one's whole tree and
  duplicates its history, and the outer one reads as permanently dirty until it
  does. The rule guarded the workspace root and nothing guarded this. Reported
  only when the outer repository does not already exclude it — asked of git
  rather than by reading a file, because the answer depends on every ignore file
  above it and on the user's own configuration.
- `harness/region-duplicated`, for a contract carrying more than one generated
  region. vat maintains the first and never looks at the rest: they keep
  whatever they hold, marked as generated, and every session loads them as
  though vat wrote them that morning — the failure this whole layer exists to
  prevent, inside the file that prevents it. Not repairable, because which of
  the two is the real one is a judgement and the abandoned one may be the only
  copy of something somebody wrote. Reported for the workspace contract and for
  every repository contract, which carry the identical structure and are where a
  session actually opens.
- `brain/schema-newer` is reported by `vat lint` and not only by `vat brain
  check`. The knowledge layer refuses to judge a brain written against a newer
  schema, for the reason its own comment gives — "the records would look clean
  because half of what governs them was invisible" — and lint read exactly that
  brain silently and certified it, in the command this project's adoption guide
  puts in CI.
- `vat changeset list` marks a closure that waived the landing gate. One closed
  with `--force` sat in the table beside changesets whose revisions were
  verified and observed on the branches they ship from, reading exactly alike —
  and which of the two it is is the whole of what the record says. `vat lint`
  reported it and `vat changeset show` revealed it; the table a person scans did
  not.
- An evidence packet listed the same canonical check once per repository that
  declares it, so a briefing for two repositories running `make check` said
  "make check" twice and told a worker nothing the scope line above had not. The
  packet records what will be run; the scope records where.
- `vat repo add --path ../outside` reached its check through Save and answered
  "vat.yaml is invalid" with exit 1 — "found errors" — for an argument that was
  simply mistyped, where a command called wrong is 2 and CI branches on the
  difference. Asked before anything is written, as the name check already is.
- An unknown flag is reported as a flag. `vat --nope status` answered "unknown
  command" and offered to suggest a verb, which sends somebody looking through
  the command list for something they never typed.
- `ssh://git@github.com/acme/x.git` is accepted. It is the SSH URL every forge
  publishes and `git` there is the login name, not a credential — and the
  scp-like form meaning exactly the same thing was accepted all along. Worse
  than the refusal: `vat repo adopt` strips userinfo rather than refusing, so
  adopting a repository cloned over SSH recorded an origin with the login name
  removed, and that URL does not authenticate. A password is still a credential
  whatever the scheme, and userinfo over http still is, because that is how a
  token is carried. `vat repo adopt` keeps the login and still strips the token,
  which are two different jobs that had been sharing one helper with the
  comparison that decides whether two spellings name the same repository — where
  the login is noise, and here it is the point.
- An origin or a branch beginning with `-` is refused. git reads such a value as
  an option wherever the call has no `--` in front of it, and the manifest that
  carries it is committed and reaches every colleague's machine. Refused where it
  enters rather than defended at each of a dozen call sites, and `git clone` now
  passes `--` as the second line of that.
- The entry point is covered. It was the one package `make cover` reported as
  not measured, and it decides the two things every invocation depends on: the
  exit code, and what happens to in-flight work when somebody presses Ctrl-C.
  Both had been fixed once by hand — the signal path by sending SIGINT to a
  running command rather than by reading the code — and neither was held to
  anything afterwards. Built and run for real, because what is under test is the
  process, with the wiring exercised in process as well so the lines are counted.
- Nothing writes to the terminal around the printer, which is where a control
  character stops being executed and starts being shown. A source check holds it
  there.
- Nothing vat prints can act on the terminal. The contract this tool generates
  says untrusted content is data and never instruction, and a terminal escape is
  an instruction to the terminal — while almost everything vat prints came out of
  a file somebody else may write: a record in the knowledge repository, a
  description in a manifest, a remote read back from `.git/config`, the text of a
  definition that would not parse. A record committed by anybody with access to
  the knowledge repository could erase the lines vat had just printed and write
  its own. Control characters are shown in caret notation rather than executed,
  and rather than dropped, so an attempt is visible instead of tidied away; the
  colour vat adds is applied around those values and never through them.
- A value carrying a newline no longer breaks the row it sits in. Every table
  and status line here is laid out in columns, and the values are free text
  somebody typed — a description, an objective, a record title — so one newline
  put the rest of the row on the next line and lost every column after it, in
  `vat harness roles`, `vat repo list`, `vat changeset list`, and the rest.
- A record title carrying a newline is refused. It becomes the record's H1,
  which is one line by construction: the first line was kept as the heading and
  the rest left sitting in the body as prose — half a title, silently, in the
  file the knowledge layer exists to make trustworthy.
- Tables align by what the terminal shows rather than by rune count. A wide
  character occupies two cells, so a Korean group name or a Japanese description
  shifted every column after it by one cell per character — in the output this
  tool prints most: `vat repo list`, `vat status`, `vat harness skills`,
  `vat changeset list`. The width table is written out here rather than taken
  from a dependency, because the dependency count is one and that is a security
  property.
- Every command shown in the documentation is checked against the command tree.
  The reference was already held to it; every other document showing one was
  not, so a renamed or removed command could sit in the README indefinitely with
  nothing to say. Anchored on a code span or a shell prompt, because "vat reads
  the manifest" is a sentence — and only the verb is checked, because nothing in
  the shape of a sample's later words separates a subcommand from a repository
  name or a query term.
- The README's `vat fit` sample showed thresholds the tool does not print —
  "agents work across more than one repository" where it says "coding agents
  work in this code at all", which is a different recommendation to a different
  person. Every sampled threshold is checked against the advisor now; the
  reasons are not, because a reason names the reader's own numbers.
- `vat fit` stated a fact about the reader from a flag default. `Signals` says
  anything left at zero is unknown and that the recommendation says so rather
  than guessing; two of the reasons guessed, telling a workspace nobody had
  described "with no agents in the loop" and "with no shared contracts". Neither
  is something vat can see — an interface crossing a boundary is not in any
  manifest — and an advisor that asserts your situation from its own defaults
  has stopped advising. The verdict is unchanged; the reason names what would
  settle it.
- `vat repo list` and `vat sync` printed a bare table header in a workspace that
  governs nothing. An empty table and a silent success look identical, which is
  what the existing guard on the other listings says — it just did not cover
  these two, and they are the ones somebody adopting the harness alone runs.
- `vat status` told a workspace that governs nothing "No repositories match.",
  which blames a filter nobody gave. It is the second screen somebody adopting
  the harness for a single repository sees, after the first one this release
  already fixed.
- `vat init` told a workspace that enrolled nothing that `vat status` would show
  "those repositories" as dirty, and offered `vat status` as a first step when
  there was nothing for it to report. Enrolling nothing is not a mistake — it is
  what adopting the harness for a single repository looks like, and that is the
  widest audience this layer has and the first screen it sees.
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
