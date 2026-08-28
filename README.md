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

An agent moved order cancellation into `payments` and `console` together.
Each repository's own `git log` is complete, and neither answers the one
question that actually matters six months later:

```console
$ vat changeset show CS-0001
CS-0001  Move order cancellation to v2
status: closed · opened 2026-05-02 · 3 days
acceptance: cancel-then-refund passes end to end

Repositories
REPOSITORY  RETURN TO  VERIFIED AT  CHECKS
payments    3f9a1c2e   a71c93d0     1 check passed
console     8b2e0d19   5c1f80ab     1 check passed
```

Two revisions, verified together, with the return point still on file. That
record is what `vat` adds on top of Git — not a replacement for it. →
[The full walkthrough](docs/WALKTHROUGH.md)

---

**Two ways in.**

**Several repositories, worked in together.** One manifest, one place to run
from, and a record of which revisions were verified together — the problem
below. → keep reading.

**One repository, with an agent working in it.** `vat harness` keeps one role
body and one procedure body, generates each runtime's file from them, and fails
the build the day a copy diverges. → [`docs/HARNESS.md`](docs/HARNESS.md)

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
| portable agent memory, so what one tool learned another can read | one of the several markdown-and-folders memory formats now converging on that — `vat brain` is readable the same way, and answers a different question |
| agent instructions for one tool | that tool's own directory, until there is a second runtime or a second copy |
| **a history of what crossed between repositories, and rules that fail loudly when reality moves away from them** | `vat` |

**On agent instructions.** The format is barely the problem — `.claude/agents/`
and `.codex/` are both Markdown with front matter. The problem is the second
copy: the moment a procedure exists in two runtimes, the one an agent happens
to load decides what it does, and neither looks less authoritative. See
[`docs/HARNESS.md`](docs/HARNESS.md).

**On agent memory.** Taking memory out of the tool and writing it as folders
and markdown is easy; the hard half is **who is allowed to write a fact, and
how does a reader know it is still true.** Portability is the format vat
shares with the rest; trust is the contract it adds. See
[`docs/BRAIN.md`](docs/BRAIN.md).

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
check `docs` out to `main`, and did not report success on your behalf. A
colleague who clones the (committed) workspace and runs `vat sync` gets the
same repositories, on the same branches, with the same contracts an agent will
read. → [The staged path in, and how to leave](docs/ADOPTION.md)

---

## Documentation

| | |
| --- | --- |
| [Walkthrough](docs/WALKTHROUGH.md) | one contract change, end to end, across three repositories |
| [Adoption](docs/ADOPTION.md) | a staged path in, and how to leave |
| [Methodology](docs/METHODOLOGY.md) | the full operating model this implements |
| [Commands](docs/COMMANDS.md) | every command, flag, and exit code |
| [Manifest](docs/MANIFEST.md) | `vat.yaml` reference |
| [Brain](docs/BRAIN.md) | record schema, lifecycle, and the promotion gate |
| [Harness](docs/HARNESS.md) | generated regions, roles, and runtime adapters |
| [Changesets](docs/CHANGESETS.md) | multi-repository atomicity |
| [Security model](docs/SECURITY_MODEL.md) | trust tiers, gates, credential boundaries |
| [Spec](docs/SPEC.md) | the file formats, normatively — what another tool needs to read a workspace |
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
