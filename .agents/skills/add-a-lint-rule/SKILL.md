---
name: add-a-lint-rule
description: Turn a described failure into a checkable vat lint or vat brain check rule.
---

# Add a lint rule

## When to use this

Somebody wants a convention enforced, or a failure has happened twice and should
not happen a third time.

## First, decide whether it is a rule at all

A rule that cannot be checked is not added. If the convention cannot become a
`vat lint` finding or a `vat doctor` finding, it does not belong in this tool —
say so and stop. Adding prose instead is how a tool that sells mechanical
enforcement ends up shipping advice.

Two more questions decide the shape:

- **Severity.** `error` is for a state that makes the workspace wrong;
  `warn` is for one that makes it worse. Warnings alone do not change an exit
  code, and every command that reports findings agrees on that.
- **Fixable.** A rule is `Fixable` only if repair is unambiguous and touches
  nothing a human wrote. Diagnosis and repair stay separate commands.

## Steps

1. Implement the check. Workspace rules live in `internal/lint`; knowledge-layer
   rules live in `internal/brain`. Collect the finding, never return early —
   these commands run in a loop while somebody cleans a repository, and one
   finding per run is unusable.
2. Register the name in the list its package keeps: `lint.RuleNames()` for
   `vat lint`, `brain.RuleNames()` for `vat brain check`. An unregistered rule
   cannot be selected with `--only`, so it is a rule nobody can ask for.
3. Add the row to the reference table: `docs/COMMANDS.md` for a lint rule,
   `docs/BRAIN.md` for a brain rule.
4. Write the test that fails without the rule.
5. `make check`.

## What catches you if you skip a step

These already exist and will go red on their own:

- `TestTheReferenceListsExactlyTheLintRulesThatExist` — the lint table matches
  `lint.RuleNames()` exactly, in both directions.
- `TestTheBrainReferenceListsExactlyTheRulesThatExist` — the same for
  `docs/BRAIN.md`.
- `TestTheReadmeQuotesTheRuleCountItActuallyHas` — the README states a number of
  rules, and a new rule changes it.

They are the reason step 2 and step 3 are not optional. Do not treat a green
suite reached by editing a table as proof; the rule still has to fire.

## When it must stop

If the failure cannot be detected from what vat can read — the manifest, the
working trees, the harness files, the records — it is not a rule. Report that
conclusion rather than approximating it with a heuristic that will produce false
findings in somebody else's workspace.
