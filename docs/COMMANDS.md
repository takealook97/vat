# Command reference

Every command that prints a table also accepts `--json`.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | ran and found nothing wrong |
| `1` | ran correctly and found problems (errors; warnings alone exit 0) |
| `2` | the invocation itself was wrong |

The distinction matters in CI: `1` means act on the findings, `2` means fix the
pipeline.

A workspace whose `requires.vat` this build does not satisfy is refused before
the command runs, exiting `1` and naming both the constraint and the running
version. See [MANIFEST.md](MANIFEST.md#requires); this applies to every command
that opens a workspace, `vat doctor` included, for the same reason a manifest
written against a newer schema is refused: a diagnosis produced by rules the
file does not use is not a diagnosis.

## Global flags

| Flag | Effect |
| --- | --- |
| `--workspace <dir>` | use this workspace root instead of searching upward |
| `--json` | machine-readable output |
| `--quiet` | print only warnings and failures |
| `--no-color` | disable colour (also honours `NO_COLOR`) |
| `--yes` | assume yes for confirmations that are not destructive |

`VAT_WORKSPACE` pins the root for a whole shell or CI job.

Flags are accepted in any position, including after a positional argument.

### Which stream carries what

**stdout is the data.** Tables, `--json` payloads, and the guidance a command
gives after succeeding. Nothing else, so `vat … --json | jq` is safe on every
path: a command that fails writes nothing to stdout at all.

A script reading vat must check the exit status, not only the output. A roster
that could not be read prints nothing on stdout, and a loop over nothing looks
exactly like a workspace governing nothing — so a wrapper that discards the
status turns "vat failed" into "there are no repositories", and every check
built on it passes. Capture the output, check the status, then iterate.

**stderr is the diagnosis.** Errors, the `usage:` line beside one, and the
advice that belongs to a failure. A consumer reads the exit code, then stderr.

The `usage:` line used to go to stdout, which made a failing `--json` run hand
its consumer a parse error while the reason sat on the stream nothing was
reading — and `vat … > out.json` wrote the usage line into the file.

---

## vat init

```
vat init [--name <name>] [--adopt] [--from-tsv <file>]
```

Creates `vat.yaml` in the current directory, writes the `.gitignore` managed
region, seeds the starter procedures, and renders the harness.

The seeded skills — `before-cross-repo-work` and `consult-the-brain-first` —
describe vat's own command sequences and nothing else, because a procedure vat
writes once and never maintains would be a second source of truth of exactly the
kind this tool exists to remove. A test holds every command they name to
actually existing. They are canonical files under `.agents/skills/` from the
moment they land: edit them, or delete them, with no consequence.

| Flag | Effect |
| --- | --- |
| `--name` | workspace name (default: the directory name) |
| `--adopt` | enrol every git repository already sitting beside the manifest |
| `--from-tsv` | import a legacy `name<TAB>origin` manifest |

`--adopt` reads each repository's origin and current branch and records them as
they are. Nothing is moved, cloned, or re-pointed. A repository checked out on a
branch other than `main` gets an explicit `default_branch`, so updates do not
silently skip it forever.

Roles are guessed from the name (`brain`, `credential`, `docs`, `infra`) and
written into the manifest where they are visible and easy to correct.

A name that cannot become a directory on Windows is refused on every platform:
the device names `con`, `prn`, `aux`, `nul`, `com0`–`com9`, `lpt0`–`lpt9` — with
or without an extension, because Windows matches the device before the first dot
— and any name ending in `.` or a space, which Windows strips silently so the
directory would not be the one named here. Role and skill names are held to the
same rule, for the same reason: `.claude/agents/con.md` cannot exist there
either.

Two repositories whose directories differ only in case are refused, on every
platform. They are one directory on macOS and on Windows, and a manifest that
validates on the author's machine and puts two entries over one colleague's
working tree is not the shared truth a workspace is built on.

Two `vat` commands changing the manifest at once are serialised, and the later
one is refused rather than overwriting work it never saw: it reads the whole
file, edits what it read, and writes the whole file back. Re-run it. `.vat/` is
vat's own local state and is excluded from the workspace's history.

A workspace that enrols nothing is a supported state, not a mistake: it is what
somebody adopting the harness for a single repository ends up with. `init` says
so rather than warning about repositories that do not exist, and offers
`vat harness adopt` as the next step.

---

## vat status

```
vat status [--group <g>] [--role <r>] [--only <names>] [--dirty] [--fetch] [--archived]
```

Branch, revision, working-tree state, and divergence for every repository. No
network unless `--fetch` is given, so it is safe to run constantly.

The `TREE` column separates `dirty` — tracked files differing from HEAD or the
index, which is work at risk — from `untracked`, which is not. vat renders a
contract into every repository, so an untracked file is the normal state of a
freshly enrolled one, and calling that dirty said every repository in a new
workspace held uncommitted work. Under `--json` they are the `dirty` and
`untracked` fields.

`--dirty` narrows the list to repositories holding work that exists only here:
tracked modifications, untracked files, unpushed commits, or stashes.

---

## vat sync

```
vat sync [--dry-run] [--offline] [--group <g>] [--role <r>] [--only <names>] [--jobs n]
```

Fetches, then fast-forwards only what can be advanced without losing anything.

| State | Meaning | Fails the run |
| --- | --- | --- |
| `CURRENT` | already matches upstream | |
| `UPDATED` | clean default branch fast-forwarded | |
| `CLONED` | was absent, cloned from origin | |
| `DIRTY` | uncommitted changes to tracked files; nothing advanced | |
| `BRANCH` | on another branch; nothing advanced | |
| `DETACHED` | HEAD is not on a branch | |
| `AHEAD` | local commits not pushed | |
| `ARCHIVED` | excluded from updates | |
| `PLANNED` | what `--dry-run` reports instead of acting | |
| `NO_REMOTE` | neither the clone nor the manifest names a remote | |
| `MISSING` | absent and not cloned | yes |
| `NOT_GIT` | directory exists but holds no repository | yes |
| `REMOTE_MISMATCH` | origin points somewhere the manifest does not name, or is absent where it names one | yes |
| `FETCH_FAILED` | the network step failed | yes |
| `DIVERGED` | both sides hold commits the other does not | yes |
| `NO_UPSTREAM` | the default branch has no remote-tracking ref | yes |

Untracked files alone are not `DIRTY`. A fast-forward cannot destroy one: git
refuses the merge and errors, which sync reports. Counting them made the first
sync of a new workspace report every repository as dirty, the only uncommitted
change being the `AGENTS.md` vat had just rendered into it. The commands that
delete or move a tree still count untracked files, because there the untracked
file is exactly what is at risk.

A repository left part of the way through a merge, rebase, cherry-pick, revert,
or bisect is dirty, and `DIRTY` says which — because the obvious reaction to an
uncommitted change is to commit it, and what that commits is a file full of
conflict markers. `vat status` reports the same thing in its note, and in
`interrupted` under `--json`.

`--dry-run` contacts nothing at all. A dry run that fetches is not a dry run.

`--offline` skips every network operation and reports local structure only.

---

## vat doctor

```
vat doctor [--network] [--offline] [--secret-max-age <days>]
```

Judges the environment and stops. It never repairs anything and never prints a
secret value.

| Section | Checks |
| --- | --- |
| tools | `git` version; `gh`, `sops`, `age` presence |
| workspace | manifest validity, root versioning, `.gitignore` coverage |
| repositories | presence, origin, branch, cleanliness |
| credentials | files that look like plaintext secrets, encrypted count, age since last change, key material readable by other users |
| brain | record counts, review queue, generated-file freshness |
| changesets | open and overdue work |
| recovery | commits and stashes that exist only on this machine |
| network | `--network` only: whether the GitHub CLI is authenticated, and the platform vat is running on |

`--secret-max-age` defaults to 180 days; `0` disables the check.

**recovery** answers the one question a backup exists to answer: if this machine
stopped working now, what would be gone. For a git repository that has a precise
form — does its history exist anywhere but here — and unpushed commits and
stashes are the two states where the answer is no.

`vat` takes no backups and will not. Where an archive goes, how it is encrypted,
and who holds the key are organisational facts this tool owns none of, and
writing outside the workspace root is the boundary everything else rests on. So
this reports, like the rest of `doctor`, and stops.

---

`--offline` is accepted and states the default: doctor reads the machine and the
workspace and reaches the network only under `--network`. `sync` and `lint` both
take the flag, and a script passing it everywhere failed on the one command that
was already offline. Passing both is refused rather than resolved silently.

## vat lint

```
vat lint [--fix] [--offline] [--only <substring>] [--list]
```

The rules, and what each one prevents:

| Rule | Severity | Prevents |
| --- | --- | --- |
| `workspace/gitignore-drift` | error | a root commit swallowing an entire nested clone |
| `workspace/ignore-region-duplicated` | error | a frozen second copy of the roster overriding the one vat maintains |
| `workspace/not-a-repository` | warn | an unversioned manifest and harness |
| `repo/missing` | error / warn | a repository never cloned; error when `required`, warning otherwise — `vat sync` draws the same line |
| `repo/not-a-repository` | error | a directory shadowing a governed repository |
| `repo/submodule-uninitialised` | warn | a build failing on an empty directory in a repository vat reports as clean |
| `repo/outside-workspace` | error | a governed directory that resolves, through a symlink, outside the workspace vat may write to |
| `repo/nested` | error | a governed repository inside another that does not exclude it, where a commit would swallow its whole tree |
| `repo/remote-mismatch` | error | fetching from somewhere the manifest does not name |
| `repo/remote-missing` | warn | a repository that can never be fetched or pushed |
| `repo/credential-in-remote` | error | a token left in a clone's `.git/config`, which the remote comparison cannot see because it strips userinfo before comparing |
| `repo/default-branch-missing` | warn | a `develop` repository skipped by every update, silently |
| `repo/checks-missing` | warn | a changeset with nothing to verify |
| `harness/workspace-missing` | error | a workspace with no agent contract |
| `harness/workspace-drift` | error | a contract that no longer describes the workspace |
| `harness/region-duplicated` | warn | more than one generated region in a contract, where vat maintains the first and never looks at the rest |
| `harness/workspace-oversized` | warn | a root file that truncates the contracts below it |
| `harness/repo-missing` | warn | a session opened in a repository with no contract |
| `harness/repo-drift` | warn | the same, out of date |
| `harness/adapter-drift` | warn | one role or skill behaving differently per runtime |
| `harness/role-metadata` | warn | a role no runtime can advertise |
| `harness/model-ambiguous` | warn | one model name written into two vendors' adapters, where it resolves in at most one |
| `harness/skill-metadata` | warn | a skill on disk that no runtime can offer, because it has no description |
| `harness/definition-malformed` | error | a role or skill file that cannot be read, reported instead of withdrawing every other definition beside it |
| `harness/adapter-orphaned` | warn | a generated adapter left behind by a deleted definition, still loaded by the runtime and pointing at a file that is gone |
| `harness/runtime-unknown` | warn | a `runtimes:` value that generates no adapter, leaving the definition inert while every other rule passes |
| `policy/trust-undeclared` | warn | a harness that cannot say which content is data |
| `brain/not-initialised` | warn | a declared knowledge repository with no records |
| `brain/schema-newer` | error | a knowledge layer written against a schema this build cannot read, which these checks would otherwise certify |
| `brain/unreferenced` | warn | a scaffolded brain the manifest never adopted, which no `vat brain` command can reach |
| `brain/generated-drift` | error | a hand-edited projection contradicting the records |
| `brain/projection-unmanaged` | error | a file holding the name of a generated projection that vat did not write, which no build will overwrite |
| `brain/source-revision-drift` | warn | a claim whose evidence moved on months ago |
| `brain/source-repo-unknown` | warn | a claim pointing at a repository that is not governed, naming the remedy that does not widen the roster |
| `brain/view-stale` | warn | a maintained view the generated index routes to, left behind by the records for longer than the review window |
| `brain/source-external-governed` | error | a claim declaring its source external while the workspace governs it, which would exempt a checkable claim from every check |
| `changeset/invalid` | error | a completion record that cannot be acted on |
| `changeset/closed-unlanded` | warn | a changeset whose closing waived the landing gate, so the waiver stays visible |
| `changeset/open-too-long` | warn | repositories mid-contract-change with no closing evidence |

`--only` matches a substring, so `--only harness` selects the whole family. A
value matching no rule at all is refused rather than reported as a clean run: a
mistyped selector in CI buys a green build that checked nothing, for as long as
nobody looks. `--list` names every rule.

`--fix` repairs only what can be repaired without judgement: it regenerates what
is generated and re-excludes what should have been excluded. It never edits a
fact, a decision, or a working tree.

Neither `repo/outside-workspace` nor `repo/credential-in-remote` is repairable,
and deliberately so. Moving a repository is a decision about layout, and
rewriting a remote is the one operation this tool does not perform — stripping a
credential from `.git/config` would also break the next push for anyone relying
on it. Both findings report and hand you the command.

`--offline` skips the rules that resolve a git revision.

---

## vat exec

```
vat exec [--group <g>] [--role <r>] [--only <names>] [--checks]
         [--jobs n] [--timeout <duration>] [--keep-going=false] -- <command>
```

Runs a command in every selected repository, in parallel, with per-repository
results. A failure in one is never hidden by success in another.

A job stopped by `--timeout` or by Ctrl-C is reported as timed out or
interrupted rather than as the signal that killed it, which renders as an exit
status of `-1` and says nothing about what happened.

**Your command is executed directly, not re-parsed by a shell.** Quoting
survives, so `-- git commit -m "wip; cleanup"` commits with that message rather
than also running a second command. Ask for a shell explicitly when you want
one: `-- sh -c 'for f in *.go; do echo $f; done'`.

`--checks` runs each repository's own canonical checks from the manifest instead
of a command you supply — how to ask "is everything still green?" without
knowing what each repository uses to answer that. Those are shell fragments by
contract and do run through a shell.

`--keep-going=false` abandons the remaining repositories after the first
failure; they are reported as skipped, which is neither a pass nor a failure.

A `--group` or `--role` that matches no repository is an error, not an empty
run — in CI an empty run is a green build that tested nothing.

---

## vat repo

```
vat repo list    [--group <g>] [--role <r>] [--archived] [--format tsv]
vat repo add     <name> --origin <url> [--role <r>] [--group <g>] [--branch <b>]
                        [--checks <cmds>] [--access <a>] [--description <text>]
                        [--required=false] [--path <dir>] [--no-clone]
vat repo new     <name> [--role <r>] [--group <g>] [--branch <b>] [--checks <cmds>]
                        [--access <a>] [--description <text>] [--private]
                        [--remote <url>] [--no-remote]
vat repo adopt   <directory> [--role <r>] [--group <g>] [--branch <b>]
                        [--checks <cmds>] [--access <a>] [--description <text>]
                        [--required=false]
vat repo remove  <name> [--delete] [--force]
vat repo archive <name>
vat repo unarchive <name>
vat repo rename  <old> <new> [--keep-path] [--origin <url>] [--plan]
```

Every mutation moves the manifest, the `.gitignore` exclusion, and the generated
harness together, because changing one without the others is the failure mode.

A repository name may hold only letters, digits, `.`, `_`, and `-`, and `new`
checks it before it creates anything. `adopt` refuses a directory that resolves
outside the workspace, which a symlink can do while looking as though it does
not. The name becomes both a directory and,
through `remote_template`, part of a URL.

Every flag is validated before anything is created, so a typo cannot leave a
directory behind that is in neither the manifest nor `.gitignore`.

`rename` moves the manifest entry, the directory, the `.gitignore` exclusion,
`policy.brain.repo` when the repository is the knowledge layer, and the
generated contracts together. `--origin` records the URL a repository answers to
after a rename on the forge; the clone's remote is never rewritten, because a
remote that does not match the manifest is a supply-chain signal and vat reports
it rather than smoothing it over. `--plan` reports every effect and writes
nothing.

A repository enrolled in an **open** changeset is refused. That record claims
which revisions were proven together, so a rename would either leave it pointing
at a repository that is gone or rewrite a claim about the past. A closed,
rolled-back, or abandoned record describes the past under the name it used then
and is left alone.

`list --format tsv` writes name, role, group, branch, state, and origin
separated by tabs, with no header and no alignment. The aligned table is for
people and changes shape with its content; JSON needs a parser. A shell script
replacing a hand-maintained roster file should not have to reach for one to read
the roster that replaced it. Check the exit status: a roster that could not be
read must not be indistinguishable from a workspace governing nothing.

`new` initialises the repository locally with a starter harness, commits it,
creates the remote through the GitHub CLI unless `--no-remote`, and enrols it.
A credential repository is created with ignore rules that refuse plaintext by
default.

`adopt` reads what is on disk. Explicit flags win; anything not given keeps what
was discovered.

`remove` refuses while uncommitted changes, unpushed commits, or stashes exist.
Stashes are invisible to `git status`, which is exactly why they are the work
most often destroyed by a cleanup. `--force` overrides. `--delete` always
prompts, even with `--yes`.

`archive` keeps a repository in the record while excluding it from daily
commands — for a repository that is finished rather than gone.

---

## vat harness

```
vat harness render
vat harness check
vat harness roles
vat harness skills
vat harness role new  <name> [--writes <repos>] [--reads <repos>] [--model <m>]
                             [--effort <e>] [--description <text>] [--runtimes <list>]
vat harness skill new <name> [--description <text>] [--runtimes <list>]
vat harness adopt [--apply]
```

`render` writes the generated region of every `AGENTS.md` and every runtime
adapter. Content outside a generated region is never touched.

Repository regions point to the workspace's brain for wider context. The brain
repository instead identifies itself as that layer; it never emits a circular
`../<brain>` pointer or tells a session not to write to the repository whose
working permit it is reading.

A repository's `AGENTS.md` belongs in that repository's history: it is the
contract a session opened inside it reads, and it only travels with the clone
once it is committed. vat writes it and commits nothing, so every command that
renders one names it until it is committed — after that the line stops.

`role new` creates a runtime-neutral role under `.agents/roles/`. A role
defaults to **read-only**: write access is granted by naming the repositories it
may change, because a role that can edit anything is a role whose boundary
cannot be reviewed.

`skill new` creates a runtime-neutral procedure under `.agents/skills/`. A role
is who is running; a skill is a procedure loaded on demand. The body stays
canonical and each adapter carries a pointer to it, never a copy.

A skill renders an adapter for Claude Code and for no other runtime, so
`--runtimes codex` produces a definition that generates nothing. The command
says so at creation rather than leaving it for the next `vat lint`. It also says
when a skill has no `--description`, because the description is the whole of
what a runtime advertises, and one invented here would satisfy
`harness/skill-metadata` while telling the runtime nothing.

`skills` lists what is defined, reporting the runtimes that actually render an
adapter rather than the `runtimes:` field, because those differ exactly where it
matters.

`adopt` moves a runtime's hand-written agent files into `.agents/` and generates
the adapters from them. Anybody who would benefit from vat's harness already has
agent files, written by hand into one runtime's directory, and copying them
across is where adoption usually stops.

It reports and writes nothing until `--apply`. A file vat generated is skipped,
and so is one whose canonical definition already exists — that is drift, which
`harness/adapter-drift` reports, and the canonical copy is the one file vat will
not overwrite. An adopted role records the runtime it came from and no other,
because a bare `model` is honoured only by a role targeting one runtime and
claiming a second would name a model that runtime cannot resolve. It grants no
write access, matching what a role with no declared write target gets.

A header key the canonical format has no field for is reported rather than
discarded in silence — the runtime the file was written for may well honour it,
and rewriting somebody's file while dropping half its header is the quiet kind
of data loss. It is said before `--apply` as well as after.

Only the Markdown adapters are candidates. A Codex adapter keeps its prose
inside a TOML string, and turning that back into a body is a conversion with
judgement in it.

A role or skill name may hold only letters, digits, `-`, and `_`. It is pasted
into a path in `.agents/roles/`, in `.agents/skills/`, and in every runtime's
adapter directory, and adapters are written whole rather than into a marked
region.

---

## vat brain

```
vat brain init      [directory]
vat brain new       <goal|gap|decision|memory> --title "..." [--claim <kind>]
                    [--owner <repo>] [--axis <a>] [--refs <ids>] [--id <id>]
vat brain build
vat brain check     [--only <rule>] [--list]
vat brain query     <terms...> [--all] [--limit n]
vat brain review    [--overdue] [--limit n]
vat brain sweep     [--apply]
vat brain promote   <id> [--reviewer <name>] [--reverified]
vat brain supersede <old-id> <new-id>
vat brain quarantine <id> --reason "..."
vat brain revoke    <id> --reason "..."
vat brain resolve   <id> [--reason "..."]
vat brain archive   [--apply]
vat brain adopt     <repository-name>
```

`new --claim current-state --owner <repo>` records the owning repository's
current revision as the claim's evidence. Records enter as `provisional`.

`query` searches a deliberately narrow surface. `--all` widens it to history,
archives, and terminal records — for auditing why something was decided, rather
than asking what is true now.

`review` orders by priority: how many records cite the claim, weighted against
how long it has gone unverified.

`sweep` lists proposed demotions; `--apply` writes them.

`promote` refuses a current-state claim with no `owned_by` and no `source_ref`.
It also refuses to move the observation date forward unless the evidence is
demonstrably unchanged — the owning repository is still at the pinned revision —
or you pass `--reverified` to state that you re-read the source yourself, in
which case the claim is re-pinned to the revision you read. When
`policy.gates.brain_promote` is `manual`, `--reviewer` is required.

`supersede` leaves the replacement `provisional` when
`policy.brain.require_promotion_gate` is set, so a new decision still crosses the
gate rather than becoming canonical on its way past.

`quarantine`, `revoke`, and `resolve` are the rest of the lifecycle. A
quarantine or a revocation must state a reason: a withdrawal nobody explained
cannot be reviewed later. An end state is one-way — none of these commands, and
not `promote`, will reopen one.

`adopt --plan` writes nothing at all — not the marker, not the projections, not
the manifest — and groups what adoption would find: how many records cannot be
read, how many carry a status this schema does not have, how many relations are
one-sided, which projections vat did not write, and which directories under
`memory/` are shaped like a session journal rather than a reusable observation.
Each group says whether its shape is mechanical. It proposes no mapping: a
knowledge repository is the one thing in a workspace whose content no tool
should reinterpret, and the value is making the work countable.

`adopt` declares which governed repository holds the knowledge layer. It writes
the `.brain` marker, because a repository declared as the brain and left without
one is a repository every other command still calls uninitialised, and it builds
the projections, because marking it makes their absence into drift and a
generated file is vat's to write. Nothing else is written: an existing
repository is brought under the rules gradually, not scaffolded in one pass.

`build` and `adopt` write a projection only when the file already at that name
is empty or carries vat's own provenance — see [SPEC.md](SPEC.md) §5.7. A
knowledge repository older than vat usually keeps a `CURRENT.md` of its own, and
it is typically the most valuable file in it. Such a file is reported as left
alone and kept exactly as it was found; `vat lint` reports the same state as
`brain/projection-unmanaged`, whose remedy is to move or delete the file, not to
run a build that will not touch it.

The generated `CURRENT.md` also routes to each maintained root view that exists:
current state, goals, gaps, execution order, decisions, reviewed observations,
and the agent operating model. `STATUS.md` is the standard current-state name;
an adopted brain that already uses `PORTFOLIO_STATUS.md` keeps that name and is
routed there when `STATUS.md` is absent.

Every `vat brain` command refuses a repository that has no marker, naming
`vat brain init` and `vat brain adopt`. Without that refusal `vat brain build`
wrote an index into a directory that is not a brain.

`archive` moves superseded, revoked, and resolved records into `archive/`,
repointing the relative links inside them. Nothing is deleted and an archived
record is still loaded, so its supersession chain is still checked from both
ends; `--apply` writes the moves.

`check --list` names every rule it can report, and `--only` narrows a run to
one class while you work through it. `--only` matches a substring, so
`--only claim` selects a family — and a value matching no rule at all is
refused rather than reported as a clean run, because a mistyped selector in CI
buys a green build that checked nothing. The rules are tabulated in
[BRAIN.md](BRAIN.md).

`check` reports `brain/record-malformed` for a file it cannot read as a record
and keeps loading the rest, and `brain/record-secret-suspected` for a record
that appears to carry a credential. The second names the line and the kind of
credential, never the value.

`adopt` points the workspace at an existing knowledge repository and reports
which records do not yet meet the schema. Nothing is rewritten.

See [BRAIN.md](BRAIN.md) for the record schema and the full lifecycle.

---

## vat changeset

```
vat changeset new       "<objective>" [--repos a,b] [--non-goal "..."]
                                      [--contract "..."] [--decision <ids>]
vat changeset add       <id> <repo>...
vat changeset status    <id>
vat changeset verify    <id> [--timeout <duration>]
vat changeset show      <id>
vat changeset list      [--open]
vat changeset close     <id> --acceptance "..." [--approved-by <name>] [--force]
vat changeset abandon   <id> [--reason <text>]
vat changeset undo-plan <id>
```

`new` requires an objective. The objective is the one claim the record makes,
which is the same reason `--acceptance` may not be empty when closing.

`new` and `add` record where each repository stands before the change begins,
because after it lands that can no longer be observed. `add` refuses once the
changeset is closed or abandoned: enrolling a repository afterwards rewrites the
one claim the record exists to make. A repository with no commits is refused
too: the return point is the one field enrolment exists to capture.

The workspace root is enrolled as `.`, and it is the only name that resolves
outside `repos:`. A contract change usually starts in the control plane — the
manifest, a role, a generated region every repository reads — and recording the
products without it left the one revision nothing could roll back to out of the
record. It is verified by `workspace.checks` in vat.yaml, and `vat ship` judges
its landing against the same `origin/<default_branch>` as any other participant.

Because the record itself is written under `changesets/` in the workspace root,
enrolling `.` means committing the record before `verify` — the same rule every
other participant is held to, arriving one step earlier.

`status` is the preflight. It reports where every participant stands and
commits nothing, distinguishing six states: `uncommitted` (a dirty tree, so no
result would describe a revision), `unverified`, `moved` (verified, and the
repository has moved on since), `unverifiable` (no canonical checks declared),
`verified`, and `landed`. When trees are dirty it prints the commits needed, in
enrolment order — adopting the harness dirties every governed repository at
once, and the order was previously discoverable only by running `verify` and
reading the failures. It exits 0 either way: it is a report, not a gate.

A failing check records a bounded tail of its output in the changeset, redacted
and marked when it was cut. `detail` alone is the command's first line, which
for most test runners is a progress bar, and a record that can prove a failure
but not explain it has kept the half nobody needs. A passing check records no
output: this is a completion record, not a log.

A participant that declares no canonical checks is recorded as `unverifiable`
with the reason, rather than left with an empty check list that reads the same
as work in progress.

`verify` runs each repository's canonical checks and records the outcome against
the exact revision it ran on. Four conditions stop a repository being entered at
all — no canonical checks, a dirty working tree, a clone that is not there, a
participant no longer in the manifest — and those are counted apart from a check
that ran and failed. Both stop the changeset, and they are not the same fact:
one is evidence that something broke, the other is the absence of any evidence.
The specific reason is printed beside each repository. It refuses on a dirty
working tree, because results recorded against a revision that does not describe
what was tested are worse than none, and refuses on a changeset that is already
closed. A dirty tree is named, not merely announced: the refusal lists what git
reports as changed, bounded, because otherwise the answer is usually the
`AGENTS.md` vat rendered and nothing said so.

`close` requires `--acceptance`, and it must describe something end to end.

`show` prints the objective, the status, the acceptance, and the notes — which
is where `abandon --reason` is kept, because why work stopped is the whole value
of an abandoned record.

`list` marks a closure that waived the landing gate. A changeset closed with
`--force` is not the same fact as one whose revisions were verified and observed
on the branches they ship from, and which of the two it is is the whole of what
this record says.

`list --open` narrows to unfinished work in both renderings, so a CI job asking
for it in `--json` gets the same answer the table shows.

`undo-plan` prints the commands that would return every repository to its start
point, in reverse enrolment order. `vat` prints and never runs it.

See [CHANGESETS.md](CHANGESETS.md).

---

## vat ship

```
vat ship <id> [--remote <name>] [--offline]
```

Report, per repository, whether the revision a changeset verified has reached
the branch that repository ships from. `vat` pushes nothing and merges nothing:
this judges, the way `doctor` judges.

The test is one git question — is the verified revision an ancestor of
`<remote>/<default-branch>` — so it has the same answer on GitHub, GitLab,
Gitea, and a bare remote on a server you own. A pull request is recorded as
evidence when you supply one and is never the gate; an open pull request is
precisely the state of not having landed.

```console
$ vat ship CS-0007
OK    payments                  landed on origin/main
FAIL  console                   9f2a1c4 is not on origin/main; verified but not landed

Result
FAIL  ship                      1 repository of 2 not landed
Land the outstanding work, then run this again.
```

| Exit | Meaning |
| --- | --- |
| 0 | every repository landed |
| 1 | at least one has not landed, could not be judged, or the changeset is not verified yet |
| 2 | no such changeset, none enrolled, or the changeset is already finished |

What was observed is written back to the changeset — `landed_on` and `landed_at`
per repository — including a partial result, because a half-landed change is the
state somebody needs to see the next morning.

A previous answer is cleared only by an observation that contradicts it. Failing
to *look* is not an observation: a run with no network, or one where a repository
is briefly not cloned, leaves the existing evidence alone. Clearing it there
would erase the record of a change that really had shipped, and no later run
could put it back.

"The ref is not here" and "the commit is not on it" are reported differently.
`<remote>/<branch>` missing from a clone means the question could not be
answered, not that the work failed to land, and the closing line says which of
the two it was.

`--offline` judges against refs already fetched, without contacting the remote.

`vat changeset close` refuses a changeset that has not landed, and points here.
`--force` still closes it, and `changeset/closed-unlanded` then reports the gap
rather than letting the waiver disappear.

---

## vat evidence

```
vat evidence new   <id> "<objective>" --repos a,b [--acceptance "..."]
                        [--non-goal "..."] [--contract "..."] [--refs <ids>]
                        [--changeset <id>] [--release-authority] [--markdown]
vat evidence show  <id> [--markdown]
vat evidence list
vat evidence check [<id>]
```

The contract a worker is given before it starts. `--markdown` renders the
briefing to paste into a session.

`check` with an identifier fails if no packet has it, rather than reporting
nothing: a merge gated on a packet that was never written must not pass.

An evidence or brain identifier may hold only letters, digits, `.`, `_`, and
`-`. Both become filenames.

`new` refuses to overwrite an existing packet: silently replacing the acceptance
criterion defeats the point of writing one down.

Release authority is false unless explicitly granted.

---

## vat metrics

```
vat metrics [--record] [--history]
```

`--record` appends a snapshot to `.vat/metrics.jsonl` so trends become visible.
A single reading says little.

A measurement with nothing to measure prints `—`, not a number. A median taken
over no claims and a failure rate over no checks are not zero: printed as `0`
and `0%` they are the most flattering possible reading of a workspace that has
verified nothing. Under `--json` the populations are `claims_measured` and
`checks_recorded`, so a consumer can tell an empty one from a genuine zero.

---

## vat fit

```
vat fit [--repos n] [--contracts n] [--people n] [--agent-sessions n]
        [--secret-repos n] [--decisions-lost]
```

Per-layer break-even verdict. Numbers are read from the workspace where they can
be. `--contracts` is the important one: how many interfaces cross a repository
boundary is what makes a multi-repo layout expensive, not repository count.

`--json` returns one object per layer with the fields `layer`, `adopt`,
`threshold`, `because`, and `command`.

---

## vat version

```
vat version [--short]
```

The build identity: the semantic version, the commit it came from, when it was
built, and the toolchain. A build with no linker stamps — one produced by
`go install` — falls back to the module version and VCS metadata the Go
toolchain records.

---

## vat completion

```
vat completion <bash|zsh|fish>
```

```bash
vat completion bash > /usr/local/etc/bash_completion.d/vat
vat completion zsh  > "${fpath[1]}/_vat"
vat completion fish > ~/.config/fish/completions/vat.fish
```
