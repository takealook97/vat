# FAQ

---

### Should I just use a monorepo?

Quite possibly. If your repositories share a release cadence, a stack, and an
access level, a monorepo is simpler and you should use one.

`vat` is for when they genuinely do not — different deploy cycles, different
languages, different people with different access — and you still need one
coherent view.

The strongest modern argument for a monorepo is about agents: can a model see
the type definition, its callers, its consumers, and their tests at once? The
answer for a `vat` workspace is yes, because a filesystem boundary is not a git
boundary. Everything is visible under one root; only history and release
responsibility stay separate.

**What you genuinely lose is cross-repository CI.** Nothing stops a contract
change from breaking a consumer before it merges. `vat changeset` does not
restore that. It makes the combination that was verified a matter of record
rather than of memory, which is a real reduction of the loss and not its
elimination. If that loss is intolerable for your work, use a monorepo.

### Why not git submodules?

Submodules fail badly in agent workflows: detached HEADs, forgotten pointer
commits, and a two-stage commit dance for every change. They also couple the
parent's history to the children's, which is the coupling this layout exists to
avoid.

Independent clones plus `git -C` is more robust and easier to reason about — and
`vat.yaml` gives you the one thing submodules genuinely provide, a declared list
of what belongs here.

### How is this different from `mu`, `meta`, `gita`, or a shell loop?

Those manage repositories. `vat` manages a workspace, which turns out to be a
different problem.

The multi-repo commands are the smallest part of this tool. What the others do
not have:

- an update state machine that refuses to lose your work, and reports why
- rules that run — manifest-to-`.gitignore` drift, contract drift, adapter
  drift, claim provenance
- generated agent contracts with a context budget
- a knowledge layer whose claims expire
- multi-repository completion records with a return plan
- a break-even advisor that tells you not to adopt half of it

If you only need parallel `git pull`, use a shell loop. It is fine.

### I only have one repository. Is this for me?

Yes, for two of the five layers.

`vat harness` solves problems that exist at a single repository: one role body
generated into every runtime you use rather than copied, a contract that holds
wherever a session was opened, and a written trust boundary saying that fetched
text is data. If you run Claude Code and Codex against the same code, their role
definitions are already drifting.

`vat brain` is worth it once agents work in the code weekly, because an agent
that re-derives a settled decision costs the same as a person who forgot it and
does so far more often.

The other three — the workspace layer, changesets, credential separation — are
genuinely about having several repositories, and `vat fit` will tell you to skip
them.

### Do I have to use the brain?

No. It is one layer of five, and `vat fit` will tell you to skip it until a
decision has already been lost or you have two or more people across four or
more repositories.

`internal/brain` imports neither the manifest nor git, so a workspace that never
adopts it pays nothing for it.

### Is the brain a RAG system?

No, and the distinction matters.

A retrieval system optimises for finding *relevant* text. The brain optimises
for knowing which text is *currently true* — a different question with a
different failure mode. Semantic search returns a confident answer from a
three-year-old chunk. The brain marks that chunk `stale` and refuses to cite it.

They compose well: point your retrieval layer at the brain, and let the brain
decide what is canonical. Retrieval stays derived, and never outranks the canon.
The contract for doing that is four rules — exclude `archive/` and `history/`,
do not let the index write its own summaries, never write anything back into a
record, keep the trail to the atomic record and its `source_ref` — and it is
written down in [BRAIN.md](BRAIN.md) rather than built into a command. `vat`
will never grow a subcommand named after a search product: a vendor adapter in
the core turns the tool into that vendor's document generator.

### Should I use one of the agent-memory tools instead?

For session memory, probably yes — and alongside, not instead. They answer a
different question.

Those tools capture what happened in a session and make it findable again. The
brain holds what the organisation reviewed and decided, and tracks whether it is
still safe to quote. A `memory` record here is not a session handoff and not an
agent's journal; it is a **reviewed, reusable observation** — recorded with the
situation that should bring it back and the condition under which it stops
applying. If it will not be useful a second time, it does not belong here.

The one thing that does not work is letting a session log become canon
automatically. Automatic capture is fine; automatic promotion is not, which is
why anything entering this layer enters as `provisional` and a claim about the
present cannot be re-dated without someone re-reading its source.

### What if my team will not maintain it?

Then adopt fewer layers. `vat fit` exists for this.

The workspace layer costs nothing ongoing: `sync`, `status`, `doctor`, `lint`.
The brain costs about fifteen minutes a week, and if nobody spends them the
review queue grows and `vat metrics` shows it growing. That visibility is
itself useful — it tells you to stop writing records rather than to write more.

A knowledge layer nobody maintains is worse than none, because it looks
authoritative. `vat` at least makes the decay measurable.

### Why does `sync` refuse to merge diverged branches?

Because merging guesses at intent. Diverged history means someone rewrote or
force-pushed, or two people worked on the same branch. Which resolution is
correct depends on what they meant, and `vat` cannot know it.

Reporting and stopping is the honest answer.

### Why does `doctor` not fix anything?

A tool that silently fixes what it finds teaches you nothing about why it broke.
On a machine holding credentials and unpushed commits, "fixing" is how work
disappears.

`vat lint --fix` does repair, and only what can be repaired without judgement:
it regenerates what is generated. It never edits a fact, a decision, or a
working tree.

### Can I use it with GitLab, Bitbucket, or a self-hosted host?

Yes. `vat` shells out to your `git`, so any host works.

Only `vat repo new` without `--remote` needs the GitHub CLI to create the remote
for you. Pass `--remote <url>` and it works anywhere; pass `--no-remote` and it
stays local.

### Does it work on Windows?

Yes. CI runs the suite on Linux, macOS, and Windows. Commands from `vat.yaml`
run through `cmd /C` on Windows and your `$SHELL` elsewhere.

### Why Go?

One static binary, no runtime to install, and trivial cross-compilation — which
matters for a tool people install before they have opinions about it.

Concurrency helps too: fetching sixteen repositories at once is a real
improvement over a sequential loop.

### Why only one dependency?

A tool that governs a workspace should not widen its attack surface. Everything
except YAML parsing is standard library, including the command router and the
completion generator.

Adding a second dependency needs an argument in the pull request.

### Can I add my own lint rules?

Not yet, and it is the most requested thing on the roadmap. The current rules
are compiled in because each one encodes a specific failure and its severity
mattered more than extensibility at the start.

If you have a rule that catches a real failure, open an issue describing the
failure — that is a strong case for adding it to the built-in set.

### What happens if I stop using `vat`?

Nothing breaks. Everything it produces is plain text you already own: Markdown,
YAML, and the exact files your agent runtimes expect. Delete the binary and the
generated regions simply stop updating.

There is no database, no export step, and no lock-in. See
[ADOPTION.md](ADOPTION.md#leaving).

### Why "vat"?

A brain in a vat: the vessel that keeps the mind alive and connected to a world
it cannot touch directly.

Your repositories are the body. `vat` is the vessel that keeps them coherent.
`vat brain` is the memory suspended inside it — deliberately separate from the
code, because knowledge that lives in code disappears with the code.
