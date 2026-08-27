---
id: G-0001
status: stale
date: "2026-05-02"
claim_kind: current-state
owned_by: payments
source_ref: payments@3f9a1c2e8b74:docs/ORDERING.md
observed_at: "2026-05-02"
revalidate_on: source-revision-change
reason: observed 115 days ago, past the 90-day window
refs: [O-0001, D-0001]
---

# G-0001 — Retries can double-submit an order

## Distance

D-0001 requires an idempotency key on every order request. The console sends
one; the scheduled reorder job does not.

## Evidence

Read from `payments@3f9a1c2e8b74:docs/ORDERING.md` on 2026-05-02, which lists
the reorder path as a known exception.

**This claim is stale.** Nobody has re-checked it since. It is not known to be
false — it is unverified, and it stops being citable until someone looks again.

## What closing it requires

The reorder job generates and persists a key per scheduled attempt. Owned by
`payments`. Roughly a day, plus a migration for existing rows.
