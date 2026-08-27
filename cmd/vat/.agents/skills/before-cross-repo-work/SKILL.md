---
name: before-cross-repo-work
description: Open and close a changeset when a change spans more than one repository.
---

# Before cross-repository work

## When to use this

A change will touch more than one repository. Open the record before the first
edit, not after: `new` and `add` record where each repository stood before
the change began, and once it has landed that can no longer be observed.

## Steps

1. `vat changeset new "<objective>" --repos a,b`. The objective is the one
   claim the record makes, so write what should be true afterwards rather than
   what you are about to type.
2. `vat changeset add <id> <repo>` for a repository that turns out to be
   involved after all. It refuses once the changeset is closed, because
   enrolling a repository afterwards rewrites the claim the record exists to
   make.
3. Do the work.
4. `vat changeset verify <id>`. It runs each repository's canonical checks
   and records the result against the exact revision it ran on. It refuses on a
   dirty working tree.
5. `vat changeset close <id> --acceptance "..."`. The acceptance must
   describe something end to end; verifying proves the combination builds, not
   that it does what it was for.
6. `vat ship <id>` to judge whether those verified revisions actually
   reached the branch each repository ships from. Verifying and shipping are
   different questions.

## When it must stop

If verification fails, stop. Do not close the changeset to tidy the list — a
closed record asserts that this combination was checked together, and one closed
over a red check is worse than no record, because somebody will trust it.

If the work is abandoned, `vat changeset abandon <id> --reason "..."`. Why it
stopped is the whole value of an abandoned record.

To undo, `vat changeset undo-plan <id>` prints the commands that would return
every repository to its start point. It prints them and never runs them; read
them before you do.
