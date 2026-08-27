---
id: O-0001
status: active
date: "2026-03-14"
claim_kind: intent
axis: correctness
reviewed_by: alex
---

# O-0001 — One order never becomes two

## Judgement criterion

Submitting the same order twice within the retry window — from any consumer,
including scheduled jobs — creates exactly one order and one charge.

Written as an observation, not an intention: it is settled by running the test,
not by anyone's opinion about the design.

## Current measurement

Not met. See G-0001: the scheduled reorder path is exempt.

The console path is covered by a contract test. The reorder path has no
equivalent, which is why the gap exists rather than merely being suspected.
