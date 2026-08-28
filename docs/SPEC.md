# The vat formats

> What another tool must know to read, write, and be trusted with a vat
> workspace.

This is the normative half of the documentation. `METHODOLOGY.md` argues for the
operating model, `COMMANDS.md` describes one implementation of it, and this file
specifies the files on disk so that implementation is not the only one possible.

A knowledge layer whose whole claim is that it outlives the tool that wrote it
has to be readable by a tool that is not `vat`. That is the entire reason this
document exists.

---

## 1. Conformance

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY**
are to be interpreted as described in RFC 2119.

An implementation is **conforming** when it reads and writes these formats as
specified. Nothing requires it to implement every command, or any command; a
tool that only reads the knowledge layer is conforming for that format.

Every format here is versioned independently. An implementation that meets a
version's requirements **MUST** be able to read anything another conforming
implementation writes at that version.

Prose is the specification; a machine-readable schema is published beside it for
the readers that are not people:

| Format | Schema |
| --- | --- |
| workspace manifest, version 1 | [`schemas/vat-manifest-v1.schema.json`](../schemas/vat-manifest-v1.schema.json) |
| completion record, version 1 | [`schemas/vat-changeset-v1.schema.json`](../schemas/vat-changeset-v1.schema.json) |
| knowledge record front matter, schema 1 | [`schemas/vat-brain-record-1.schema.json`](../schemas/vat-brain-record-1.schema.json) |

A schema is a projection of this document, not a second source of truth. Where
the two disagree, this document is the specification and the schema has a bug.
Manifests and completion records carry a `yaml-language-server` modeline naming
their schema, so a file can be validated by a tool that has never heard of
`vat`. Front matter inside Markdown carries none, because no reader looks for
one there.

## 2. What this does not specify

Being explicit about the boundary is the difference between a specification and
a description of a program.

| Not specified | Why |
| --- | --- |
| command names, flags, output, exit codes | those are `vat`'s interface, not the format's |
| how a workspace is stored beyond the files below | a workspace is a directory of directories; nothing else is required |
| the prose inside a record | the whole point is that a human wrote it |
| how checks are run, or in what order | a conforming tool may run them any way it likes |
| retrieval, ranking, or embedding | see §5.7 |

## 3. Common rules

1. Every file described here is **UTF-8** text.
2. Every file **MUST** be writable atomically — written to a temporary file in
   the same directory and renamed. A reader **MUST** tolerate finding a
   temporary file and **MUST NOT** treat it as content.
3. Dates are `YYYY-MM-DD`. Timestamps are RFC 3339 **with an explicit offset**,
   which may be local. A reader **MUST NOT** assume `Z`. Both are strings, and
   a writer **MUST** quote them: unquoted, `2026-08-11` is a timestamp to any
   YAML 1.1 reader and a string to a YAML 1.2 one, so two conforming
   implementations would disagree about the type of the same field.
4. A revision is a git object name, full or unambiguously abbreviated — at
   least seven hexadecimal characters. A branch name is not a revision: a
   branch moves and takes the evidence with it, which is the half of this rule
   that matters.
5. No file described here **MAY** contain a secret. Implementations that surface
   credential state **MUST** limit themselves to existence, permissions, and
   age.
6. An identifier used to build a path **MUST** be validated before it is joined
   to one, and an implementation **MUST** refuse to write outside the workspace
   root. The permitted set differs by identifier, because they are joined into
   different things:

   | Identifier | Permitted | Length | Also |
   | --- | --- | --- | --- |
   | repository name | letters, digits, `-`, `_`, `.` | ≤ 100 | **MUST NOT** begin with `.` |
   | brain record id | letters, digits, `-`, `_`, `.` | ≤ 64 | **MUST NOT** begin with `.` |
   | changeset id | `CS-` followed by digits | — | the whole identifier is the pattern |
   | role or skill name | letters, digits, `-`, `_` | ≤ 64 | no `.`: it is joined into a runtime's own directory layout |

---

## 4. The workspace manifest — version 1

One file, `vat.yaml`, at the workspace root. Its presence is what makes a
directory a workspace; discovery **SHOULD** search upward from the working
directory.

```yaml
version: 1

requires:
  vat: ">=0.3.0 <0.4.0"   # optional

workspace:
  name: acme
  default_branch: main
  remote_template: https://github.com/acme/{name}.git   # optional
  description: Ordering and the console that operates it.  # optional

policy:
  sync:      { fast_forward_only: true, allow_autostash: false, allow_auto_push: false, parallelism: 8 }
  trust:     { canonical: [brain], semi_trusted: [workspace-repos], untrusted: [web, model-output] }
  brain:     { repo: brain, stale_after_days: 90, review_sla_days: 30, require_promotion_gate: true }
  changeset: { max_open_days: 14, require_rollback_point: true }
  gates:     { deploy: manual, external_write: manual, brain_promote: manual }

repos:
  - name: payments
    origin: https://github.com/acme/payments.git
    role: product
    required: true
    checks: [make check]
```

### 4.1 Fields

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | `1` |
| `requires.vat` | no | version constraint on the tool operating this workspace (§4.4) |
| `workspace.name` | yes | identifies the workspace |
| `workspace.default_branch` | yes | the branch used when a repository declares none |
| `workspace.remote_template` | no | expands `{name}`; the placeholder is required when present |
| `workspace.checks` | no | canonical commands proving the control plane itself is healthy (§6.5) |
| `repos[].name` | yes | unique; also the directory name unless `path` says otherwise |
| `repos[].origin` | yes | the remote this repository is fetched from |
| `repos[].role` | yes | one of §4.2 |
| `repos[].path` | no | directory under the root; defaults to `name` |
| `repos[].group` | no | free-form label for selecting a slice of the workspace |
| `repos[].default_branch` | no | overrides the workspace default |
| `repos[].required` | no | a missing clone is an error rather than a warning |
| `repos[].access` | no | `public` or `private`; metadata |
| `repos[].checks` | no | the canonical commands that prove this repository is healthy |
| `repos[].archived` | no | kept for the record, excluded from updates |
| `repos[].description` | no | free text |

### 4.2 Roles

`product`, `brain`, `credential`, `docs`, `agent`, `infra`.

At most **one** repository **MAY** have role `brain`.

Two repositories **MUST NOT** declare the same `origin` *and* resolve to the
same branch: they would fetch and push over each other, and nothing else here
would report it. Sharing an origin across **different** branches is a
worktree-per-branch layout and is permitted.

### 4.3 What an implementation must not do

These are safety properties, not preferences. A tool that breaks them is not
conforming, however useful it is.

- It **MUST NOT** rewrite a repository's remote to resolve a mismatch. A
  mismatch is a supply-chain signal.
- It **MUST NOT** discard or stash uncommitted work to make an update succeed.
- It **MUST NOT** write a credential into the manifest. An `origin` carrying
  userinfo **MUST** be rejected or stripped before it is recorded.
- It **MUST NOT** commit a governed repository into the workspace's own history.
  The root `.gitignore` **MUST** exclude every governed path.

### 4.4 Requiring a version of the tool

`version` states which file format this is. It does not state which commands
exist, what they print, or what they refuse to write, and those are what a
workspace comes to depend on: two tools reading the same `version: 1` may treat
the same file differently.

`requires.vat` is a constraint on the tool. It is space-separated comparison
terms that **MUST** all hold, with the operators `>=`, `>`, `<=`, `<`, `=` — for
example `">=0.3.0 <0.4.0"`. Caret and tilde ranges are not part of this format
and an implementation **MUST** refuse them rather than ignore them: a constraint
that cannot be parsed permits every version, which is worse than declaring none.

An implementation **MUST** refuse to operate a workspace whose `requires.vat` it
does not satisfy, and the refusal **MUST** name both the constraint and its own
version. Where its own version cannot be read — a build carrying no stamp — it
**MUST NOT** treat the constraint as violated: the requirement cannot be
evaluated in either direction, and the workspace is no worse off than before the
field was written.

A prerelease sorts before the release it leads to: `0.3.0-rc1` does not satisfy
`>=0.3.0`. Build metadata after `+` takes no part in the ordering.

A `git describe` suffix is not a prerelease and **MUST NOT** be read as one:
`v0.3.0-4-g2ad652e` and `v0.3.0-dirty` name builds *after* `v0.3.0`, carrying
everything that release carries, and reading them as candidates for it would
refuse the build somebody is running from the default branch.

A tool too old to know this field refuses the manifest outright, because
unknown keys are rejected. That is the same answer for a worse reason, and it is
why the field is safe to add to version 1.

---

## 5. The knowledge layer — schema 1

A directory, identified by a `.brain` marker file, holding atomic records and
generated projections.

```
brain/
  .brain              marker; carries `schema: 1`
  goals/  gaps/  decisions/  memory/     atomic records, one Markdown file each
  archive/            records that have reached an end state
  CURRENT.md          generated index
  graph.json          generated relationship graph
```

### 5.1 The marker

An implementation writing a brain **MUST** create the marker, and it **MUST**
contain a line `schema: <integer>` for schema 1 and later. A marker with no
`schema:` line predates versioning and **MUST** be read as schema 1.

A reader **SHOULD** also recognise a directory that has the record directories
but no marker: brains predate the marker, and refusing to read one loses the
records the format exists to keep.

A reader encountering a schema it does not implement — greater than its own, or
a value it cannot parse — **MUST** report that fact and **MUST NOT** present the
records as a complete picture. Reading a newer brain and reporting it sound is
the failure mode this version number exists to prevent: the records look clean
because half of what governs them is invisible.

Reporting rather than refusing is deliberate. A reader that refuses outright is
useless to somebody trying to find out what they are holding, and this layer's
whole claim is that the files remain readable. What it may not do is stay
quiet.

### 5.2 A record

YAML front matter, then Markdown prose. The prose is the record; the front
matter is what a tool may reason about.

```markdown
---
id: D-0001
status: active
date: 2026-03-14
claim_kind: intent
owned_by: payments
source_ref: payments@a71c93d0e5f218:docs/orders.md
observed_at: 2026-03-14
reviewed_by: alex
refs: [O-0001]
---

# D-0001 — Orders own their own idempotency keys
...
```

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | yes | unique across the whole brain |
| `status` | yes | one of §5.3 |
| `date` | no | when the record was written |
| `claim_kind` | see §5.4 | `current-state`, `historical`, or `intent` |
| `owned_by` | see §5.4 | the repository or system canonical for the fact |
| `source_ref` | see §5.4 | `<repo>@<revision>[:<path>]` |
| `observed_at` | see §5.4 | when the claim was last verified against its source |
| `revalidate_on` | no | what should trigger a re-check |
| `supersedes` / `superseded_by` | see §5.5 | the replacement chain |
| `refs` | no | related record identifiers |
| `axis` | no | groups goals into themes |
| `reason` | see §5.3 | why a record was quarantined or revoked |
| `reviewed_by` | no | who promoted it |

The filename **SHOULD** begin with the identifier. The filename is **not**
authoritative: `id` is.

### 5.3 Status

| Status | Meaning |
| --- | --- |
| `provisional` | written but not reviewed. **MUST NOT** be cited as settled |
| `active` | reviewed and citable |
| `stale` | was active; its observation aged past the policy window |
| `quarantined` | trust is in doubt. `reason` **MUST** be present |
| `superseded` | replaced. `superseded_by` **MUST** be present |
| `revoked` | withdrawn. `reason` **MUST** be present |
| `resolved` | a gap that was closed. Valid **only** for gaps |

`superseded`, `revoked`, and `resolved` are **end states**. An implementation
**MUST NOT** return a record from an end state to `active`.

A reader that does not understand a status **MUST** report it rather than
ignoring it. An unknown status means no rule governs the record.

### 5.4 Provenance

A record with `claim_kind: current-state` asserts something is true **now**, and
therefore decays. For such a record:

- `owned_by` **MUST** be present.
- `source_ref` **MUST** be present and **MUST** name a revision, not a branch.
- `observed_at` **MUST** be present.
- Once `observed_at` is older than the policy window, an implementation **MUST**
  report the record as aged out and **MUST NOT** present it as verified.
  Demoting it to `stale` on disk **MAY** require an explicit action, so that a
  read-only reader never rewrites records. It is the age test, not the stored
  status, that decides whether a claim may be cited as current.

`historical` records what happened and does not decay. `intent` records what the
organisation means to do. Neither requires provenance.

This is the load-bearing rule of the whole format. A claim about the present
with no evidence and no expiry is how a knowledge repository becomes a confident
liar.

### 5.5 Supersession

`supersedes` and `superseded_by` **MUST** agree at both ends: if `B` supersedes
`A`, then `A.superseded_by == B.id` and `B.supersedes` contains `A.id`.

A decision is **never** rewritten to reflect a change of mind. A new decision
supersedes it. The chain is the record of how thinking moved.

Implementations **MUST** detect and report a cycle.

### 5.6 Promotion

Becoming `active` is a **gate**, not a transition a writer may take on its own.
When `policy.brain.require_promotion_gate` is set, an implementation **MUST
NOT** let a record reach `active` except through an explicit promotion, and
**MUST** refuse to promote a `current-state` claim that lacks `owned_by` or
`source_ref`.

Re-dating a record's `observed_at` is an assertion that somebody re-read the
source. An implementation **MUST NOT** move `observed_at` forward automatically
when the source revision has changed.

### 5.7 Generated projections

`CURRENT.md` and `graph.json` are **derived** and **MUST NOT** be authoritative.
A conforming tool **MAY** regenerate them at any time and **MUST** treat a hand
edit as drift rather than as content.

Every generated projection **MUST** carry its own provenance, so that a tool can
tell a projection it produced from a file that merely holds the same name:

- `CURRENT.md` **MUST** contain this line:

  ```text
  <!-- Generated by `vat brain build` from the atomic records. Do not edit. -->
  ```

- `graph.json` **MUST** carry the top-level field `generated_by`, whose value is
  the string `vat brain build`.

A tool **MUST NOT** overwrite a file at either name that is non-empty and does
not carry that provenance. A knowledge repository that predates this
specification usually keeps a current-state document of its own, and it is
typically the most valuable file in the repository; adoption that renders over
it destroys the thing being adopted. Such a file is not drift — no regeneration
can repair it — and an implementation **SHOULD** report it as its own finding,
distinct from drift, naming a remedy the reader can act on.

An empty file at either name carries no claim and **MAY** be written over: it is
what an interrupted write leaves behind.

`CURRENT.md` **SHOULD** route to the maintained root views that exist beside the
records, including current state, goals, gaps, execution order, decisions,
reviewed observations, and the agent operating model. An adopted repository
**MAY** call its current-state view `PORTFOLIO_STATUS.md`; a reader that supports
adoption **SHOULD** recognise that name when `STATUS.md` is absent. These links
do not make the views atomic canon: they keep the generated entry point from
stranding the synthesis documents it tells readers to use.

A retrieval or embedding layer built over a brain **MUST NOT** be treated as an
authority on what is currently true. It finds records; §5.3 and §5.4 decide
whether they may be cited.

---

## 6. The completion record — version 1

One YAML file per record, under `changesets/`, named `CS-NNNN.yaml`.

```yaml
id: CS-0001
objective: Move order cancellation to v2
status: closed
opened_at: 2026-08-11
closed_at: 2026-08-14
integration_acceptance: cancel-then-refund passes end to end
repositories:
  - name: payments
    rollback_point: 3f9a1c2e8b7412
    revision: a71c93d0e5f218
    branch: main
    checks:
      - { command: make check, status: pass, ran_at: 2026-08-14T09:12:44Z, revision: a71c93d0e5f218 }
    landed_on: origin/main
    landed_at: 2026-08-14
    review: https://github.com/acme/payments/pull/318   # optional evidence
```

`status` is one of `open`, `verified`, `closed`, `rolled_back`, `abandoned`.

`opened_at` and `closed_at` **MUST** be written as `YYYY-MM-DD`. An
implementation that cannot read the date cannot compute the age, and an age
reported as zero is indistinguishable from a changeset opened today — so a
malformed date silently exempts the record from every staleness check.

### 6.1 The three claims

A changeset makes three claims that **MUST** be recorded separately, because
they are separately falsifiable and collapsing them is the failure this format
exists to prevent.

| Field | Claim |
| --- | --- |
| `checks[].status` at `checks[].revision` | these revisions **passed their checks together** |
| `landed_on` / `landed_at` | that revision **reached the branch its repository ships from** |
| `integration_acceptance` | the pieces **work together end to end** |

An implementation **MUST NOT** infer any of the three from another.

### 6.2 Landing

`landed_on` names a ref the recorded `revision` was observed to be an ancestor
of. The test **MUST** be reachability in git — equivalent to
`git merge-base --is-ancestor <revision> <ref>` — and **MUST NOT** depend on any
forge's API.

`review` is evidence and **MUST NOT** be treated as a gate. Forges model review
differently, and an open review is precisely the state of not having landed.

An implementation **MUST** clear a previous landing observation before recording
a new one. Landing is a claim about now; a stale one on a rewritten branch is
the worst thing this record can say.

### 6.3 Rollback

`rollback_point` **MUST** be captured when a repository is enrolled, not when
the change completes: after the change lands it can no longer be observed.

A generated return plan **MUST** be ordered in reverse enrolment order, so no
window exists in which a consumer expects an interface that is already gone.

### 6.5 The control plane as a participant

`repositories[].name` names a repository in the manifest, with one exception:
the name `.` denotes the workspace root itself.

A contract change usually begins in the control plane — the manifest, a shared
role, a generated region every repository reads — and a record naming only the
products omits the revision that describes what the others were verified
against. That is the one revision a rollback most needs.

The workspace root cannot appear in `repos:`, so an implementation supporting
this **MUST** read its checks from `workspace.checks` and **MUST** treat
`workspace.default_branch` as the branch it ships from. Every other unresolvable
name **MUST** stay an error: `.` is the exception, not a general escape, and the
manifest already refuses a repository name beginning with `.` so the two cannot
collide.

An implementation **MUST NOT** treat the workspace as verified because it
declares no checks — the same rule §6.1 states for any participant. The record
lives under `changesets/` in that same root, so writing it dirties the tree the
checks describe; the record **MUST** be committed before the workspace can be
verified, exactly as for any other participant.

---

## 7. The agent harness

```
.agents/roles/<name>.md              canonical role contract
.agents/skills/<name>/SKILL.md       canonical procedure
.claude/agents/<name>.md             generated adapter
.claude/skills/<name>/SKILL.md       generated adapter
.codex/agents/<name>.toml            generated adapter; `-` in the name becomes `_`
```

Whether generated content has drifted **MUST** be decided on content and not on
line endings. git converts LF to CRLF on checkout under its default
configuration on Windows, so a byte comparison reports every generated file in
the workspace as drifted, on every run, on a file nobody has touched — and a
repair that rewrites it produces the same finding on the next run.

A generated region **MUST** fit the budget the implementation itself enforces on
the file holding it. Where a roster is too long to list, the implementation
**MUST** state how many entries it did not list rather than dropping them
silently: a reader given a partial list and no count concludes it is the whole
one.

An implementation **MUST NOT** silently overwrite a manifest that changed
between reading it and writing it. Every command that changes the manifest reads
the whole file, edits what it read, and writes the whole file back, so two of
them running together otherwise leave only the later one's work — and both
report success. The conflict **MUST** be reported and the write refused; merging
is a guess about whether the other change belongs beside this one.

An origin or a branch handed to git **MUST NOT** begin with `-`: git reads such
a value as an option, and the manifest that carries it is committed and reaches
every colleague.

An implementation **MUST NOT** treat a bare user name in an SSH URL as a
credential. `ssh://git@host/path` is the form every forge publishes and the user
there is a login name; a credential is a password, or userinfo over http where
that is how a token is carried.

A name that becomes a directory or a file — a repository, a role, a skill —
**MUST NOT** be one a supported platform cannot create. An implementation
**MUST** refuse the Windows device names (`con`, `prn`, `aux`, `nul`, `com0`
through `com9`, `lpt0` through `lpt9`, with or without an extension) and a name
ending in `.` or a space, on every platform and not only where the filesystem
objects. Two names that differ only in case **MUST** be refused for the same
reason: they are one directory on macOS and on Windows. A workspace description
that is valid only on the machine that wrote it is not a shared description.

A canonical file holds the prose. An adapter holds **discovery metadata and a
pointer**, and **MUST NOT** contain a copy of the prose. Two copies of a
contract that disagree by one line are worse than one copy nobody has read,
because both look authoritative.

An implementation **MUST** be able to report an adapter that no longer matches
what its canonical file would generate.

An implementation **MAY** seed canonical procedures when a workspace is created.
A seeded file is canonical and belongs to the workspace: an implementation
**MUST NOT** rewrite one that already exists, and removing one **MUST** be
without consequence. Anything seeded is therefore ordinary content under
`.agents/skills/`, distinguished by nothing on disk.

A definition **MAY** name the runtimes it targets; naming none targets every
runtime that has an adapter of that kind. The two kinds do not share one set: a
role has an adapter for Claude Code and for Codex, a skill for Claude Code only,
because Codex discovers a skill through the canonical directory itself.

An implementation **MUST** report a `runtimes:` value that selects no adapter of
the kind it appears on — including a runtime the implementation otherwise
supports. Checking both kinds against one list leaves `runtimes: [codex]` on a
skill correct in spelling, unsupported in effect, and silent: no adapter means
no drift to report, so the definition generates nothing while every other check
passes.

### 7.1 Models are per runtime

A model name belongs to one vendor's namespace. An implementation **MUST NOT**
write a model name into an adapter for a runtime whose namespace does not
contain it, and **SHOULD** omit the field rather than guess — a runtime falls
back to its own default, but fails on a name it cannot resolve.

### 7.2 Generated regions

Where a generated block lives inside a hand-written file, it **MUST** be
delimited by markers and an implementation **MUST NOT** modify anything outside
them.

### 7.3 Trust

A generated contract **MUST** state which content is data rather than
instruction. Search results, fetched pages, issue comments, and model output are
data. An implementation that generates agent contracts and omits this is not
conforming: it is the one instruction the agent cannot derive for itself.

---

## 8. Versioning

| Format | Current | Declared in |
| --- | --- | --- |
| manifest | 1 | `version:` in `vat.yaml` |
| brain | 1 | `schema:` in `.brain` |
| changeset | 1 | implied by `id` pattern `CS-NNNN` |

A version is incremented when a change would make an existing conforming reader
wrong.

The three formats differ on what "wrong" means, and an implementer has to know
which kind each one is:

| Format | Unknown keys | Adding an optional field |
| --- | --- | --- |
| brain record front matter | a reader **MUST** ignore them | not a version change |
| manifest | a reader **MUST** reject the file | a version change |
| completion record | a reader **MUST** reject the file | a version change |

The manifest and the completion record are **closed** on purpose. A mistyped
policy key that parses is a rule silently switched off, and a workspace whose
`fast_forward_only` never took effect because somebody wrote
`fast_foward_only` is worse off than one that refused to start.

Changes to this document are proposed the way any change to `vat` is: with an
issue naming a failure that already happened. See `CONTRIBUTING.md`.

---

## 9. Reference implementation

[`vat`](https://github.com/takealook97/vat) — Go, one dependency, MIT. Its test
suite asserts this document against the code: a rule stated here and unchecked
there is a defect in one of the two.
