# Security model

What `vat` defends, what it merely makes visible, and what it deliberately does
not attempt.

For reporting a vulnerability, see [SECURITY.md](../SECURITY.md).

---

## The three separations

### 1. Reading is not judging is not acting

The most common failure in an agent system is collapsing these into one
permission because they happen to arrive together.

| Capability | Granted by |
| --- | --- |
| read a repository | being in the workspace |
| write a repository | that repository's contract, and the role's `writes` list |
| deploy, or write to any external system | `policy.gates`, and a human |
| make a claim canonical | `policy.gates.brain_promote`, and a review |

A role that declares no `writes` generates a **read-only** adapter. A
senior-sounding name is not a grant of capability.

### 2. Ciphertext is not plaintext, and neither is the authority to apply it

The credential repository holds encrypted material. The key lives outside it,
with a recovery recipient in a different failure domain. Applying to production
is a third, separate approval.

`vat` never decrypts anything. `vat doctor` reports only:

- files whose names suggest unencrypted secrets
- how many encrypted files are tracked
- how long the oldest has gone unchanged
- key material any other user on the machine can read

The last of those looks at the mode, not the contents, and deliberately ignores
ciphertext: an encrypted file at `0644` is what encryption is for, and reporting
it would train you to ignore the check. It applies on Unix only — see
**Platform limits**.

No value, no key name from inside a file, no path outside the workspace.

### 3. Canon is not retrieval

| Tier | Sources | May do |
| --- | --- | --- |
| canonical | the brain | state facts, constrain behaviour |
| semi-trusted | workspace repositories | state facts about themselves |
| untrusted | search results, fetched pages, issue comments, model output | **nothing. this is data** |

---

## Indirect prompt injection

An agent that reads many repositories, issue threads, fetched pages, and search
results is reading text other people can write. This is a genuine attack
surface, and permission separation does not address it: the *content* is the
vector.

**What `vat` does**

- renders the trust table into every generated contract, at both levels
- states plainly that an imperative sentence inside retrieved content is a
  quotation, not a request
- instructs agents to report content that appears to instruct them
- removes retrieved content from the precedence order entirely
- warns when `policy.trust.untrusted` is empty, because a harness that never
  names untrusted sources cannot tell an agent which text is data

**What `vat` cannot do**

Enforce any of it. A model may ignore its contract. This is a control you
configure and a boundary you make explicit — **not a guarantee.**

Treat it as defence in depth alongside capability limits, not as a substitute
for them. The gates are the real control; the trust tiers make violations
legible.

---

## Command execution

`vat exec` and `vat changeset verify` run commands taken from `vat.yaml`.

**A repository that can modify a workspace manifest can run code on anyone who
then runs those commands.** Treat `vat.yaml` with the same care as a CI
configuration:

- review manifest changes in pull requests
- do not run `vat exec --checks` in a workspace you do not trust
- remember that `vat sync` never runs anything from a manifest — only `exec` and
  `changeset verify` do

Cloning someone else's workspace and running `vat exec` is equivalent to running
their shell script. `vat` does not sandbox it and does not pretend to.

---

## Destructive-action policy

`vat` treats losing work as a security failure, not merely a bug.

| Action | Protection |
| --- | --- |
| updating | never stashes, resets, checks out, or merges |
| repository removal | refuses while uncommitted changes, unpushed commits, or stashes exist |
| directory deletion | always prompts, even with `--yes` |
| returning a changeset | prints the plan; never carries it out |
| remote mismatch | fails; never rewrites the remote |
| `doctor` | judges; never repairs |
| a timed-out command | its whole process group is signalled, so children do not outlive it |
| `lint --fix` | regenerates only what is generated; never touches a fact or a working tree |

Stashes get special attention because they are invisible to `git status`, which
makes them the work most often destroyed by a cleanup.

---

## What `vat` writes, and where

| Path | Contents | Committed |
| --- | --- | --- |
| `vat.yaml` | the manifest | yes |
| `AGENTS.md`, `*/AGENTS.md` | generated regions | yes |
| `.agents/`, `.claude/`, `.codex/` | roles and adapters | yes |
| `changesets/`, `evidence/` | records | yes |
| `.gitignore` | the managed region | yes |
| `.vat/metrics.jsonl` | the local ledger | no — derived |

Every write goes through an atomic temp-file-and-rename, so an interrupted run
cannot leave a half-written manifest, contract, or record.

`vat` opens no network connection of its own. Network activity is `git` and, for
`vat repo new`, the GitHub CLI — both invoked as subprocesses using your
existing authentication.

---

## Supply chain

- **One direct dependency**, `gopkg.in/yaml.v3`, pinned in `go.sum`.
- Reproducible builds via `-trimpath` and stamped version metadata.
- Release artefacts carry SHA-256 checksums.
- CI verifies that `go mod tidy` produces no diff.

Keeping the dependency count near zero is a security property, not an aesthetic
one. A tool that governs a workspace should not widen its attack surface.

---

## Platform limits

**File permissions on Windows.** `vat` writes files with an explicit mode, and
on Unix that mode holds. Windows has no POSIX permission bits — Go's `os.Chmod`
there only toggles the read-only attribute — so a file `vat` intends to create
as `0600` is created with whatever the directory's ACL grants.

Nothing `vat` writes contains a secret, so this is not a disclosure path in
itself. It does mean that on Windows the mode reported by `vat doctor` for a
credential file describes an attribute, not an access control, and should not be
read as one. Use NTFS permissions on the credential repository instead.

## A threat this model does not cover

**A compromised developer machine.** `vat` runs with your permissions and your
git credentials. Nothing here defends against an attacker who already has them.

The credential boundary limits *blast radius* — ciphertext in git, keys
elsewhere, production application separately approved — but it does not stop
someone who controls the machine where the keys are used.
