# Changesets

> The record that pays back what a multi-repo layout costs you.

---

## The cost

Choosing many repositories over one costs you the atomic commit. A cross-cutting
change becomes several commits with no relationship recorded anywhere.

Six weeks later:

- Which revisions were verified **together**? Nobody can say.
- What does reverting mean? Reconstruct it from three reflogs and hope.
- Did anyone check the pieces work together, or only that each repository's own
  tests passed?

Most teams pay this cost and never account for it. A changeset is the accounting.

---

## The lifecycle

```console
$ vat changeset new "Move order cancellation to v2" --repos payments,console
OK    CS-0001                   changesets/CS-0001.yaml
INFO  payments                  return point 3f9a1c2e
INFO  console                   return point 8b2e0d19
```

The return point is captured **now**, because after the change lands it can no
longer be observed.

```console
$ vat changeset verify CS-0001
OK    payments · make check     14.2s at a71c93d0
OK    console · pnpm test       31.8s at 5c1f80ab
OK    CS-0001                   every repository verified
```

Each repository's canonical checks from `vat.yaml` run, and each outcome is
recorded against the exact revision it ran on.

A dirty working tree **refuses to verify**. Checks that pass over uncommitted
changes prove nothing about the revision they would be filed under, and a record
that looks verified either way is worse than no record. Commit or stash first.

A changeset that is already closed also refuses: re-verifying would rewrite its
status while its closing evidence stayed in the file, leaving a record claiming
both at once.

Verifying is not shipping. The checks passing proves the combination works; it
says nothing about whether those revisions ever reached anybody else.

```console
$ vat ship CS-0001
OK    payments                  landed on origin/main
FAIL  console                   5c1f80ab is not on origin/main; verified but not landed

Result
FAIL  ship                      1 repository of 2 not landed
```

The test is whether each verified revision is an ancestor of the branch its
repository ships from — one git question, with the same answer on GitHub,
GitLab, Gitea, and a bare remote on a machine you own. vat pushes nothing and
merges nothing; it judges, and landing the work stays yours.

**A pull request is not the gate.** Every forge models one differently, so
gating on one would buy a dependency and tie the record to a vendor — and an
open pull request is precisely the state of *not* having landed. Supply one and
it is recorded as evidence beside the revision; the gate stays plain git.

```console
$ vat changeset close CS-0001 --acceptance "cancel-then-refund passes end to end"
OK    CS-0001                   closed
```

`close` refuses a changeset that has not landed, and points at `vat ship`.
`--force` still closes it — a real workspace sometimes has to — and
`changeset/closed-unlanded` then reports the gap, so a waiver stays visible
instead of becoming indistinguishable from a change that shipped.

`--acceptance` is required. Every repository's own checks passing is not the
same as the pieces working together, and that gap is exactly where
multi-repository changes break. Closing without naming the outcome loses the
only record of whether anyone checked.

---

## The record

```yaml
id: CS-0001
objective: Move order cancellation to v2
status: closed
opened_at: 2026-08-11
closed_at: 2026-08-14

non_goals:
  - changing refund timing
contracts:
  - POST /orders/{id}/cancel response schema
integration_acceptance: cancel-then-refund passes end to end
decisions: [D-0042]
approved_by: alex

repositories:
  - name: payments
    rollback_point: 3f9a1c2e8b7412
    revision: a71c93d0e5f218
    branch: main
    checks:
      - command: make check
        status: pass
        ran_at: 2026-08-14T09:12:44Z
        revision: a71c93d0e5f218
    landed_on: origin/main
    landed_at: 2026-08-14
    review: https://github.com/acme/payments/pull/318

  - name: console
    rollback_point: 8b2e0d19c4a077
    revision: 5c1f80ab3d9e61
    branch: main
    checks:
      - command: pnpm test
        status: pass
        ran_at: 2026-08-14T09:13:22Z
        revision: 5c1f80ab3d9e61
    landed_on: origin/main
    landed_at: 2026-08-14
```

## The control plane is a participant

```console
$ vat changeset new "Move order cancellation to v2" --repos .,payments,console
```

`.` is the workspace root. It holds `vat.yaml`, the roles, the skills, and the
generated regions every governed repository reads, so a contract change usually
starts there — and a record naming only the products leaves out the revision
that describes what the others were verified against. That is the revision a
rollback needs most.

It is verified by `workspace.checks` in `vat.yaml`, since the root is not in
`repos:` and cannot be, and `vat ship` judges its landing against
`origin/<default_branch>` like any other participant. Declaring no checks makes
it unverifiable rather than verified, which is the rule for everything else too.

One consequence worth knowing before you meet it: the record is written under
`changesets/` in that same root, so creating it dirties the tree its own checks
are about to describe. Commit the record, then verify — the discipline every
participant is held to, arriving one step earlier.

`revision` is what the checks ran on. `landed_on` is where that revision was
later observed. Keeping them apart is the whole point: one is "we proved it",
the other is "it reached everyone", and collapsing them is how a workspace ends
up certain about a change that never shipped. `review` is optional evidence —
recorded, never required, and never the gate.

Plain YAML, committed with the workspace. Safe to edit by hand; `vat lint`
checks it.

---

## When the way back stops existing

`rollback_point` is the one field in the record that cannot be reconstructed.
Everything else can be read back off git; the revision a repository stood at
before the change began cannot, which is why it is captured at enrolment.

A rewritten history takes it away — a force-push, a squashed branch, a pruned
object — and the record goes on asserting a return point in exactly the voice of
one that is there. `vat lint` reports that as
`changeset/rollback-point-missing`. There is no fix line worth printing: no
command recovers a revision the repository does not hold. Recover it from a
mirror or a bundle, or record why the way back was lost.

A repository that is not cloned on this machine is not reported. Its absence
says nothing about whether the history still holds the revision.

---

## The return plan

```console
$ vat changeset undo-plan CS-0001
# Return plan for CS-0001 - Move order cancellation to v2
# Reverse enrolment order. Review every line before acting on it.
git -C console  reset --hard 8b2e0d19c4a077   # was 5c1f80ab3d9e
git -C payments reset --hard 3f9a1c2e8b7412   # was a71c93d0e5f2
```

**Reverse enrolment order.** The contract owner is enrolled first, so it is
undone last — no window exists where a consumer still expects an interface that
is already gone.

**`vat` prints and never runs it.** Returning several repositories at once is
irreversible and depends on facts `vat` cannot see: what has been deployed, and
what others have already pulled. The plan is the part that is hard to
reconstruct; acting on it is your decision.

A repository with no recorded return point makes the plan refuse rather than
emit a half-plan.

---

## What lint enforces

| Rule | Prevents |
| --- | --- |
| a repository with no `rollback_point` | a change that cannot be undone |
| the same repository enrolled twice | contradictory records for one repository |
| closing with no `integration_acceptance` | mistaking per-repository green for a working system |
| closing while a repository has no passing checks at a known revision | evidence-free completion |
| closing while a revision was never observed on the branch it ships from | a completion record for work still sitting on a branch |
| a single-repository changeset | ceremony where a commit would do |
| open past `max_open_days` | repositories mid-contract-change with no closing evidence |

---

## When to open one

**Do**, when a change spans two or more repositories and at least one of them
consumes an interface another provides.

**Do not**, for a change inside one repository. Its own history is a complete
record, and `vat lint` will tell you so.

`vat fit --contracts N` gives the threshold for your situation: below two
cross-repository contracts, this layer is ceremony.

---

## Relationship to evidence packets

| | |
| --- | --- |
| `vat evidence` | the contract given to a worker **before** it starts |
| `vat changeset` | the evidence recorded **after**, and the way back |

A packet can name the changeset it belongs to, which links the intent to the
outcome:

```bash
vat evidence new EP-004 "Cancel v2 in payments" --repos payments --changeset CS-0001
```

Together they close the loop that delegation usually leaves open: the worker is
told what "done" means before starting, and completion is settled by the
recorded checks rather than by the worker's own report.
