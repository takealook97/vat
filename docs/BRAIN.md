# The brain

> Reviewed organisational knowledge: facts that carry where they came from, and
> stop counting as true when nobody re-checks them.

Code says what a system does. It never says what the organisation was trying to
achieve, what was already tried and abandoned, or why a decision was made — and
those are what is lost first, and most expensively.

---

## Why every knowledge base rots, and what stops it here

Someone writes "the collector runs hourly." It is true. It stays in the file for
three years after it stopped being true, and it is quoted as current fact the
whole time.

Four mechanisms make that structurally impossible:

1. **One fact per file**, so nothing hides inside a document nobody finishes.
2. **Provenance on every claim about the present** — the owning repository, the
   exact revision, the observation date.
3. **Automatic demotion** once an observation ages out. Not deletion: the claim
   becomes *unverified*, and stops being citable.
4. **A bounded review queue**, ordered by what it costs to leave unresolved.

---

## Layout

```text
brain/
├── .brain                    marker
├── AGENTS.md                 how an agent works in here
├── CURRENT.md                generated index — the entry point for every question
├── graph.json                generated relation graph
├── GOAL.md                   projections. list, ordering, links — no detail
├── STATUS.md
├── GAP_ANALYSIS.md
├── ROADMAP.md
├── DECISIONS.md
├── MEMORY.md
├── AGENT_OPERATING_MODEL.md
├── goals/O-0001-....md       atomic canon. one fact each
├── gaps/G-0001-....md
├── decisions/D-0001-....md
├── memory/2026-08/M-0001-....md
├── history/
└── archive/
```

`CURRENT.md` and `graph.json` are **generated**. Editing them by hand is drift,
and `vat brain check` reports it.

---

## The record

```yaml
---
id: G-0014
status: active
date: 2026-05-02

claim_kind: current-state
owned_by: payments
source_ref: payments@3f9a1c2e8b74:docs/ORDERING.md
observed_at: 2026-05-02
revalidate_on: source-revision-change

refs: [O-0003, D-0031]
---

# G-0014 — Retries can double-submit an order

## Distance
...
## Evidence
...
## What closing it requires
...
```

| Field | Meaning |
| --- | --- |
| `id` | unique across the repository |
| `status` | see the lifecycle below |
| `date` | when the record was created |
| `claim_kind` | `current-state`, `historical`, or `intent` |
| `owned_by` | the repository canonical for this fact |
| `source_ref` | `<repo>@<revision>[:<path>]` — an exact revision, never a branch |
| `observed_at` | `YYYY-MM-DD`, when it was last verified against its source |
| `revalidate_on` | what should trigger a re-check |
| `supersedes` / `superseded_by` | the decision replacement chain |
| `refs` | related record identifiers |
| `axis` | grouping, for goals |
| `reason` | required for `quarantined` and `revoked` |
| `reviewed_by` | who promoted it |

### `claim_kind` decides which rules apply

| Kind | Requires provenance | Decays |
| --- | --- | --- |
| `current-state` | yes | yes |
| `historical` | no | no |
| `intent` | no | no |

These three are the whole set; `vat brain new` rejects anything else rather than
writing an unrecognised kind into a record where no check would ever look at it
again.

A statement about what happened does not decay. A statement about what is true
right now does.

### Why the revision, not the branch

A branch keeps moving and takes the evidence with it. `payments@main` means
"whatever payments says today", which is not evidence of anything. `vat brain
check` warns when a `source_ref` names a branch.

---

## Lifecycle

```text
            ┌──────────────┐
   new ────►│ provisional  │──── promote ───┐
            └──────────────┘                │
                                            ▼
            ┌──────────────┐  sweep   ┌──────────┐
            │    stale     │◄─────────│  active  │
            └──────┬───────┘          └────┬─────┘
                   │  promote (re-verified)│
                   └──────────────►────────┤
                                           │
            ┌──────────────┐               │
            │ quarantined  │◄──────────────┤  suspected wrong
            └──────┬───────┘               │
                   │                       │
                   ▼                       ▼
            ┌──────────────┐        ┌──────────────┐
            │   revoked    │        │  superseded  │
            └──────────────┘        └──────────────┘
```

| Status | Citable | Meaning |
| --- | --- | --- |
| `provisional` | no | recorded, never reviewed |
| `active` | **yes** | reviewed, and its evidence is current |
| `stale` | no | was true when observed; nobody has re-checked it |
| `quarantined` | no | suspected wrong or contaminated |
| `superseded` | no | replaced, kept for its reasoning |
| `revoked` | no | withdrawn, kept as a tombstone |
| `resolved` | no | a closed gap |

**Only `active` is an answer.** Everything else is a pointer to work.

### `stale` is the important one

```console
$ vat brain sweep
WARN  G-0014    active → stale (observed 115 days ago, past the 90-day window)

$ vat brain sweep --apply
OK    G-0014    active → stale (observed 115 days ago, past the 90-day window)
```

The claim is not false. Nobody knows. That distinction is what separates a
knowledge repository that is still useful in year three from one that has become
a confident liar.

Nothing is deleted; the reasoning survives intact.

### `quarantined` and `revoked` keep the trail

A claim can be *suspect* without being disproven. Deleting it would destroy the
record of why it was doubted, so it is withheld instead — and a withdrawal must
state a reason, or it cannot be reviewed later. `vat brain check` enforces that.

---

## The review queue

```console
$ vat brain review
ID      STATUS       AGE  CITED  TITLE                          WHY
G-0014  stale        115  7      Retries are not idempotent     observation aged out; re-verify against the owning repository
D-0031  quarantined  62   3      Pricing is per seat            suspected wrong; confirm or revoke
G-0022  stale        94   0      Log rotation is weekly         observation aged out; re-verify against the owning repository
```

Priority weighs how many records cite the claim against how long it has gone
unverified, with quarantined items lifted above unreviewed ones because they may
already have been quoted as fact.

Without that ordering the queue is a flat list. It grows until it is ignored
wholesale, and then the knowledge layer is untrustworthy again — the exact state
it was built to prevent.

`policy.brain.review_sla_days` sets the point past which the queue itself is
reported as failing. That is deliberate: an unbounded queue is a defect of the
process, not of any one record.

---

## The promotion gate

```console
$ vat brain promote G-0014 --reviewer alex
error: G-0014: a current-state claim needs source_ref before it can be promoted
```

Analysis does not become organisational truth because it was useful. A claim
about the present with no owner and no source revision cannot be promoted at
all — the refusal is what makes the gate real rather than decorative.

Promotion stamps the observation date and the reviewer.

### What is worth promoting

- a conclusion drawn across two or more repositories
- a comparison or audit that is expensive to redo
- anything that changes a goal judgement, a gap, an execution order, or an
  approval boundary
- a current-state analysis likely to be asked about repeatedly

### What is not

One-off lookups. Ideas with no evidence. Conversation summaries.

---

## Supersession

```console
$ vat brain supersede D-0031 D-0042
OK    D-0031    superseded by D-0042
OK    D-0042    active, supersedes D-0031
```

A record cannot supersede itself: that writes a chain pointing nowhere, and no
`vat` command can repair it afterwards.

The original is never edited to say something new — that would destroy the only
account of why it once made sense. Both files are updated so the chain reads
correctly from either end, and `vat brain check` fails on a one-way link or a
cycle.

---

## Reading

```console
$ vat brain query idempotency retries
INFO  G-0014  Retries can double-submit an order    active
      gaps/G-0014-retries-can-double-submit.md
      │ Two identical requests within the retry window create two orders.
```

The default surface is deliberately narrow: the root projections and the
non-terminal records. Superseded reasoning is excluded, because an answer
assembled from it is worse than no answer.

`--all` widens the search to history, archives, and terminal records — for
auditing why something was decided, rather than asking what is true now.

The reading contract:

1. Find identifiers in `CURRENT.md`.
2. Open only those records.
3. Re-verify any claim about the present against the repository that owns it. A
   record states when it was last observed, not that it is still true.
4. Open `history/` and `archive/` only when asked for past reasoning.

---

## Drift is a signal, not a verdict

```console
$ vat lint
WARN  brain/source-revision-drift · G-0014    payments has moved 47 commits since this was observed at 3f9a1c2; re-check, do not assume it broke
```

`vat` deliberately refuses to conclude anything. A typo commit does not change
what is true. Turning drift into a specific, dated item on the review queue is
the whole point; automatically invalidating claims would make the layer useless
in a week.

---

## Adopting an existing knowledge repository

```console
$ vat brain adopt cortex
OK    cortex        adopted as the brain repository
INFO  records       178 found
WARN  schema        12 records need attention; run `vat brain check` for the list
      → Nothing was rewritten. Bring them up one at a time.
```

`vat` reads what is there and reports what does not meet the schema. It never
rewrites a record, so an existing repository can be brought under the rules
gradually rather than converted in one pass.

---

## Working here

```bash
vat brain new gap --title "Retries can double-submit" \
                  --claim current-state --owner payments
# write the record
vat brain build && vat brain check
vat brain promote G-0014 --reviewer alex
vat brain build
```

`build` before `check`, and `build` after any status change: the index is a
projection, and a stale projection is drift.
