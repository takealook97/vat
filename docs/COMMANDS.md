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

---

## vat init

```
vat init [--name <name>] [--adopt] [--from-tsv <file>]
```

Creates `vat.yaml` in the current directory, writes the `.gitignore` managed
region, and renders the harness.

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

---

## vat status

```
vat status [--group <g>] [--role <r>] [--only <names>] [--dirty] [--fetch] [--archived]
```

Branch, revision, working-tree state, and divergence for every repository. No
network unless `--fetch` is given, so it is safe to run constantly.

`--dirty` narrows the list to repositories holding work that exists only here:
uncommitted changes, unpushed commits, or stashes.

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
| `DIRTY` | uncommitted work; nothing advanced | |
| `BRANCH` | on another branch; nothing advanced | |
| `DETACHED` | HEAD is not on a branch | |
| `AHEAD` | local commits not pushed | |
| `ARCHIVED` | excluded from updates | |
| `PLANNED` | what `--dry-run` reports instead of acting | |
| `MISSING` | absent and not cloned | yes |
| `NOT_GIT` | directory exists but holds no repository | yes |
| `REMOTE_MISMATCH` | origin points somewhere the manifest does not name | yes |
| `FETCH_FAILED` | the network step failed | yes |
| `DIVERGED` | both sides hold commits the other does not | yes |
| `NO_UPSTREAM` | the default branch has no remote-tracking ref | yes |

`--dry-run` contacts nothing at all. A dry run that fetches is not a dry run.

`--offline` skips every network operation and reports local structure only.

---

## vat doctor

```
vat doctor [--network] [--secret-max-age <days>]
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
| network | `--network` only: whether the GitHub CLI is authenticated, and the platform vat is running on |

`--secret-max-age` defaults to 180 days; `0` disables the check.

---

## vat lint

```
vat lint [--fix] [--offline] [--only <substring>] [--list]
```

The rules, and what each one prevents:

| Rule | Severity | Prevents |
| --- | --- | --- |
| `workspace/gitignore-drift` | error | a root commit swallowing an entire nested clone |
| `workspace/not-a-repository` | warn | an unversioned manifest and harness |
| `repo/missing` | error / warn | a repository never cloned; error when `required`, warning otherwise — `vat sync` draws the same line |
| `repo/not-a-repository` | error | a directory shadowing a governed repository |
| `repo/remote-mismatch` | error | fetching from somewhere the manifest does not name |
| `repo/remote-missing` | warn | a repository that can never be fetched or pushed |
| `repo/default-branch-missing` | warn | a `develop` repository skipped by every update, silently |
| `repo/checks-missing` | warn | a changeset with nothing to verify |
| `harness/workspace-missing` | error | a workspace with no agent contract |
| `harness/workspace-drift` | error | a contract that no longer describes the workspace |
| `harness/workspace-oversized` | warn | a root file that truncates the contracts below it |
| `harness/repo-missing` | warn | a session opened in a repository with no contract |
| `harness/repo-drift` | warn | the same, out of date |
| `harness/adapter-drift` | warn | one role behaving differently per runtime |
| `harness/role-metadata` | warn | a role no runtime can advertise |
| `policy/trust-undeclared` | warn | a harness that cannot say which content is data |
| `brain/not-initialised` | warn | a declared knowledge repository with no records |
| `brain/generated-drift` | error | a hand-edited projection contradicting the records |
| `brain/source-revision-drift` | warn | a claim whose evidence moved on months ago |
| `brain/source-repo-unknown` | warn | a claim pointing at a repository that is not governed |
| `changeset/invalid` | error | a completion record that cannot be acted on |
| `changeset/open-too-long` | warn | repositories mid-contract-change with no closing evidence |

`--fix` repairs only what can be repaired without judgement: it regenerates what
is generated and re-excludes what should have been excluded. It never edits a
fact, a decision, or a working tree.

`--offline` skips the rules that resolve a git revision.

---

## vat exec

```
vat exec [--group <g>] [--role <r>] [--only <names>] [--checks]
         [--jobs n] [--timeout <duration>] [--keep-going=false] -- <command>
```

Runs a command in every selected repository, in parallel, with per-repository
results. A failure in one is never hidden by success in another.

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
vat repo list    [--group <g>] [--role <r>] [--archived]
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
vat repo rename  <old> <new> [--keep-path]
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
vat harness role new <name> [--writes <repos>] [--reads <repos>] [--model <m>]
                            [--effort <e>] [--description <text>] [--runtimes <list>]
```

`render` writes the generated region of every `AGENTS.md` and every runtime
adapter. Content outside a generated region is never touched.

`role new` creates a runtime-neutral role under `.agents/roles/`. A role
defaults to **read-only**: write access is granted by naming the repositories it
may change, because a role that can edit anything is a role whose boundary
cannot be reviewed.

A role name may hold only letters, digits, `-`, and `_`. It is pasted into a
path in `.agents/roles/` and in every runtime's adapter directory, and adapters
are written whole rather than into a marked region.

---

## vat brain

```
vat brain init      [directory]
vat brain new       <goal|gap|decision|memory> --title "..." [--claim <kind>]
                    [--owner <repo>] [--axis <a>] [--refs <ids>] [--id <id>]
vat brain build
vat brain check
vat brain query     <terms...> [--all] [--limit n]
vat brain review    [--overdue] [--limit n]
vat brain sweep     [--apply]
vat brain promote   <id> [--reviewer <name>]
vat brain supersede <old-id> <new-id>
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

`adopt` points the workspace at an existing knowledge repository and reports
which records do not yet meet the schema. Nothing is rewritten.

See [BRAIN.md](BRAIN.md) for the record schema and the full lifecycle.

---

## vat changeset

```
vat changeset new       "<objective>" [--repos a,b] [--non-goal "..."]
                                      [--contract "..."] [--decision <ids>]
vat changeset add       <id> <repo>...
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
one claim the record exists to make.

`verify` runs each repository's canonical checks and records the outcome against
the exact revision it ran on. It refuses on a dirty working tree, because
results recorded against a revision that does not describe what was tested are
worse than none, and refuses on a changeset that is already closed.

`close` requires `--acceptance`, and it must describe something end to end.

`show` prints the objective, the status, the acceptance, and the notes — which
is where `abandon --reason` is kept, because why work stopped is the whole value
of an abandoned record.

`list --open` narrows to unfinished work in both renderings, so a CI job asking
for it in `--json` gets the same answer the table shows.

`undo-plan` prints the commands that would return every repository to its start
point, in reverse enrolment order. `vat` prints and never runs it.

See [CHANGESETS.md](CHANGESETS.md).

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
