---
name: cut-a-release
description: Take vat from a green main to a signed, published release.
---

# Cut a release

## When to use this

A version is going out. Not for a local multi-platform build — that is
`make release-snapshot`, which publishes nothing.

## Inputs

- `main`, clean, with `make check` passing
- `CHANGELOG.md`
- `.github/workflows/release.yml`, which is what actually publishes

## Before anything

Run `git fetch --tags`. Every version this binary reports comes from
`git describe`, so a checkout missing the last tag builds a binary that names an
older release: on the v0.5.1 commit with only v0.4.2 fetched, `make check` built
`vat v0.4.2-7-gaae7c2b`. That was cosmetic until `requires.vat` began comparing
versions, and it is not any more — a workspace pinned `>=0.5.0` refuses that
binary, built from the exact commit that satisfies it.

Run `make check`. The tag is the trigger and there is no gate after it: a tag
pushed on a red tree publishes a red release.

Decide the version from what changed, under semantic versioning. `vat` is a
command-line contract, so a changed exit code, a removed flag, or a changed
on-disk format is a breaking change even when no Go symbol moved.

## Steps

1. Move the entries under `## [Unreleased]` in `CHANGELOG.md` beneath a new
   `## [X.Y.Z] - YYYY-MM-DD` heading, and leave `## [Unreleased]` in place and
   empty above it. The heading carries no `v`; the tag does.
2. Commit: `chore: release vX.Y.Z`.
3. Tag: `git tag vX.Y.Z`. The workflow triggers on `v*` and on nothing else.
4. `git push origin main` **and then** `git push origin vX.Y.Z`.

   Push the tag by name. `--follow-tags` pushes only *annotated* tags, and every
   tag this repository has is lightweight, so it skipped the tag in silence:
   main moved, the workflow never triggered, and the push reported success.
   Confirm with `git ls-remote --tags origin | grep vX.Y.Z` before believing the
   release happened.
5. The workflow then builds the five targets in its `TARGETS` list, writes a
   CycloneDX SBOM per target, generates the shell completions, packages and
   checksums the archives, attests them **and** `checksums.txt`, and publishes
   with generated release notes.
6. Verify the release page carries, for every target, an archive and a
   `.cdx.json`, plus one `checksums.txt` and a provenance attestation.

## When a published release turns out to be defective

**A published tag is frozen. Never move one, and never delete one.**
`proxy.golang.org` caches a version's content the first time anyone fetches it
and offers no deletion, so `go install module@vX.Y.Z` will serve that code
forever. Re-pointing the tag leaves two different trees answering to one version
while the proxy keeps serving the first, and deleting the tag on the forge does
not reach the proxy at all.

The fix is two things, not one:

1. Ship the correction as the **next** version. Never as the same one.
2. **Retract the defective version in `go.mod`**, with a comment saying what was
   wrong with it. Retraction is what keeps the toolchain from offering it:
   `go get` refuses a retracted version, `go list -m -versions` hides it, and
   `@latest` skips past it.

```
retract (
    // What was wrong with it, in enough detail to judge the risk.
    v0.1.5
    // A contiguous range, when several versions share one defect.
    [v0.1.2, v0.1.4]
)
```

A retraction only takes effect once it is published in a version **above** the
ones it names, so the retraction and the fix ship together in the new tag.

Record it in `CHANGELOG.md` too. The `retract` block is read by the toolchain
and by nobody else; a person deciding whether they are running something unsafe
reads the release notes.

Retract for a defect that matters to somebody running it — a disclosed
credential, a write outside the directory vat governs, a wrong version stamp.
Not for a typo.

## When it must stop

Stop before pushing if `make check` is red, if the working tree is dirty, or if
the version cannot be justified from the diff. Stop after pushing, too, until
`git ls-remote --tags origin` shows the tag: a push that moved main and left the
tag behind reports success and publishes nothing. None of those is recoverable
afterwards, because the push is the publication.

Deciding that a defect is severe enough to retract is a judgement about other
people's installations. Say what the defect is and let a human make it.

Nothing in this repository updates the Homebrew tap. If a release needs to reach
`brew install`, that happens outside this repository and is not this skill's to
assume.
