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
4. `git push origin main --follow-tags`.
5. The workflow then builds the five targets in its `TARGETS` list, writes a
   CycloneDX SBOM per target, generates the shell completions, packages and
   checksums the archives, attests them **and** `checksums.txt`, and publishes
   with generated release notes.
6. Verify the release page carries, for every target, an archive and a
   `.cdx.json`, plus one `checksums.txt` and a provenance attestation.

## When it must stop

**A published tag is frozen. Never move one.** The Go module proxy caches a
version the first time anyone fetches it, so re-pointing `vX.Y.Z` leaves two
different trees answering to one version while the proxy keeps serving the
first. A release that went out wrong goes out again as the next patch version.

Stop before pushing if `make check` is red, if the working tree is dirty, or if
the version cannot be justified from the diff. None of those is recoverable
afterwards.

Nothing in this repository updates the Homebrew tap. If a release needs to reach
`brew install`, that happens outside this repository and is not this skill's to
assume.
