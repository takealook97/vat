# One change, three repositories

> The arc the other commands exist for: one contract change, from the session
> that starts it to the session that inherits it.

This assumes a workspace already exists — see the [60 seconds](../README.md#60-seconds)
walkthrough in the README if it does not yet.

---

## 1 · Say what "done" means while it is still cheap to scope

```console
$ vat evidence new EV-0007 "Order cancellation moves to v2" \
    --repos payments,console \
    --acceptance "cancel-then-refund passes end to end against a live payments"
OK    EV-0007                   evidence/EV-0007.yaml
```

The briefing is what you hand the agent — objective, repositories it may write
to, and the acceptance it will be judged against. Written first, it is a
contract; written afterwards, it is whatever happened to pass.

## 2 · Open the changeset, which captures the way back before anything moves

```console
$ vat changeset new "Move order cancellation to v2" --repos payments,console
OK    CS-0001                   changesets/CS-0001.yaml
INFO  payments                  return point 3f9a1c2e
INFO  console                   return point 8b2e0d19
```

## 3 · Do the work

In the repositories, with git, as normal — `vat` is not in that loop and does
not want to be. `vat status` is how you see all of it at once.

## 4 · Verify, land, then close

```console
$ vat changeset verify CS-0001
OK    payments · make check     14.2s at a71c93d0
OK    console · pnpm test       31.8s at 5c1f80ab
OK    CS-0001                   every repository verified

$ vat ship CS-0001
OK    payments                  landed on origin/main
FAIL  console                   5c1f80ab is not on origin/main; verified but not landed

$ vat changeset close CS-0001 --acceptance "cancel-then-refund passes end to end"
```

Three different claims, and a workspace that collapses them is confidently
wrong about at least one: checks passing is not the pieces working together,
which is why `--acceptance` must describe something end to end; and neither is
shipping, which is the one git question `vat ship` asks regardless of forge.

## 5 · Record why, not just what

```console
$ vat brain new decision --title "Cancellation is a v2-only operation" --owner payments
OK    D-0042                    decisions/D-0042-cancellation-is-a-v2-only-operation.md

Write the record, then: vat brain build && vat brain check
It stays provisional until: vat brain promote D-0042
```

Provisional means not citable. A record nobody reviewed does not get to count
as a fact simply because it exists.

## 6 · The next session starts where this one stopped

```console
$ vat brain query cancellation
INFO  D-0042  Cancellation is a v2-only operation  provisional
      decisions/D-0042-cancellation-is-a-v2-only-operation.md
      │ # D-0042 — Cancellation is a v2-only operation

1 result. Open the records themselves; this is an index, not an answer.
```

Six months on, `vat changeset show CS-0001` still names the revisions that were
verified together and the checks that proved it, and `vat changeset undo-plan
CS-0001` still prints the reverse-enrolment order to get back — consumers
first, then the contract they depend on, and `vat` prints the plan without ever
running it, because what has already been deployed is not something `vat` can
see.

---

That is the loop: a boundary an agent cannot cross by accident, a record of
what was verified together, and a fact that expires when nobody re-checks it.
The layers are independently useful, and `vat fit` will tell you which of them
you have not earned yet.
