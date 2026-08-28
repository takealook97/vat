<div align="center">

<img src="docs/assets/vat.png" alt="vat" width="200">

# vat

**Git records each repository. `vat` records what happened across them.**

*Your repositories are the body, `vat` the vessel, `vat brain` the memory
suspended inside it — where nothing counts as a fact until someone records when
they last checked.*

[![CI](https://github.com/takealook97/vat/actions/workflows/ci.yml/badge.svg)](https://github.com/takealook97/vat/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/takealook97/vat.svg)](https://pkg.go.dev/github.com/takealook97/vat)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/takealook97/vat?sort=semver)](https://github.com/takealook97/vat/releases)

<img src="docs/assets/demo.svg" alt="vat init, status, sync, and lint run against a three-repository workspace" width="820">

</div>

---

**Two ways in.**

**Several repositories, worked in together.** One manifest, one place to run
from, and a record of which revisions were verified together — the problem
below. → keep reading.

**One repository, with an agent working in it.** `vat harness` keeps one role
body and one procedure body, generates each runtime's file from them, and fails
the build the day a copy diverges. → [The agent harness](#the-agent-harness)

`vat fit` will tell you which of the five layers you do not need yet.

---

## The problem

```text
~/acme/
├── payments/   ← git repo
├── console/    ← git repo
└── docs/       ← git repo

$ cd payments
$ claude
```

The moment a session opens inside `payments`, that becomes the agent's whole
world. You are working on `acme`; it thinks it is working on `payments`, and
everything on the other side of `../` is an external path it has to be told
about, one relative reference at a time. `vat`'s answer is to start one level
up:

```text
$ cd ~/acme
$ vat init --adopt
$ codex
```

Now the workspace is the project. The repositories are its parts. Three
specific things about that arrangement are otherwise written down, assumed, or
remembered — and nothing checks them:

- **What crossed the boundary.** An API change shipped across three
  repositories. Each `git log` is complete and none of them is the answer:
  nobody can say which three revisions were verified *together*, or how to get
  back — and `for r in */; do git -C "$r" pull; done` reports success while one
  repository silently stashed your work.
- **The contract your agents read.** A session at the parent directory can see
  every repository, which is the point and also the hazard — reading them all
  is not permission to write to any of them. Meanwhile the same role is defined
  in `.claude/agents/` and `.codex/agents/`, and nobody diffs a prompt.
- **The facts both of them rely on.** A doc says the payments service "has
  retry-safe ordering." It was true when someone wrote it. Nobody has checked
  since, and it is quoted as current fact weekly, by people and by agents that
  cannot tell that nobody has.

`vat` turns each one into a check that runs:

```console
$ vat lint
FAIL  workspace/gitignore-drift · console                 1 governed repository is not excluded by .gitignore; a workspace commit would swallow it
      → vat lint --fix
FAIL  harness/adapter-drift · .codex/agents/planner.toml  runtime adapter no longer matches its role definition in .agents/roles
      → vat harness render
WARN  brain/source-revision-drift · G-0014                payments has moved 47 commits since this was observed at 3f9a1c2; re-check, do not assume it broke
WARN  changeset/open-too-long · CS-0007                   open for 31 days, past the 14-day limit; repositories are mid-contract-change with no closing evidence

Result
FAIL  lint                      2 errors, 2 warnings across 40 rules
2 of these can be repaired with `vat lint --fix`.
```

---

## Who this is for

**You work across several repositories, and so do your agents.** One manifest,
one place to run from, a written boundary per repository so a session at the
parent directory cannot edit its way across one by accident, and a record of
which revisions were verified together.

**You use coding agents on a codebase you care about.** One repository is
enough. `vat harness` generates the per-runtime adapters from one role body so
Claude Code and Codex cannot drift apart. If you already have `.claude/agents/`
or `.claude/skills/`, `vat harness adopt` moves what you wrote under that
contract in one command.

**Your team keeps re-deriving what it already decided.** `vat brain` records a
fact with the revision it was read from, and demotes it when nobody re-checks
it. Not a wiki, not a vector store: a claim that expires.

Run `vat fit` and it will tell you which of those you do *not* need yet.

## What this is not

Being clear about the neighbours is more useful than pretending there are none.

| If what you want is | Reach for |
| --- | --- |
| parallel `git pull` across many repositories | a shell loop, [`mu-repo`](https://github.com/fabioz/mu-repo), [`meta`](https://github.com/mateodelnorte/meta), [`gita`](https://github.com/nosarthur/gita) |
| one build graph and task runner over many projects | [Nx](https://nx.dev), [Turborepo](https://turbo.build), [Bazel](https://bazel.build) |
| a place to file architecture decision records | [`adr-tools`](https://github.com/npryce/adr-tools), [Log4brains](https://github.com/thomvaill/log4brains) |
| semantic recall over your documents for an agent | a retrieval layer — and point it at `vat brain`, which decides what is canonical |
| portable agent memory, so what one tool learned another can read | one of the several markdown-and-folders memory formats now converging on that — `vat brain` is readable the same way, and answers a different question, below |
| agent instructions for one tool | that tool's own directory, until there is a second runtime or a second copy — the case below |
| **a history of what crossed between repositories, and rules that fail loudly when reality moves away from them** | `vat` |

**On agent instructions.** The format is barely the problem — `.claude/agents/`
and `.codex/` are both Markdown with front matter. The problem is the second
copy: the moment a procedure exists in two runtimes, the one an agent happens
to load decides what it does, and neither looks less authoritative. See
[The agent harness](#the-agent-harness).

**On agent memory.** Taking memory out of the tool and writing it as folders
and markdown is easy; the hard half is **who is allowed to write a fact, and
how does a reader know it is still true.** Portability is the format vat
shares with the rest; trust is the contract it adds. See
[`vat brain`](#vat-brain--memory-that-expires-on-purpose).

`vat` is not a build system, and it will not make your agent smarter. It gives
the agent a boundary it cannot cross by accident, and leaves behind an account
of what actually happened across your repositories.

## Install

```bash
# Homebrew — macOS and Linux
brew install takealook97/tap/vat

# Go 1.25+
go install github.com/takealook97/vat/cmd/vat@latest
```

Or download an archive for macOS, Linux, or Windows from
[Releases](https://github.com/takealook97/vat/releases). Every release also
publishes SHA-256 checksums, a CycloneDX SBOM per platform, and a signed
build-provenance attestation:

```bash
gh attestation verify vat_darwin_arm64.tar.gz --repo takealook97/vat
```

A checksum says an archive matches a list published beside it by whoever
published the archive. The attestation is a signed statement of which commit
and which workflow run built it, and the signature is not the publisher's to
forge.

One binary. One dependency (`gopkg.in/yaml.v3`). No runtime, no daemon, no
config server. `vat` shells out to your `git`, so your credential helpers,
hooks, and host config apply unchanged.

---

## 60 seconds

```console
$ cd ~/work                    # a folder with several repos already cloned
$ vat init --adopt --name acme
OK    vat.yaml                  4 repositories enrolled
OK    .gitignore                governed repositories excluded from the root history
OK    .agents/skills/before-cross-repo-work/SKILL.md  seeded
OK    .agents/skills/consult-the-brain-first/SKILL.md  seeded
OK    .claude/skills/before-cross-repo-work/SKILL.md  generated
OK    .claude/skills/consult-the-brain-first/SKILL.md  generated
OK    AGENTS.md                 generated
OK    CLAUDE.md                 generated
OK    brain/AGENTS.md           generated
OK    console/AGENTS.md         generated
OK    docs/AGENTS.md            generated
OK    payments/AGENTS.md        generated
INFO  brain                     brain · https://github.com/acme/brain.git
INFO  console                   product · https://github.com/acme/console.git
INFO  docs                      docs · https://github.com/acme/docs.git
INFO  payments                  product · https://github.com/acme/payments.git

Next
  vat status        see where every repository stands
  vat doctor        judge the environment
  vat fit           decide which layers are worth adopting yet

$ vat status
REPOSITORY  BRANCH   REV      TREE   VS ORIGIN  NOTE
brain       main     9af189c  clean  =
console     feature  772418c  dirty  +2         not on main
docs        master   3bf4bf2  clean  -4
payments    main     3bebc3c  clean  =

4 repositories · 1 dirty · 1 ahead · 1 behind · workspace acme
Run `vat sync` to fast-forward what can be advanced safely.

$ vat sync
REPOSITORY  STATE    BRANCH   REV      DETAIL
brain       CURRENT  main     9af189c
console     DIRTY    feature  772418c  uncommitted changes to tracked files; nothing advanced
docs        UPDATED  master   1e5b9a0
payments    CURRENT  main     3bebc3c

1 advanced · 2 already current · 1 left alone on purpose · 0 need attention
```

Notice what `sync` did **not** do: it did not stash `console`'s work, did not
check `docs` out to `main`, and did not report success on your behalf.

---

## A colleague joins

The manifest is committed, so the workspace is reproducible from it.

```console
$ git clone git@github.com:acme/workspace.git ~/work && cd ~/work
$ vat sync
REPOSITORY  STATE   BRANCH  REV      DETAIL
brain       CLONED  main    9af189c
console     CLONED  main    772418c
docs        CLONED  main    1e5b9a0
payments    CLONED  main    3bebc3c

4 advanced · 0 already current · 0 left alone on purpose · 0 need attention
```

They now have the same repositories, on the same branches, with the same
contracts an agent will read — including the ones written for a runtime they do
not use, because the adapters are generated from one body rather than copied.
`vat doctor` tells them what their machine is missing before they find out the
slow way.

---

## One change, three repositories

The commands above are the floor. `vat evidence` scopes the work,
`vat changeset` captures the way back and the revisions verified together,
`vat ship` tells verified apart from landed, and `vat brain` records why —
one contract change, from the session that starts it to the session that
inherits it. → [The full walkthrough](docs/WALKTHROUGH.md)

---

## Nothing here is adopted by default

Most methodology tools assume you want all of it. This one starts by telling
you what to skip.

```console
$ vat fit --contracts 1 --people 1
OK    workspace                 adopt — 4 repositories: knowing what to clone, and what state each is in, has stopped being memorable
SKIP  harness                   not yet — nothing here says agents work in this code; pass --agent-sessions if any do
SKIP  changesets                not yet — nothing here says an interface crosses a repository boundary; count them with --contracts
SKIP  brain                     not yet — nothing here says a decision has been lost, or that more than one person works across these repositories
SKIP  credential                not yet — no repository here is declared as holding secrets

Conclusion
Adopt workspace now. Leave the rest until its threshold is met; adopting a layer
early costs ceremony and buys nothing.
```

| Layer | Adopt when | What it gives you |
| --- | --- | --- |
| **workspace** | 3+ repositories worked in together | `init` `status` `sync` `doctor` `exec` |
| **harness** | coding agents work in this code at all | generated `AGENTS.md`, one role body → N runtime adapters, drift-checked |
| **changesets** | 2+ interfaces cross a repo boundary | the revision bundle that was verified together, and the way back |
| **brain** | a decision was already lost, agents work here weekly, or 2+ people across 4+ repos | reviewed facts with provenance and an expiry |
| **credential** | secrets live in 2+ places | encrypted canon, rotation age tracking |

---

## `vat brain` — memory that expires on purpose

Every knowledge base decays the same way: someone writes "the collector runs
hourly," it is true, and it stays in the file for three years after it stopped
being true. `vat brain` makes that structurally impossible by attaching
provenance to every claim about the present:

```yaml
---
id: G-0014
status: active
claim_kind: current-state
owned_by: payments
source_ref: payments@3f9a1c2e8b74:docs/ORDERING.md
observed_at: 2026-05-02
revalidate_on: source-revision-change
---
```

A claim nobody re-checks inside the policy window is demoted, not deleted:

```console
$ vat brain sweep --apply
OK    G-0014    active → stale (observed 115 days ago, past the 90-day window)
```

It is not false. It is *unverified*, and it stops being citable until a human
looks again — the difference between a knowledge repository that is useful in
year three and one that has become a confident liar.

| | |
| --- | --- |
| `provisional` | recorded, never reviewed — not citable |
| `active` | reviewed and citable |
| `stale` | was true when observed; nobody has re-checked it |
| `quarantined` | suspected wrong; withheld until resolved |
| `superseded` | replaced, kept for its reasoning |
| `revoked` | withdrawn, kept as a tombstone |

Decisions are replaced, never rewritten — `vat brain supersede` updates both
ends of the chain, and `vat brain check` fails if a link points only one way.
And drift is a signal, not a verdict: when the owning repository moves, `vat
lint` says so without concluding the claim is false.

**Above the atomic records, one more layer.** `vat brain init` also scaffolds
`GOAL.md`, `STATUS.md`, `ROADMAP.md`, `DECISIONS.md`, and `MEMORY.md` —
maintained summaries, no detail, each linked from the generated index the
moment it exists. `vat` writes none of their prose and will not judge whether
one is *right*: `brain/view-stale` only reports a view the records have moved
past for longer than the review window. Judging correctness is not a tool's
job. Noticing neglect is.

That generated index stays bounded on the same principle: each section of
`CURRENT.md` keeps the fifteen records the rest of the repository cites most,
and says how many more live in the file beside it. An index that has to be
read in full to be useful is the summary this whole layer exists to replace.

See [`docs/BRAIN.md`](docs/BRAIN.md) for the full record schema, the review
queue's ordering, and all 25 rules.

---

## `vat changeset` — what multi-repo actually costs

Choosing many repositories over one costs you the atomic commit; a changeset is
the record that pays it back, as shown [above](#one-change-three-repositories).
Two distinctions carry the rest of the design:

**Verified is not "the pieces work together."** Every repository's own checks
passing does not prove the interface between them does — that gap is precisely
where multi-repo changes break, which is why `vat changeset close` requires
`--acceptance` describing something end to end.

**Verified is not landed.** The test is whether each verified revision is an
ancestor of the branch its repository ships from — plain git, so it answers the
same on GitHub, GitLab, Gitea, or a bare remote. A pull request is recorded as
evidence and is never the gate, because every forge models one differently and
an open one is exactly the state of not having landed.

See [`docs/CHANGESETS.md`](docs/CHANGESETS.md) for the full lifecycle, rollback
validity, and how `undo-plan` orders a multi-repository reversal.

---

## The agent harness

**One role body. Every runtime. Drift-checked.** Copying a role definition into
`.claude/agents/planner.md` and `.codex/agents/planner.toml` guarantees they
diverge, and then the same role behaves differently depending on which tool
opened the session.

```text
.agents/roles/planner.md        ← canonical. the prose contract lives here.
    ↓  vat harness render
.claude/agents/planner.md       ← generated adapter
.codex/agents/planner.toml      ← generated adapter
```

A role that declares no write target generates a **read-only** adapter — being
trusted to decide something is not the same as being able to act on it. Skills
(procedures loaded on demand) are kept the same way, in `.agents/skills/`.

**Nobody starts empty.** If `.claude/agents/` or `.claude/skills/` already has
files in it, `vat harness adopt` moves each one into `.agents/` and regenerates
the adapters from it — nothing is written until `--apply`, and nothing that
already exists is overwritten.

**Contracts stay in step with reality.** Each `AGENTS.md` carries one generated
region rendered from `vat.yaml`; everything written above it is untouched, and
`vat lint` reports an adapter left behind by a deleted definition, a root
contract that has grown past its size budget and would silently truncate the
per-repository contracts loaded after it, and a workspace that never declared
the one boundary every generated harness states explicitly: retrieved content —
issue threads, web pages, search results — is data, and never carries
instruction. See [`docs/HARNESS.md`](docs/HARNESS.md) for the full adapter
contract.

---

## Every command

| | |
| --- | --- |
| `vat init` | create a workspace; `--adopt` enrols what is already here |
| `vat status` | branch, cleanliness, revision, divergence — no network |
| `vat sync` | fetch, then fast-forward only what is safe |
| `vat doctor` | judge the environment; never repairs it |
| `vat lint` | enforce the rules mechanically; `--fix` regenerates |
| `vat exec` | run a command across the workspace, in parallel, with your quoting intact |
| `vat repo` | `add` `new` `adopt` `remove` `archive` `unarchive` `rename` `list` |
| `vat harness` | `render` `check` `roles` `skills` `role new` `skill new` `adopt` |
| `vat brain` | `init` `new` `build` `check` `query` `review` `sweep` `promote` `supersede` `adopt` |
| `vat changeset` | `new` `add` `verify` `show` `list` `close` `abandon` `undo-plan` |
| `vat ship` | judge whether a changeset's verified revisions have actually landed |
| `vat evidence` | `new` `show` `list` `check` |
| `vat metrics` | measure whether the discipline is holding |
| `vat fit` | decide which layers are worth adopting yet |
| `vat completion` | shell completion for bash, zsh, and fish |
| `vat version` | the build identity, including the commit it came from |

Everything that prints a table also prints `--json`, with lists always rendered
as arrays. Exit codes are part of the interface: `0` clean, `1` found errors,
`2` called wrong. Warnings alone exit `0`.

---

## Adding and removing repositories

The manifest, the `.gitignore` exclusion, and the generated harness always move
together — changing one without the others *is* the failure mode.

```console
$ vat repo new payments --group backend --private
OK    payments                  initialised with a starter harness
OK    payments                  pushed to https://github.com/acme/payments.git
OK    .gitignore                updated
OK    payments                  registered as product
OK    AGENTS.md                 regenerated
OK    payments/AGENTS.md        regenerated

$ vat repo remove legacy-api
FAIL  legacy-api                uncommitted changes in the working tree
FAIL  legacy-api                3 commits not on any remote
FAIL  legacy-api                1 stash entry

Refusing to remove legacy-api. Push or discard the work above, or pass --force.
```

Stashes are invisible to `git status`, which is exactly why they are the work
most often destroyed by a cleanup. `--delete` always prompts, even with `--yes`.

---

## Does it work? Ask it.

A checklist measures whether people performed rituals. These measure whether
the rituals produced the effect they were for.

```console
$ vat metrics
MEASURE           NOW  CHANGE  WHAT IT MEANS
lint errors       0    -2      rules the workspace declares but does not meet
review queue      14   +6      claims awaiting verification; sustained growth means knowledge is decaying
review overdue    3    +3      past the review window
citable records   41   -6      records usable as evidence right now
open changesets   2    +1      cross-repository work with no closing evidence
stale changesets  1    +1      open past the limit, so the revision bundle is drifting from what shipped
rework rate       11%          share of recorded checks that failed
```

The review queue is the one to watch. If it only grows, knowledge is being
written faster than it is verified, however diligently records are added.

---

## Design principles

1. A workspace is a control plane, not a code repository.
2. Repositories in one folder are still independent. `git -C` is the boundary.
3. Being able to read every repository is not permission to write to any of them.
4. The root contract is a map; each repository's contract is the working permit.
5. Updating never discards local work. Dirty, ahead, and diverged are respected.
6. A remote pointing somewhere else is a supply-chain signal, not a convenience.
7. A revision moving is a reason to re-check, not proof something broke.
8. Detail lives in atomic records; summaries are projections, and regenerable.
9. Decisions are superseded, never rewritten.
10. Secrets are ciphertext in git; the key and the authority to apply it are separate.
11. Retrieval is derived. It never outranks the canon, and it never carries instructions.
12. No automation collapses reading, judging, and acting into one permission.

Everything above is enforced by a command, or it is not in this tool.

---

## Documentation

| | |
| --- | --- |
| [Walkthrough](docs/WALKTHROUGH.md) | one contract change, end to end, across three repositories |
| [Methodology](docs/METHODOLOGY.md) | the full operating model this implements |
| [Spec](docs/SPEC.md) | the file formats, normatively — what another tool needs to read a workspace |
| [Commands](docs/COMMANDS.md) | every command, flag, and exit code |
| [Manifest](docs/MANIFEST.md) | `vat.yaml` reference |
| [Brain](docs/BRAIN.md) | record schema, lifecycle, and the promotion gate |
| [Harness](docs/HARNESS.md) | generated regions, roles, and runtime adapters |
| [Changesets](docs/CHANGESETS.md) | multi-repository atomicity |
| [Adoption](docs/ADOPTION.md) | a staged path in, and how to leave |
| [Security model](docs/SECURITY_MODEL.md) | trust tiers, gates, credential boundaries |
| [FAQ](docs/FAQ.md) | monorepo, submodules, and other fair objections |

---

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for
the commit convention (`{category}: {description}`), the review checklist, and
how to run the suite.

```bash
make check     # format, vet, race-enabled tests, build
```

## License

[MIT](LICENSE)
