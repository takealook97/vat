# Adoption

> A staged path in, and an honest path out.

---

## First, decide what not to adopt

```console
$ vat fit
```

Every layer is overhead until the problem it solves is real. A solo developer
with two repositories who adopts a knowledge repository, a credential
repository, and cross-repository changesets has bought ceremony and no benefit,
and will abandon all of it within a month.

The order below is not decoration. Adopting the knowledge layer before the
workspace is stable produces records about a state nobody can reproduce.

---

## Stage 0 — write the ownership table

Before any tooling. One row per fact or asset that more than one person or
repository cares about:

| Fact or asset | Canonical repository | Consumers | Who approves a change | How it is verified |
| --- | --- | --- | --- | --- |
| the order API contract | payments | console | the repository owner | contract test |
| organisation-wide priority | brain | everyone | the organisation owner | review + lint |
| environment variables | credential | the deploy pipeline | the operations owner | decrypt + hash |
| an agent's journal | that agent's repository | the coordinator | its operator | review |

**A fact with no owner ends up duplicated in several documents, and then they
disagree.** If you write nothing else from this document, write this table.

You will find rows you cannot fill. Those are the real findings.

---

## Stage 1 — the workspace

*Threshold: three or more repositories worked in together.*

```bash
cd ~/work
vat init --adopt
vat status
vat doctor
```

`--adopt` enrols what is already there, reading each repository's origin and
current branch as they are. Nothing is moved or re-pointed.

Then correct what the guesses got wrong:

```yaml
repos:
  - name: payments
    role: product          # the guess is usually right; check it
    group: backend         # add these; they make every command selective
    checks:
      - make check         # add these; changesets depend on them
```

```bash
vat lint --fix
git add vat.yaml AGENTS.md CLAUDE.md .gitignore && git commit -m "chore: adopt vat"
```

Replace whatever loop you were using with `vat sync`. That alone stops the
category of accident where an update destroys uncommitted work.

**You can stop here.** For many teams this is the whole benefit.

---

## Stage 2 — the harness

*Threshold: agents work across more than one repository.*

The generated regions already exist from stage 1. What is left is the part only
you can write — above the region, in each repository:

- what this repository is responsible for, and what it is not
- what to read before editing
- which files must change together when a contract changes
- what proves the work is done
- what it must never do without explicit approval

Then define roles once and let the adapters be generated:

```bash
vat harness role new reviewer --description "Reviews a finished diff against its evidence."
vat harness role new planner  --description "Turns a goal into an ordered plan." --writes brain
vat harness render
```

Add `vat lint` to CI. Without it the contracts drift within a month and the
layer is decorative.

---

## Stage 3 — changesets

*Threshold: two or more interfaces cross a repository boundary.*

No setup. On the next cross-repository change:

```bash
vat changeset new "Move order cancellation to v2" --repos payments,console
# ... do the work ...
vat changeset verify CS-0001
vat changeset close CS-0001 --acceptance "cancel-then-refund passes end to end"
```

The value is not visible on the first one. It appears the first time someone
asks "what did we ship together, and how do we get back?" and the answer takes
ten seconds instead of an afternoon.

---

## Stage 4 — the brain

*Threshold: a decision was already lost, or two or more people across four or
more repositories.*

Start small. Do not import a wiki.

```bash
vat repo new brain --role brain --private
vat brain init
```

Record only what you would otherwise re-derive:

```bash
vat brain new decision --title "Orders own their own idempotency keys"
vat brain new gap --title "Retries can double-submit" \
                  --claim current-state --owner payments
```

Then, weekly:

```bash
vat brain sweep --apply && vat brain build && vat brain check
vat brain review
```

Twenty well-formed records beat two hundred unverified ones. The layer's value
is that everything in it is either citable or visibly not — a large repository
of unreviewed claims has no more authority than a wiki.

### Adopting an existing knowledge repository

```bash
vat brain adopt cortex
vat brain check
```

`vat` reads and reports; it never rewrites a record. Bring records up to schema
in the order the review queue suggests, not all at once.

---

## Stage 5 — credentials

*Threshold: secrets live in two or more places.*

```bash
vat repo new credential --role credential --private
```

The repository is created with ignore rules that refuse plaintext by default.

- Commit **ciphertext only**. A private repository is not encryption.
- Keep keys outside the repository, with a recovery recipient in a different
  failure domain.
- Test recovery on a machine that has never seen the keys. An untested recovery
  path is a plan, not a capability.
- Track rotation: `vat doctor --secret-max-age 90`.

---

## Keeping it working

**In CI**

```yaml
- run: vat lint
- run: vat doctor
```

**Weekly, wherever your team already meets**

```bash
vat brain sweep --apply
vat brain review --overdue
vat changeset list --open
vat metrics --record
```

The review queue length is the health indicator. If it only grows, knowledge is
being written faster than it is verified, and adding more records makes it worse
rather than better.

---

## Leaving

This matters, and most tools will not tell you.

Everything `vat` produces is plain text you already own:

| Artefact | What it is without `vat` |
| --- | --- |
| `vat.yaml` | a readable list of repositories and policy |
| `AGENTS.md` | a Markdown file with an HTML comment in it |
| `.agents/roles/*.md` | Markdown with YAML front matter |
| `.claude/`, `.codex/` | exactly the files those tools expect |
| brain records | one Markdown file per fact |
| `changesets/*.yaml` | readable YAML records |

Delete the binary and nothing breaks. The generated regions stop updating; the
files stay valid. No database, no lock-in, no export step.

That is deliberate. A tool that holds your knowledge hostage has the wrong
relationship to it — and a tool built on the principle that facts have canonical
owners should not make itself one.
