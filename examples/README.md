# Examples

`workspace/` is a complete, minimal workspace: a manifest, the generated
workspace contract, a knowledge repository with three records and its generated
index, and one closed changeset.

Every file is the real shape `vat` reads and writes. Copy the pieces you need.

```bash
vat --workspace examples/workspace brain query idempotency
vat --workspace examples/workspace brain review
vat --workspace examples/workspace changeset show CS-0001
vat --workspace examples/workspace changeset undo-plan CS-0001
vat --workspace examples/workspace fit --contracts 2 --people 3
```

## Why `vat lint` fails here

Deliberately. The repositories this manifest names are not cloned — they do not
exist — so you see exactly what a workspace looks like before `vat sync` has run:

```console
FAIL  repo/missing · payments       not cloned
FAIL  workspace/gitignore-drift     5 governed repositories are not excluded by .gitignore
```

That is the point of the example. A workspace whose manifest describes
repositories nobody has cloned is a real state, and `vat` says so rather than
pretending otherwise.

## What to look at

| File | Shows |
| --- | --- |
| `vat.yaml` | every policy knob, with comments on the ones that are easy to get wrong |
| `AGENTS.md` | the generated workspace contract: roster, precedence, trust tiers, gates |
| `brain/decisions/D-0001-*.md` | a decision that records *why*, and what would make it worth revisiting |
| `brain/gaps/G-0001-*.md` | a **stale** claim — evidence pinned to a revision, aged past the window |
| `brain/goals/O-0001-*.md` | a goal written as an observation, so it can actually be judged |
| `brain/CURRENT.md` | the generated index, including the "needs attention" section |
| `changesets/CS-0001.yaml` | a closed cross-repository change with its return points |
| `.agents/skills/*/SKILL.md` | the two procedures `vat init` seeds — canonical, yours to edit |
| `.claude/skills/*/SKILL.md` | what a generated skill adapter is: front matter and a pointer, never a copy |

The stale gap is the most instructive file. It is not marked wrong — it is
marked *unverified*, with the exact revision and date it was last true.

The pair of skill files is the second. Open both: the canonical one holds the
procedure, and the generated one holds ten lines that only make it discoverable.
Editing the generated copy is the mistake `harness/adapter-drift` exists to
report.
