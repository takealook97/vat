# Contributing to vat

Thank you for considering it. This document is short because most of what
matters is enforced by `make check` rather than by convention.

## Before you start

For anything larger than a bug fix, open an issue or a discussion first. `vat`
refuses features that only *might* be useful, and it is kinder to say so before
you have written the code than after.

The strongest proposals name a failure that already happened to you.

## Getting set up

```bash
git clone https://github.com/takealook97/vat.git
cd vat
make check
```

You need Go 1.25 or newer and `git`. Nothing else. The test suite creates real
git repositories in temporary directories, so `git` must have a user identity
configured.

```bash
make help      # every available target
make test      # the suite, with the race detector
make cover     # coverage per package
make lint      # golangci-lint, if installed
```

## Commit convention

```
{category}: {description}
```

The category is one of:

| | |
| --- | --- |
| `feat` | new capability |
| `fix` | corrected behaviour |
| `refactor` | behaviour preserved, structure changed |
| `docs` | documentation only |
| `test` | tests only |
| `chore` | dependencies, tooling, housekeeping |
| `perf` | measurably faster |
| `ci` | build and release pipeline |

Write the description in the imperative, lower case, with no trailing full
stop. Explain *why* in the body when the reason is not obvious from the diff.

```
feat: demote a claim whose observation aged past the policy window

Without this the review queue only grows, and a knowledge repository quietly
becomes a confident liar: a claim recorded once stays active forever and an
agent quotes a two-year-old observation as the present.
```

Commit messages, code, comments, and documentation are all in English.

## What a change needs

- **A test that fails without it.** New behaviour, and every bug fix, needs one.
  A bug fix without a regression test is an invitation for the bug to return.
- **`make check` passing.** Formatting, vet, the race-enabled suite, and a
  build. This is the canonical proof; nothing else counts as one.
- **Doc comments on exported names.** Every package has a package comment
  saying what it owns.
- **Comments that explain why.** The code already says what it does. A comment
  earns its place by recording the reasoning, the trade-off, or the failure
  that a future reader would otherwise re-discover.

## Design rules this project holds itself to

These are not style preferences. A pull request that breaks one of them will be
asked to change, however good it otherwise is.

1. **A rule that cannot be checked is not added.** If a new convention cannot
   become a `vat lint` rule or a `vat doctor` finding, it belongs in a document
   somewhere else.
2. **Never silently modify a working tree.** No stash, no reset, no checkout, no
   automatic merge. A state `vat` cannot resolve safely is reported and left
   alone.
3. **Never rewrite a remote to make something work.** A mismatch is a
   supply-chain signal, not a convenience problem.
4. **Never print a secret.** Findings about credentials are limited to
   existence, permissions, and age.
5. **Diagnosis and repair are separate commands.** `doctor` judges; it never
   fixes. `lint --fix` only regenerates what is generated.
6. **Report every finding at once.** These commands are run in a loop while
   cleaning a repository, and one finding per run makes that loop unbearable.
7. **Immutability by default.** Functions return new values rather than mutating
   arguments; `WithRepo` and `WithParticipant` are the pattern to follow.
8. **Dependencies stay near zero.** The current count is one. Adding a second
   needs a strong argument in the pull request.

## Package layout

| | |
| --- | --- |
| `cmd/vat` | entry point, signal handling |
| `internal/cli` | command tree, flags, output |
| `internal/manifest` | `vat.yaml` schema, validation, immutable updates |
| `internal/workspace` | root discovery, paths, `.gitignore` region |
| `internal/gitx` | the git command line |
| `internal/syncx` | the update state machine |
| `internal/doctor` | environment diagnosis |
| `internal/lint` | workspace rules and safe repair |
| `internal/harness` | generated regions, roles, runtime adapters |
| `internal/brain` | records, projections, lifecycle, query |
| `internal/changeset` | multi-repository completion records |
| `internal/evidence` | worker contracts |
| `internal/metrics` | measurement and the local ledger |
| `internal/fit` | the adoption break-even advisor |
| `internal/runner` | command execution across repositories |
| `internal/frontmatter` | YAML headers in Markdown |
| `internal/fsx` | atomic writes |
| `internal/ui` | terminal output |
| `internal/version` | build metadata |

`internal/brain` deliberately imports neither `manifest` nor `gitx`. A workspace
that never adopts the knowledge layer pays nothing for it, and the seam stays
clean enough that the package could be lifted out entirely. Please keep it that
way.

## Test style

Arrange, act, assert — with those comments when the sections are not obvious.
Name a test after the behaviour it pins down, not the function it calls:

```go
func TestADirtyWorkingTreeIsReportedAndLeftUntouched(t *testing.T)
func TestPromoteRefusesAClaimWithNoProvenance(t *testing.T)
```

When a test protects against a specific disaster, say so in a comment. The next
person to read it needs to know what it is defending.

## Reporting a security issue

Do not open a public issue. See [SECURITY.md](SECURITY.md).

## Code of conduct

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
