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

## What `memory` is, and what it is not

Three different things get called memory, and this layer owns exactly one of
them. Conflating them is how a knowledge repository fills up with an agent's
session log and stops being quotable.

| Content | Owner |
| --- | --- |
| where the last session got to, and what to pick up | the agent runtime, or a retrieval layer |
| how a particular agent prefers to work | that agent's own repository |
| **a verification trap this repository keeps springing** | **brain — a reviewed observation** |
| **what is running now, and the revision that proves it** | **brain — a current-state claim** |
| **a judgement the organisation adopted, and why** | **brain — a decision** |

A `memory` record here is a **reviewed, reusable observation**: something worth
reaching for again, not something that happened. If it will not be useful the
second time, it is a session note and belongs where session notes go.

That is why `vat brain new memory` opens with these headings:

```markdown
## Trigger          what situation should bring this back
## Lesson           what to do differently, in one sentence
## Evidence         the run that failed, the file, the exact revision
## Scope            `workspace`, or the one repository it applies to
## Reuse condition  what has to stay true for it to still apply
```

They are a convention, not a schema. Nothing checks them yet, deliberately: a
field becomes part of the record schema here only once there is a rule worth
enforcing on it, and that is decided by use rather than in advance. A lesson
scoped to a branch is not recorded at all — it expires before anyone re-reads
it.

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
├── history/                  long-form past material, written by people
└── archive/decisions/D-0002-....md   records that reached an end state
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
   new ───►┌─────────────┐
           │ provisional │──────── promote ────────┐
           └─────────────┘                         │
                                                   ▼
           ┌─────────────┐        sweep      ┌───────────┐
           │    stale    │◄──────────────────│  active   │
           └──────┬──────┘                   └─────┬─────┘
                  │  promote --reverified          │
                  └───────────────►────────────────┤
                                                   │
           ┌─────────────┐      quarantine         │
           │ quarantined │◄────────────────────────┤
           └──────┬──────┘                         │
                  │ revoke            supersede / resolve
                  ▼                                ▼
   ════════════════════════════════════════════════════════════
           ┌─────────────┐   ┌──────────────┐   ┌──────────┐
           │   revoked   │   │  superseded  │   │ resolved │
           └─────────────┘   └──────────────┘   └──────────┘
```

Below the line is one way. Nothing promotes back out — if a withdrawn claim
turns out to hold, it is recorded again rather than revived, because a tombstone
that can be flipped is not a tombstone. `vat brain archive` moves these three
out of the working directories.

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

Promotion stamps the observation date and the reviewer, and there are three
further things it will not do.

**It will not re-date a claim against evidence nobody looked at.** vat reads the
owning repository. If it is still at the revision the claim was pinned to,
nothing about the source has changed and the date moves freely. If it has moved
— or vat cannot see the repository at all — the date only moves with
`--reverified`, which is you stating you re-read the source. Without that, one
keystroke turns a four-hundred-day-old sentence into "verified today", which is
the exact failure the whole layer exists to prevent.

```console
$ vat brain promote G-0014 --reviewer alex
error: G-0014: payments has moved since this was observed (pinned 3f9a1c2e8b74,
  now 9d4e7b1a0c62), so the observation date cannot be advanced.
  Re-read the source at the new revision, then: vat brain promote G-0014 --reverified
```

Passing `--reverified` re-pins `source_ref` to the revision you actually read.
Leaving the old one would date the record today against something nobody opened.

**It will not revive an end state.** A superseded, revoked, or resolved record
stays that way; if the claim turns out to hold after all, record a new one. A
tombstone that can be flipped back is not a tombstone.

**It will not accept an unsigned promotion** when
`policy.gates.brain_promote` is `manual`. A gate nobody has to sign is a note.

The same gate closes the path around it: with
`policy.brain.require_promotion_gate` set, `vat brain supersede` leaves the
replacement `provisional` instead of promoting it on the way past.

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

## Withdrawing, doubting, closing

```console
$ vat brain quarantine D-0031 --reason "contradicted by the billing export"
OK    D-0031    quarantined

$ vat brain revoke D-0031 --reason "the export was right; this was never true"
OK    D-0031    revoked

$ vat brain resolve G-0022
OK    G-0022    resolved
```

These states carried check rules and review-queue weights from the first
release and had no command, so reaching one meant hand-editing the YAML of the
record whose trustworthiness was already in doubt — the manual step the tool
exists to remove, performed at the worst possible moment.

A reason is required for a quarantine and a revocation. `resolved` describes a
gap that has been closed, so only a gap can take it.

---

## The archive

```console
$ vat brain archive
WARN  D-0031    revoked → archive/decisions/D-0031-pricing.md

$ vat brain archive --apply
OK    D-0031    revoked → archive/decisions/D-0031-pricing.md
```

Terminal records leave the working directories. Nothing is deleted, and an
archived record is still loaded — its supersession chain is still checked from
both ends — but it is out of the entry point, out of the default search surface,
and in one directory an external index can exclude wholesale.

Relative links inside a moved record are repointed so they still resolve.

`history/` is the other half of that separation and vat never writes to it. It
is where people put long-form past material — the analysis a decision was drawn
from, a narrative that has been overtaken — so that it stays searchable under
`--all` without sitting in the surface that answers ordinary questions. vat
creating it and filling it would make it a second archive with no rule
governing what belongs there.

---

## The on-disk contract

The point of writing knowledge as files is that it outlives the tool that wrote
it. That is only true if the files say which agreement they were written
against, so the marker carries a version:

```
$ cat brain/.brain
# This directory is a vat brain repository.
# Generated projections: CURRENT.md, graph.json — rebuild with `vat brain build`.
schema: 1
```

Schema 1 is:

| | |
| --- | --- |
| record directories | `goals/` `gaps/` `decisions/` `memory/`, one Markdown file per record |
| record identity | `id` in YAML front matter; the filename repeats it for humans and is not authoritative |
| lifecycle | `status` is one of `provisional` `active` `stale` `quarantined` `superseded` `revoked` |
| provenance | `source` names `<repo>@<revision>[:<path>]`; `observed` is the date it was read |
| supersession | `superseded_by` and `supersedes` are written at both ends and must agree |
| generated | `CURRENT.md` and `graph.json` are derived and never authoritative |
| out of the working set | `archive/`, still loaded so a supersession chain reads from both ends |

Anything in that table is what another tool may rely on. `brain/schema-newer`
is what stops an older `vat` from reading a newer brain and reporting it clean
because half of what governs it was invisible.

A brain scaffolded before the version existed has no `schema:` line. That is not
an error — it is the same contract, written before the contract had a number.

---

## What the index refuses to become

`CURRENT.md` is an entry point, which means it has a size. Each section lists at
most fifteen records and then says how many are left and where they are:

```markdown
| `D-0031` | active | Pricing is per seat | [D-0031-pricing.md](decisions/D-0031-pricing.md) |

12 more in [DECISIONS.md](DECISIONS.md).
```

The fifteen kept are the ones the rest of the repository cites most, the same
measure the review queue uses. Truncating by identifier would keep whatever was
written first and hide everything current.

Before those bounded record sections, the index links the maintained root views
that exist: current state, goals, gaps, execution order, decisions, reviewed
observations, and the agent operating model. `STATUS.md` is the standard current
state name; an adopted brain using `PORTFOLIO_STATUS.md` keeps that name and is
routed there when `STATUS.md` is absent.

An index that has to be read in full to be used is the summary file this layer
was built to replace, arriving late — once the repository is finally large
enough to be worth having.

---

## What `vat brain check` reports

Every rule, and the state each was written for. An unlisted rule is one nobody
knows to look for, so this table and `brain.RuleNames()` are compared by a test.

| Rule | Severity | Fires when |
| --- | --- | --- |
| `brain/claim-observed` | error | a current-state claim with no `observed_at` — it can never age out |
| `brain/claim-owner` | error | a current-state claim naming no owning repository |
| `brain/claim-source` | error | a current-state claim with no `source_ref`, or one that is not `<repo>@<revision>[:<path>]` |
| `brain/claim-source-branch` | warn | evidence pinned to a branch, which keeps moving and takes the claim's meaning with it |
| `brain/claim-stale` | warn | an active claim past the policy window; `vat brain sweep --apply` demotes it |
| `brain/date-unreadable` | warn | a present but invalid date no staleness rule can read; an absent optional `date` is not an error |
| `brain/id-duplicate` | error | two records claiming the same identifier, so a reference resolves to either |
| `brain/id-missing` | error | a record with no identifier; nothing can cite it and nothing can supersede it |
| `brain/link-broken` | error | a relative link in a record that resolves to nothing |
| `brain/quarantine-reason` | error | a quarantine with no stated cause, which cannot be reviewed or lifted later |
| `brain/record-malformed` | error | a file that cannot be read as a record, and is therefore invisible to every rule above |
| `brain/record-secret-suspected` | error / warn | a line that carries a credential; error for unmistakable shapes, warning for heuristics |
| `brain/ref-missing` | error | a reference to a record that does not exist |
| `brain/ref-withdrawn` | warn | a record citing a revoked or quarantined one as support |
| `brain/review-overdue` | warn | a record past `review_sla_days` — a defect of the process, not of the record |
| `brain/revoke-reason` | error | a tombstone with no stated cause |
| `brain/schema-newer` | error | a brain written against a contract this build cannot read, whose newer fields would be silently invisible |
| `brain/status-unknown` | error | a status no rule here understands, so no rule here governs the record |
| `brain/supersede-cycle` | error | a replacement chain that loops, so it has no current end |
| `brain/superseded-asymmetric` | error | a chain readable from one end only |
| `brain/superseded-missing` | error | `superseded_by` naming a record that does not exist |
| `brain/superseded-orphan` | error | status `superseded` with nothing named as the replacement |
| `brain/superseded-status` | error | `superseded_by` set while the status says otherwise |
| `brain/supersedes-asymmetric` | error | the same break, seen from the replacement |
| `brain/supersedes-missing` | error | `supersedes` naming a record that does not exist |
| `brain/title-missing` | warn | a record with no heading; the index can show only its identifier |

```console
$ vat brain check --only claim     # the provenance rules alone
$ vat brain check --list           # every rule name
```

---

## Two rules about the records themselves

**A file that cannot be read is a finding, not a crash.** One record with a
merge conflict marker in its header used to take down `check`, `query`, `sweep`,
`build`, `doctor`, and `lint` together. Now it is `brain/record-malformed` and
everything else still loads and still reports.

**A record must not carry a credential.** `brain/record-secret-suspected` names
the line and the kind of credential and never the value — a finding that repeats
a secret has published it a second time, somewhere people paste into chat. This
rule was the one thing the methodology asserted with nothing checking it, and it
matters most here: a brain repository is exactly what an organisation points a
search index at.

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

Ranking discounts length. Counting raw occurrences instead is arithmetic, not
relevance: a long record repeating one query word beats a short record that
answers all three, and the long record is usually the sprawling one nobody has
split up yet. Matching every term is worth more than any amount of repetition.

The reading contract:

1. Find identifiers in `CURRENT.md`.
2. Open only those records.
3. Re-verify any claim about the present against the repository that owns it. A
   record states when it was last observed, not that it is still true.
4. Open `history/` and `archive/` only when asked for past reasoning.

---

## Pointing a search index at the brain

Semantic search is a real need and it is not this layer's job. vat has one
third-party dependency and opens no network connection, and both are security
properties rather than accidents; an embedding model inside the binary would
cost both. There is also a plainer reason: `policy.trust.untrusted` already
classes embeddings with scraped pages and model output. A vector is not
evidence, and this layer deals in evidence.

So retrieval goes outside, and the boundary is a contract rather than an
integration. **vat will never grow a command named after a search product.** A
vendor adapter in the core turns the tool into that vendor's document generator
and ties its release cycle to someone else's.

What vat offers an index is what it already writes:

| Surface | What an index gets from it |
| --- | --- |
| `goals/`, `gaps/`, `decisions/`, `memory/` | atomic records, one fact each, with provenance in the header |
| `CURRENT.md` and the root projections | vat's own summary layer, already written |
| `graph.json` | every record's id, kind, **status**, path, owner, and `source_ref` |
| `archive/`, `history/` | everything finished with, in directories of their own |

Four rules make that safe to index:

1. **Exclude `archive/` and `history/`.** They hold exactly the withdrawn and
   replaced claims that must not surface as answers. This is why the archive
   command exists — a directory-level exclusion is the cheapest filter any index
   has, and it only works if terminal records are actually in a directory.
2. **Do not let the index summarise.** vat already has a summary layer, and it
   is checked for drift against the records. A second, unchecked summary
   competing with it is the failure this layer was built to prevent. Index
   embeddings, not generated prose.
3. **Nothing flows back.** No output of a retrieval layer is written into a vat
   record. A search result is a place to look, and it holds no authority over a
   record's status or over what a person asked for.
4. **Keep the trail.** A result should be traceable to the atomic record and its
   `source_ref`, so a reader can check the claim's status rather than trusting
   the snippet.

Nothing about this needs vat to know the index exists. If it disappears, every
`vat` command behaves identically — vat never called it.

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
OK    cortex/.brain  written
OK    cortex/graph.json  generated
WARN  cortex/CURRENT.md  left alone; vat did not write it
      → Move or delete it to let vat own the name, then `vat brain build`.
INFO  records       178 found
WARN  schema        12 records need attention; run `vat brain check` for the list
      → Nothing was rewritten. Bring them up one at a time.
```

`vat` reads what is there and reports what does not meet the schema. It never
rewrites a record, so an existing repository can be brought under the rules
gradually rather than converted in one pass.

Two files are written: the marker that makes the directory a brain, and the
generated projections whose absence that marker turns into drift. Nothing else.
Adoption that reported success and left `vat lint` answering "run vat brain
init" about the same directory had not finished.

A projection is written only when the name is free — empty, or already holding
a file that carries vat's own provenance line. A repository that has kept a
`CURRENT.md` by hand for years keeps it, and the file is reported rather than
replaced. That state is `brain/projection-unmanaged` in `vat lint`; the remedy
is yours to choose, because the file is yours:

- move it (`git mv CURRENT.md STATE.md`) and let vat generate the index beside
  it, or
- delete it once its content has been broken into atomic records, which is the
  conversion this layer exists for.

Running `vat brain build` while the file stands does nothing to it, by design:
a repair that destroys the thing it was pointed at is not one.

---

## Claims about systems you do not govern

A `source_ref` naming a repository outside `vat.yaml` is reported, because
provenance pointing at nothing reads as verified. Do not silence it by enrolling
the system: that stops the warning by making the workspace claim to sync,
diagnose, and ship a repository it does not own, and widens `sync`, `doctor`,
and every changeset with it.

```yaml
source_ref: legacy-crm@abc1234
source_external: true
```

Says it once. Nothing else is suppressed — the claim still expires on schedule,
because nothing here can re-read it for you. Setting it for a repository the
workspace *does* govern is an error: it would exempt a checkable claim from
every rule that makes it checkable.

---

## Keeping the maintained views honest

The generated index routes readers to `STATUS.md`, `ROADMAP.md`, `DECISIONS.md`
and the rest. vat writes neither their content nor their judgements, so no
schema rule reaches them — a repository can report zero findings while every
document its own entry point recommends has gone months without being revisited.

`brain/view-stale` reports a view the records left behind by more than
`policy.brain.review_sla_days`. The window is not zero on purpose: every record
change makes every view technically older, and a rule that fires on every commit
is one people silence rather than act on. It judges dates, never content —
whether a view is *correct* is not something a tool can say.

---

## What the generated index shows

`CURRENT.md` is bounded, so it ranks rather than lists. Two sections answer two
different questions:

- **Active decisions** keeps the fifteen the rest of the repository cites most,
  and says so in the file. That is the right cut for "what does this place rest
  on".
- **Newest decisions** names up to five recent ones the ranking left out. A
  decision taken yesterday is cited by nothing yet, so ranking can only hide it,
  and an index that cannot show the newest decision gets read as stale.

Above them, **Canonical views** links the maintained synthesis documents that
exist — `STATUS.md`, `GOAL.md`, `ROADMAP.md` and the rest. An adopted repository
keeping `PORTFOLIO_STATUS.md` is recognised under that name too, so adoption
never requires renaming a document to be reachable from the index.

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
