---
name: decision-records
description: Use when the architect settles a trade-off whose consequences outlive the session (a contract, a schema, a deploy topology, an admission or consistency rule), when a plan's decisions are chosen or its work lands, when asked to record or supersede a decision, or when asked to review the decisions of an arc ("why did we choose X", "what did we decide in the waitroom work"). Drafts records in docs/decisions/, confirms them with evidence, and prints an arc's review table.
---

# Decision records

The format, the lifecycle and the one rule live in `docs/decisions/README.md`.
Read it before writing a record; this file is only the procedure.

## Does it earn a record?

The README's test: a choice a competent engineer could have made differently,
whose consequences outlive the session. A flag, a local name or a test's shape
does not; a contract, a datastore, a deploy topology or an admission rule does.
When unsure, write it — a thin record costs less than a lost reason.

## When the call is made — draft it, `proposed`

1. Next number: the highest `docs/decisions/NNNN-*.md` plus one. Never reuse one.
2. The review row first. If `Question | Chose | Instead of | Cost` will not fill
   in one line each, the decision is not yet clear enough to record.
3. Options: every one that was genuinely on the table, including the one not
   taken, each with its cost.
4. Decision, by the one rule — never a paraphrase:
   - the architect's words, quoted;
   - a delegation, quoted as one: `Delegated: "<their words>"`;
   - an approach that came inside an approved plan and was never put as its own
     choice: say exactly that;
   - reconstructed later: say so, and from which plan or commits.
5. `Arc:` — a short slug shared by every record of one line of work, and a link to
   its plan or design.
6. Commit it in the same change as the work it decides, not afterwards.

## When the work lands — accept it

Flip `proposed` to `accepted` and add `## Outcome`: the commits, the tests that
went red then green, the run that exercised it, and anything it found. An Outcome
that says only "done" is not evidence. After this a record changes only to gain a
`superseded by` pointer.

## When a decision is replaced — supersede it

Write the new record. In the old one, change only the status line to
`superseded by [NNNN](NNNN-<slug>.md) on <date>`. Never delete a record or edit
its reasoning.

## Link it

- The plan item that decided it links to the record: `→ [0006](../decisions/0006-….md)`.
  An advisory hook flags a `(Chosen)` option in a plan with no such link.
- If it settles a question in the decision register in the root `CLAUDE.md`, the
  row names the record as its owner.

## Regenerate the index

```bash
python3 docs/decisions/index.py          # rewrite the review tables in README.md
python3 docs/decisions/index.py --check  # what CI runs
```

Commit the regenerated README with the record. CI fails on a stale index.

## Reviewing an arc

Print the arc's rows from the README index. Answer from the records, not from
memory of the session. Point out every record whose Decision is a delegation or
"not put as a separate choice": those are the calls the architect has not yet
made their own.
