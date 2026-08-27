---
id: D-0001
status: active
date: "2026-03-14"
claim_kind: intent
reviewed_by: alex
refs: [O-0001]
---

# D-0001 — Orders own their own idempotency keys

## Decision

The client supplies an idempotency key with every order request, and `payments`
stores it against the created order. The console never generates one.

## Why

Two clients retrying the same submission must produce one order. Generating the
key server-side cannot distinguish a retry from a genuine second order, because
by then the two requests are identical.

Putting it in the console was considered and rejected: a second consumer would
have to reimplement the same scheme, and the two would drift.

## Consequences

Every consumer of the order API must generate and persist a key before it calls.
That is a real cost for new integrations, accepted because the alternative is
duplicate charges.

## Reversal

If the ordering path ever becomes single-consumer, this is worth revisiting.
This file is not edited if that happens — a new decision supersedes it.
