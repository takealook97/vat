# `vat.yaml`

The single declaration of which repositories a workspace governs, and under
which policy. Everything else reads the workspace shape from here.

Unknown keys are **rejected**. A typo in a policy key would otherwise silently
leave the default in place, disabling the very rule it was meant to configure.

```yaml
version: 1

workspace:
  name: acme
  default_branch: main
  remote_template: https://github.com/acme/{name}.git
  description: Ordering, fulfilment, and the console that operates them.

policy:
  sync:
    fast_forward_only: true
    allow_autostash: false
    allow_auto_push: false
    parallelism: 8

  trust:
    canonical: [brain, credential]
    semi_trusted: [workspace-repos]
    untrusted: [search-results, embeddings, web, issue-comments, model-output]

  brain:
    repo: brain
    stale_after_days: 90
    review_sla_days: 30
    require_promotion_gate: true

  changeset:
    max_open_days: 14
    require_rollback_point: true

  gates:
    deploy: manual
    external_write: manual
    brain_promote: manual

repos:
  - name: payments
    origin: https://github.com/acme/payments.git
    role: product
    group: backend
    default_branch: main
    required: true
    access: private
    description: Order state and the public API contract.
    checks:
      - make check

  - name: console
    origin: https://github.com/acme/console.git
    role: product
    group: frontend
    checks:
      - pnpm test
      - pnpm typecheck

  - name: brain
    origin: https://github.com/acme/brain.git
    role: brain

  - name: credential
    origin: git@github.com:acme/credential.git
    role: credential
    access: private

  - name: legacy-api
    origin: https://github.com/acme/legacy-api.git
    role: product
    default_branch: master
    archived: true
```

---

## `requires`

```yaml
requires:
  vat: ">=0.3.0 <0.4.0"
```

| Key | Meaning |
| --- | --- |
| `vat` | version constraint on the tool operating this workspace; optional |

`version: 1` says which file format this is. It does not say which commands
exist or what they refuse to write, so a workspace whose layout depends on a
behaviour — a projection left alone, a check that now fails — cannot express
that with the schema version alone. `requires.vat` can.

Space-separated comparison terms, all of which must hold: `>=`, `>`, `<=`, `<`,
`=`. Caret and tilde ranges are refused rather than ignored, because a
constraint nothing can parse permits every version. A prerelease sorts before
its release, so `0.3.0-rc1` does not satisfy `>=0.3.0` — but a `git describe`
suffix is not a prerelease: `v0.3.0-4-g2ad652e` is four commits *after* v0.3.0
and satisfies it.

A vat that does not satisfy the constraint refuses to open the workspace at all,
naming both versions — the same answer it gives to a manifest written against a
newer schema, and for the same reason: every command past that point would be
acting on rules it does not implement. A build whose own version cannot be read
(`dev`, which is what `go build` produces) is not refused; the constraint cannot
be evaluated either way.

---

## `workspace`

| Key | Meaning |
| --- | --- |
| `name` | required; identifies the workspace in generated contracts |
| `default_branch` | the branch `sync` fast-forwards when a repository does not declare its own |
| `remote_template` | expands `{name}` into an origin URL for `vat repo new`; the placeholder is required |
| `description` | rendered into the workspace contract |
| `checks` | the canonical commands that prove the control plane itself is healthy |
| `vocabulary` | renames the nouns vat uses in generated prose |

### `vocabulary`

```yaml
workspace:
  name: payments
  vocabulary:
    workspace: project        # default: workspace
    brain: knowledge base     # default: knowledge layer
```

A company that calls a bundle of repositories a "project" otherwise runs two
vocabularies in one team: its own, and the tool's. These nouns are substituted
into the generated contracts — `AGENTS.md` and the runtime adapters — which is
what its people and its agents actually read.

Display only. It never reaches `role: brain`, the `policy.brain.*` keys, the
name of a command, or a field in `--json` output. `vat.yaml` is a published
format other tools read by name, and a role that means something different per
organisation is a format that has forked.

Repository and directory names are not vocabulary: those are already yours, and
`vat repo rename` moves one after the fact. Changing a noun later is the same —
edit it, and `vat lint` reports the generated regions as drift until
`vat lint --fix` or `vat harness render` rewrites them.

---

`checks` here is the workspace's own, not a default for `repos[]`. The workspace
root is a repository too — it holds this file, the roles, the skills, and the
generated contracts every governed repository reads — but it is not in `repos:`
and cannot be. A changeset enrols it as the participant named `.`, and these are
the commands `vat changeset verify` runs against it.

---

## `policy.sync`

| Key | Default | Meaning |
| --- | --- | --- |
| `fast_forward_only` | `true` | never create a merge commit |
| `allow_autostash` | `false` | must stay false for the guarantee that updating never discards local work |
| `allow_auto_push` | `false` | must stay false; a local-ahead branch is a human decision |
| `parallelism` | `8` | concurrent git network operations |

---

## `policy.trust`

The boundary between content that may carry instructions and content that is
only ever data. It has to be written down to be enforceable.

| Key | Meaning |
| --- | --- |
| `canonical` | may state facts and constrain behaviour |
| `semi_trusted` | may state facts about themselves only |
| `untrusted` | data. never instructions, and no position in the precedence order |

Rendered into every generated contract. `vat lint` warns when `untrusted` is
empty, because a harness that never names untrusted sources cannot tell an agent
which text is data.

---

## `policy.brain`

| Key | Default | Meaning |
| --- | --- | --- |
| `repo` | the sole repository with `role: brain` | which repository holds the knowledge layer |
| `stale_after_days` | `90` | an active current-state claim is demoted once its observation is older than this |
| `review_sla_days` | `30` | how long a claim may sit unreviewed before the queue itself is reported as failing |
| `require_promotion_gate` | `true` | a canonical record needs an explicit promotion step |

Choosing `stale_after_days`: shorter than how fast your systems actually change,
longer than your team can realistically re-verify. Ninety days suits most teams;
a fast-moving platform may want thirty.

---

## `policy.changeset`

| Key | Default | Meaning |
| --- | --- | --- |
| `max_open_days` | `14` | report a changeset left open longer than this |
| `require_rollback_point` | `true` | refuse to verify a changeset whose repositories have no recorded pre-change revision |

---

## `policy.gates`

Each is `manual` or `auto`. They separate judgement authority from mutation
capability: a role that may decide something still needs the matching gate to
act on it.

| Key | Covers |
| --- | --- |
| `deploy` | releasing to any environment |
| `external_write` | writing to any system outside the workspace |
| `brain_promote` | making a claim canonical |

Rendered into the workspace contract so an agent can read its own boundary.

---

## `repos[]`

| Key | Required | Meaning |
| --- | --- | --- |
| `name` | yes | unique; letters, digits, `.`, `_`, `-` |
| `origin` | yes | the canonical remote URL |
| `role` | defaults to `product` | see below |
| `path` | no | directory under the workspace root; defaults to `name` |
| `group` | no | lets commands operate on a slice of the workspace |
| `default_branch` | no | overrides the workspace default |
| `required` | no | a missing clone fails `sync` and `lint` rather than warning |
| `access` | no | `public` or `private` |
| `checks` | no | the canonical commands that prove this repository is healthy |
| `archived` | no | kept in the record, excluded from daily commands |
| `description` | no | rendered into the workspace contract |

### Roles

| Role | Behaviour |
| --- | --- |
| `product` | code and implementation docs; warned when it declares no checks |
| `brain` | the knowledge layer; at most one per workspace |
| `credential` | encrypted secrets; scanned for plaintext and rotation age, never read |
| `docs` | documentation and sites |
| `agent` | an agent's own identity and journal, kept separate from the brain on purpose |
| `infra` | infrastructure definitions |

### `default_branch` matters more than it looks

A repository on `master` in a workspace defaulting to `main` is skipped by every
update forever, reported as "on another branch" — a note nobody reads. Declaring
it turns a silent skip into a real update. `vat lint` reports the mismatch, and
`vat init --adopt` records it for you.

### Names

A repository name may hold only letters, digits, `.`, `_`, and `-`, and may not
exceed 100 characters — the longest a git host accepts. It becomes a directory
name and a path segment in a remote URL, so it is checked before either is
built rather than when the manifest is saved.

### `origin` records identity, never access

An origin carrying a credential — `https://user:token@host/repo.git` — is
rejected, and so is a `remote_template` that carries one. Neither refusal quotes
the value back: an error message is not where a token surfaces. `vat.yaml` is committed, so a token pasted into it is published by the
next push of the workspace root. Keep it in your git credential helper; the
remote still works. `vat repo adopt` and `vat init --adopt` read a remote that
already has one and record it stripped rather than refusing, because they record
what they found rather than what you typed.

### The sync policy is checked, not merely declared

`fast_forward_only` must be `true`, and `allow_autostash` and `allow_auto_push`
must be `false`. `vat sync` provides all three unconditionally, so a manifest
declaring otherwise is stating something that will not happen; validation
rejects it. Omitting the keys takes the safe values.

### Two repositories may share an upstream, but not a branch of one

Tracking `main` in one directory and `release` in another is a worktree-per-
branch layout and is accepted. Two entries with the same `origin` *and* the same
branch are not: they would fetch and push over each other, and nothing else
reports it. A `remote_template` without `{name}` produces exactly that, which is
why the placeholder is required.

### `checks` are the contract with `changeset verify`

`vat changeset verify` runs exactly these commands and records the result
against the revision it ran on. Without them a changeset has nothing to verify,
which `vat lint` reports.

Keep them fast enough to run often and complete enough to be believed.

---

## Migrating from a two-column manifest

```bash
vat init --from-tsv config/repos.tsv
```

Reads `name<TAB>origin`, guesses roles from names, and writes a full manifest.
Review the guesses, add `checks` and `group`, and run `vat lint`.
