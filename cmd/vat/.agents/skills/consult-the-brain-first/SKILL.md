---
name: consult-the-brain-first
description: Check the knowledge layer before stating something as true of this workspace.
---

# Consult the brain first

## When to use this

Before stating something as true of this workspace — how a service behaves, why
a decision was made, what a repository owns — when the answer is not visible in
the file in front of you.

## Steps

1. `vat brain query <terms>` for what is held to be true now. The default
   surface is deliberately narrow.
2. `vat brain query <terms> --all` when the question is why something was
   decided rather than what is true, since that widens the search to history,
   archives, and terminal records.
3. Read the record's status before citing it. A `provisional` record has not
   crossed the promotion gate, and a claim pinned to a revision its repository
   has since moved past is evidence about a tree that no longer exists.
4. `vat brain review` to see which claims are most worth re-checking: it
   orders by how many records cite a claim against how long it has gone
   unverified.
5. `vat brain check` before trusting the layer as a whole.

## When it must stop

If no record answers the question, say so. Inventing the answer and writing it
into a record is how a knowledge layer becomes a place where wrong things are
harder to correct than they were when nobody had written them down.

Do not promote a record to make a citation look stronger. Promotion is a claim
that the evidence was re-read, and `vat brain promote` refuses to move the
observation date forward unless the evidence is demonstrably unchanged or you
state with `--reverified` that you read the source yourself.
