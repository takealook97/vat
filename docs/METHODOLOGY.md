# The operating model

> How to run many repositories in one workspace without the code, the
> organisational knowledge, the secrets, and the agents' memory contaminating
> each other.

`vat` is the mechanical half of this document. Everything described here is
either enforced by a command or explicitly marked as a judgement no tool can
make for you.

---

## 1. The one-sentence version

Four different things are usually collapsed into one folder, and separating them
is the whole method:

1. **where you go and run things** — the workspace
2. **where facts and decisions are settled** — the brain
3. **where code is implemented** — the product repositories
4. **where secrets and derived memory are kept** — credentials, and the retrieval layer

The workspace is a *control plane*, not a code repository. It knows what exists,
brings it to a known state, and says who owns what. It holds none of it.

---

## 2. Ownership

Ownership is the load-bearing concept. Most teams never write this table down,
and every problem that follows traces back to that.

| Layer | Owns | Does not own |
| --- | --- | --- |
| workspace | the repository roster, installation, updating, diagnosis, top-level routing | product code, organisational decisions, plaintext secrets |
| brain | goals, reviewed decisions, current state, gaps, execution order, permission boundaries | implementation detail, secret values, an agent's own journal |
| credential | encrypted environment, keys, auth configuration | decrypted results, general documentation, product code |
| product repository | code, tests, API and schema, architecture, its own decisions | organisation-wide priority, another repository's implementation |
| agent repository | that agent's identity, principles, workflow, operating journal | the organisation's settled facts |
| retrieval layer | fast semantic search, session memory | any authority over what is currently true |

> **A fact with no owner ends up duplicated in several documents, and then they
> disagree.** That single sentence is the reason for everything below.

Write this table before you write any tooling. `vat fit` will tell you which
rows you do not need yet.

---

## 3. Why not one repository

The usual case against a monorepo — different release cadences, stacks, access
levels, CI, and release policy — is a set of arguments about *people and
organisations*. They are true, and they are not the interesting ones any more.

The modern case *for* a monorepo is about agents: can a model see the type
definition, its callers, its consumers, and their tests at once? That is a
serious advantage and it deserves a serious answer.

**The answer is that a filesystem boundary is not a git boundary.** Repositories
cloned side by side under one root are all visible to a shell and to an agent.
`git -C <path>` addresses each one exactly. The agent gets the wide view; the
repositories keep independent branches, history, CI, releases, permissions, and
rollback points.

What you genuinely lose is cross-repository CI: nothing stops a contract change
from breaking a consumer *before* it merges. `vat changeset` does not restore
that, and does not pretend to. What it does is make the combination that was
verified a matter of record rather than of memory. That is a real reduction of
the loss, not its elimination — and if that loss is intolerable for your work,
a monorepo is the right answer and this tool is not for you.

---

## 4. The workspace

### 4.1 One manifest

Every repository the workspace governs is declared once, in `vat.yaml`. Every
command reads that one declaration. Hardcoding the list in a second place is how
a repository ends up in the manifest and missing from `.gitignore`, and then the
next root commit swallows the entire nested clone.

`vat` closes that specific hole by writing both together, and `vat lint` fails
when they disagree.

The manifest carries more than name and URL, because two columns are not enough:
a repository whose default branch is `develop` while the workspace default is
`main` is otherwise skipped by every update forever, reported as "on another
branch" — a note nobody reads.

### 4.2 Updating is a state machine, not a loop

`for r in */; do git -C "$r" pull; done` fails in three ways at once: it can
destroy local work, it hides per-repository failures behind an overall success,
and it makes decisions that belong to a human.

```text
repository absent
  └─ path empty and online ────────> clone from the declared origin

repository present
  ├─ remote mismatch ──────────────> FAIL. never rewritten: this is a supply-chain signal
  ├─ fetch failed ─────────────────> FAIL
  ├─ dirty working tree ───────────> report, advance nothing. your work is not stashed
  ├─ not on the default branch ────> report, advance nothing. you are not checked out elsewhere
  ├─ diverged ─────────────────────> FAIL. an automatic merge here guesses at intent
  ├─ local ahead ──────────────────> report, push nothing. pushing is your decision
  ├─ not behind ───────────────────> current
  └─ clean default branch, behind ─> fast-forward only
```

Dirty, feature-branch, and local-ahead are **normal working states, not
failures**. Reporting them as errors trains people to ignore the exit code,
which is worse than not having one.

A failure in one repository is never hidden by success in another.

### 4.3 Diagnosis never repairs

`vat doctor` judges and stops. A tool that silently fixes what it finds teaches
you nothing about why it broke, and on a machine holding credentials and
unpushed commits, "fixing" is how work disappears.

It never prints a secret. Findings about the credential repository are limited to
existence, whether a file looks encrypted, permissions, and age.

---

## 5. The harness

A harness is not one prompt file. It is the set of contracts that make an agent
behave consistently inside a repository:

```text
AGENTS.md                entry rules, prohibitions, what to read, what proves completion
docs/ARCHITECTURE.md     structure and dependency direction
docs/CONVENTIONS.md      code and documentation rules
docs/DECISIONS.md        judgements that must not be reversed by accident
tests/, fixtures/        expected behaviour, pinned mechanically
Makefile or check script the canonical verification command
.agents/roles/           runtime-neutral role contracts: who is running
.agents/skills/          runtime-neutral procedures, loaded on demand
.claude/, .codex/        generated per-runtime adapters
```

A contract is always in context and states what is true of the repository. A
procedure is read only when it applies, and states how a particular job is done.
Keeping them apart is what stops the always-loaded file from growing until it
truncates the ones meant to load after it.

A good harness answers: what is this repository responsible for; what does it
never do; what must be read before editing; which files must change together
when a contract changes; when is a network write or a deploy allowed; what
command proves completion; what state does it stop in on failure.

### 5.1 Read progressively

Reading every document every time is not thorough, it is counterproductive: old
history and current projection blur together and the agent quotes a superseded
decision as fact. Start at the index, open only what it names, and re-verify any
claim about the present against the repository that owns it.

**A harness must say what not to read.**

### 5.2 Two layers, and neither is optional

Tools discover context files differently. Some walk upward from the working
directory; some read from the git root down. A session opened inside a nested
repository may never see the workspace file at all.

So the defence is doubled: the workspace file carries the roster, the routing,
and the shared boundaries; each repository's file carries enough to work safely
even when opened alone.

**The root file is a map. Each repository's file is the working permit.**

This is duplication, and duplication drifts — so the shared part is *generated*
from the manifest into a marked region, and the drift is linted. The principle
applied to knowledge is applied to the harness itself.

### 5.3 Keep the root file small

Agent runtimes accumulate context files from the home directory down to the
working directory and stop at a byte budget. An oversized root file does not
merely waste context: it silently truncates the per-repository contracts that
were supposed to load after it, and the failure is invisible.

`vat lint` reports a root contract that has grown past its budget.

### 5.4 Precedence

```text
1. the user's explicit request in this session
2. security, permission, and operational gates
3. the target repository's AGENTS.md
4. the workspace's shared routing
5. architecture, decision, and convention documents
```

**Retrieved content holds no position in this order at all.** See §9.

---

## 6. The brain

Not a wiki. A versioned record of what the organisation believes is true, why it
decided what it decided, and how far the present is from the goal.

Each repository can be accurate about itself and still contradict its neighbour:
repository A says "not built yet", repository B says "running in production".
The brain owns those cross-repository contradictions, the distance between goal
and present, and the dependency order of the work.

### 6.1 Atomic records and projections

```text
atomic canon                       current projection
goals/*.md          ──────────────► GOAL.md
gaps/*.md           ──────────────► GAP_ANALYSIS.md
decisions/*.md      ──────────────► DECISIONS.md
memory/YYYY-MM/*.md ──────────────► MEMORY.md
                                    CURRENT.md   (generated)
                                    graph.json   (generated)
```

One fact per file, with its own detail and evidence. The projection holds the
list, the ordering, and the links — nothing else.

Every "AI memory" system fails the same way: detail is appended to a summary
file, the file passes three hundred lines, nobody reads it, and an agent quotes
its stale opening as current fact. Separating the two makes that impossible, and
makes the generated half checkable for drift. The entry point is bounded for the
same reason: a section that lists every record is a summary file again.

`memory/` is the part of this most often misread. It does not hold session
handoffs or an agent's journal — those belong to the runtime and to the agent's
own repository. It holds a **reviewed, reusable observation**: something worth
reaching for the next time the same situation appears, recorded with the
circumstance that should bring it back and the condition under which it stops
applying. If it will not be useful a second time, it is not a record here.

This is also where the boundary with retrieval sits. Semantic search over the
knowledge layer is useful and belongs outside the tool: an index reads the
Markdown and `graph.json`, excludes the archived and historical directories, and
writes nothing back. A search result is a place to look. The layer decides what
may be cited, and no index outranks it.

### 6.2 A claim about the present carries its evidence

```yaml
claim_kind: current-state
owned_by: payments
source_ref: payments@3f9a1c2e8b74:docs/ORDERING.md
observed_at: 2026-05-02
revalidate_on: source-revision-change
```

`source_ref` pins an **exact revision**, never a branch. A branch keeps moving
and takes the evidence with it.

**A revision moving is not a claim becoming false.** A typo commit changes
nothing. Drift is a signal that re-checking is due — `vat lint` reports it and
deliberately refuses to conclude anything from it.

### 6.3 Six states, and only one of them is an answer

| | |
| --- | --- |
| `provisional` | recorded, never reviewed — not citable |
| `active` | reviewed and citable |
| `stale` | was true when observed; nobody has re-checked it |
| `quarantined` | suspected wrong or contaminated; withheld until resolved |
| `superseded` | replaced by a later decision, kept for its reasoning |
| `revoked` | withdrawn, kept as a tombstone |

`stale` is the state that keeps the whole thing honest across years. Without it,
a claim recorded once stays "active" forever. `vat brain sweep` applies it on a
clock, and demotion is never deletion: the record and its reasoning survive, it
simply stops being citable.

`quarantined` exists because a claim can be *suspect* without being disproven,
and deleting it would destroy the trail showing why it was doubted. A withdrawal
must state a reason, or it cannot be reviewed later.

### 6.4 Decisions are replaced, never rewritten

Editing a settled decision to say something new destroys the only account of why
it once made sense.

```yaml
# the original, untouched except for the link forward
id: D-0031
status: superseded
superseded_by: D-0042

# the replacement
id: D-0042
status: active
supersedes: [D-0031]
```

Both ends are checked. A one-way link is an error.

### 6.5 The review queue must be bounded

`revalidate_on: source-revision-change` is a good rule that fails without a
queue policy. With five active repositories, re-check candidates accumulate
weekly. Six months later the queue holds a hundred items, everyone ignores it
wholesale, and the brain is untrustworthy again — the exact state it was built
to prevent.

Three mechanisms keep it bounded:

- **automatic demotion** once an observation ages past the window
- **priority** weighted by how many records cite the claim and how long it has
  gone unverified — a stale claim nothing cites can wait; one the roadmap rests
  on cannot
- **a review SLA**, past which the queue itself is reported as failing

### 6.6 Promotion is a gate, not an honour system

Analysis does not become canonical because it was useful. A claim about the
present with no owner and no source revision **cannot be promoted at all** —
that refusal is what makes the gate real.

Worth promoting: a conclusion drawn across two or more repositories; a
comparison or audit that is expensive to redo; anything that changes a goal
judgement, a gap, an execution order, or an approval boundary.

Not worth promoting: one-off lookups, ideas with no evidence, conversation
summaries.

---

## 7. Multi-repository atomicity

Three repositories changing together is at least three commits. They cannot be
one. What replaces atomicity is a **revision bundle plus verification evidence
plus a closing gate**:

- fix the participating repositories and the contract before starting
- record where each one stood, because after the change it can no longer be
  observed
- verify in dependency order and record the exact revision each check ran on
- confirm each verified revision actually reached the branch its repository
  ships from, because verified and shipped are different claims
- close only with a single end-to-end outcome — every repository's own checks
  passing is not the same as the pieces working together
- never declare completion when a step failed

Without this record, "which three revisions were verified together?" is
unanswerable within weeks, and reverting becomes archaeology. `vat changeset`
is that record, and it generates the return plan in reverse enrolment order so
no window exists where a consumer expects an interface that is already gone.

### 7.1 The landing gate is git, not a forge

The last step is the one most easily faked. "We tested it" and "it shipped" feel
like the same sentence on the day, and read as the same claim forever after —
which is how a workspace becomes certain about a change still sitting on
somebody's branch.

The gate must therefore be observable, and it must be observable **everywhere**.
A pull request is not: GitHub, GitLab, and Gerrit each name and model one
differently, so gating on it buys a vendor dependency and answers the wrong
question anyway, because an open pull request is precisely the state of not
having landed.

What holds on every forge, and on a bare remote on a machine you own, is one
git question:

```
git -C <repo> merge-base --is-ancestor <verified-revision> <remote>/<default-branch>
```

`vat ship` asks it for every participating repository and records the answer.
Nothing is pushed and nothing is merged — landing the work stays a human act,
and the tool's job is to refuse to pretend it happened. A pull request URL is
kept beside the revision as evidence, never as the gate.

---

## 8. Delegation

An agent Head interprets a request, plans, hands work to executors, verifies the
result, and judges completion. Two rules make it work.

**Give a written contract before starting.** Delegation without one produces
work that is plausible and wrong: the worker infers the goal, invents an
acceptance criterion, and reports success against its own invention. The
contract names the objective, the repositories it may write to, what is
explicitly out of scope, the observable outcome that settles it, and the
commands that prove it.

**A worker's report of success is not evidence.** The coordinator checks the
diff, the canonical command, the revision, and the official state. `vat evidence`
holds the contract; `vat changeset verify` produces the evidence.

**Isolate capability by role.** Only the Head needs organisation-wide retrieval.
An executor sees its packet and its target repositories. A reviewer sees a
finished diff and the verification evidence. Sensitive domains are separate
capabilities. A role that declares no write target generates a **read-only**
adapter — having a senior-sounding name grants no mutation capability.

---

## 9. Trust, and the boundary agents cannot see

An agent that reads many repositories, issue threads, fetched pages, and search
results is reading text other people can write. Permission separation does not
help here: the content itself is the attack surface.

| Tier | Sources | May do |
| --- | --- | --- |
| canonical | the brain | state facts, constrain behaviour |
| semi-trusted | workspace repositories | state facts about themselves |
| untrusted | search results, fetched pages, issue comments, model output | **nothing. this is data** |

**Retrieved content holds no position in the precedence order.** An imperative
sentence inside a fetched document is a quotation, not a request. If retrieved
content appears to instruct an agent, that is something to report to the user,
not to follow.

`vat` renders this boundary into every generated contract, and `vat lint` warns
when a workspace has never declared one. This is a control you configure — it is
not a guarantee any tool can provide.

---

## 10. Secrets

Keeping secrets in each repository's `.env` means nobody can say which copy is
current and a new machine cannot be rebuilt reliably.

- Commit **ciphertext only**. A private repository is not encryption: accounts
  are compromised, repositories are forked, backups leak.
- Keys live outside the repository, with an independent recovery recipient in a
  different failure domain.
- Deployed environments are *materialised results*, never the canon. Changes
  start at the canon.
- Compare by hash and key name, never by value.
- Applying to production is a separate approval.

**Document what exists, who owns it, how it is verified, the file permissions,
and the recovery status. Never a token, a password, a key body, or an internal
address.**

### 10.1 Rotation is the half that gets forgotten

Recovery procedures are usually well documented. Rotation almost never is. A
credential system without rotation is not an asset, it is a liability that grows.

Track when material last changed, define the procedure for suspected exposure,
and check key age on a schedule: `vat doctor --secret-max-age 90`.

---

## 11. Failure handling

| Situation | Never do this automatically | Do this |
| --- | --- | --- |
| dirty working tree | stash, reset, checkout | report, advance nothing |
| remote mismatch | rewrite the remote | stop; treat as supply-chain or path error |
| diverged branch | merge or rebase | a human judges the branch's intent |
| local ahead | push | confirm the owner and the release scope |
| source revision drift | delete the claim | mark it for re-check |
| conflicting search results | take the highest similarity | re-verify against the canon |
| partial index update | promote it to stable | discard staging or restore backup |
| credential mismatch | harvest production back into the canon | fix the cause at the canon |
| repeated verification failure | delegate again forever | stop after a correction budget |
| an external write is needed | act because the role allows it | require explicit approval and a gate |

---

## 12. Measuring whether any of this works

A checklist measures whether people performed rituals. These measure whether the
rituals produced the effect they were for:

- **review queue length, over time** — the leading indicator. If it only grows,
  knowledge is being written faster than it is verified.
- **median claim age** — how stale the typical current-state claim's evidence is.
- **lint findings** — how far the workspace is from its own declared rules.
- **open and stale changesets** — cross-repository work with no closing evidence.
- **rework rate** — how often work reported as done did not survive verification.

`vat metrics --record` keeps a local ledger, because a single reading says
little and the direction of travel says everything.

---

## 13. Adoption, and the break-even point

Every layer is overhead until the problem it solves is real. One developer with
two repositories who adopts a knowledge repository, a credential repository, and
cross-repository changesets has bought ceremony and no benefit, and will abandon
all of it within a month.

| Layer | Adopt when |
| --- | --- |
| workspace | 3+ repositories worked in together |
| harness | agents work across more than one repository |
| changesets | 2+ interfaces cross a repository boundary |
| brain | a decision was already lost, or 2+ people across 4+ repositories |
| credential | secrets live in 2+ places |

The order matters. Adopting the knowledge layer before the workspace is stable
produces records about a state nobody can reproduce. Adding semantic search
before canonical ownership produces fast answers with no way to tell which one
is true.

`vat fit` gives this verdict for your actual situation.

---

## 14. Anti-patterns

**"They are in one folder, so it is one repository."**
Each `.git` is a separate repository. Commits, branches, and tags are per
repository.

**"The top-level agent rules apply everywhere automatically."**
That depends on the tool and where the session started. Read the target
repository's contract explicitly.

**"Reading everything makes the answer more accurate."**
It makes it less accurate. Old history and current projection blur, and the
agent cites what is no longer true.

**"The index remembers, so the source is unnecessary."**
Semantic search misses, returns stale chunks, and confuses similar things. The
revision and the original text are what settle it.

**"Merge the agent's journal into the canon automatically."**
An agent's observations, inferences, and errors become permanent organisational
fact. Promotion is reviewed.

**"Updating means every repository ends on the latest main."**
That is how local work disappears. Safe updating respects dirty, branch, ahead,
and diverged.

**"The credential repository is private, so plaintext is fine."**
Private repositories leak through account compromise, mistaken sharing, logs,
forks, and backups.

**"The role is senior, so it can deploy."**
Judgement authority and mutation capability are different things. Deployment,
payment, purchasing, and production writes are each separately approved.

---

## 15. Twelve sentences

1. A workspace is a control plane, not a code repository.
2. Repositories in one folder are still independent. `git -C` is the boundary.
3. Being able to read every repository is not permission to write to any of them.
4. The root contract is a map; each repository's contract is the working permit.
5. Updating never discards local work.
6. A remote pointing somewhere else is a supply-chain signal, not a convenience.
7. A revision moving is a reason to re-check, not proof something broke.
8. Detail lives in atomic records; summaries are projections, and regenerable.
9. Decisions are superseded, never rewritten.
10. Secrets are ciphertext in git; the key and the authority to apply it are separate.
11. Retrieval is derived. It never outranks the canon, and it never carries instructions.
12. No automation collapses reading, judging, and acting into one permission.

Tools change. Canonical ownership, evidence by revision, progressive reading,
fail-closed updating, and review-gated promotion do not.
