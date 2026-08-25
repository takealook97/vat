# Security policy

## Supported versions

The latest release is supported. Fixes are not backported.

## Reporting a vulnerability

Please report privately through
[GitHub Security Advisories](https://github.com/takealook97/vat/security/advisories/new).
Do not open a public issue.

Include what an attacker could achieve, the steps to reproduce it, and the
version you tested. You can expect an acknowledgement within a week.

## What is in scope

`vat` runs on a developer machine and holds no service of its own, so the
interesting surface is narrow but real:

- **Command execution.** `vat exec` and `vat changeset verify` run commands from
  `vat.yaml`. A repository that can modify a workspace manifest can run code on
  anyone who then runs those commands. Treat `vat.yaml` with the same care as a
  CI configuration.
- **Path traversal.** A manifest entry must stay inside the workspace. A `path:`
  that escapes it is a bug — please report it.
- **Secret disclosure.** `vat` must never print a credential value. Findings
  about the credential repository are limited to file existence, whether a file
  looks encrypted, and how long it has gone unchanged. Any output that reveals
  more is a bug.
- **Working-tree destruction.** `vat` must never discard uncommitted work,
  unpushed commits, or stashes without an explicit flag and, for deletion, a
  prompt. A path that loses work silently is a security issue, not merely a bug.

## What is deliberately not defended

- **A workspace you do not trust.** Cloning someone else's workspace and running
  `vat exec` is equivalent to running their shell script. `vat` does not sandbox
  it.
- **Content an agent reads.** `vat` declares a trust boundary and renders it into
  every generated contract, but it cannot enforce what a model does with text.
  The boundary is a control you configure, not a guarantee `vat` provides.

## Hardening notes

- Keep `policy.gates.*` at `manual` unless a human reviews every automated
  action of that kind.
- Declare `policy.trust.untrusted` explicitly. `vat lint` warns when it is empty,
  because a harness that never names untrusted sources cannot tell an agent which
  text is data.
- Run `vat doctor --secret-max-age 90` on a schedule. Recovery procedures are
  usually documented; rotation almost never is, and a long-lived unrotated
  secret is the quiet half of a credential system.
