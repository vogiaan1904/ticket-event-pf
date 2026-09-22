# Decision records

A design says how the platform should work. A plan says what was done on a day.
Neither says **who decided, and why they chose this over the alternative** — and
that is the part a reader cannot reconstruct from the code.

One file per decision, `NNNN-<slug>.md`, numbered in the order they were made.
Numbers are never reused and a superseded record is never deleted: it gains a
`Superseded by` line and stays, because the reasoning that was replaced is
itself evidence.

## Format

```markdown
# NNNN — <the decision, as a statement>

**Date:** YYYY-MM-DD
**Status:** accepted | superseded by [NNNN](NNNN-<slug>.md)

## Context
The forces. What was true that made this a question at all.

## Options
Each one that was genuinely on the table, with what it costs.

## Decision
The call, in the words of whoever made it.

## Consequences
What this makes easy, what it makes hard, and what has to be true for it to
keep holding.
```

## The one rule that matters

**The Decision section is written by whoever made the call, in their words.**
Not a summary of their answer. A record that paraphrases the decider is a record
of the paraphraser's reasoning, which is the thing this folder exists to avoid.

## What earns a record

A choice a competent engineer could have made differently, whose consequences
outlive the session that made it: a schema, a contract, a deploy topology, the
shape of CI, what is allowed on `main`.

Tactical calls do not: which flag to pass, how to name a local variable. Those
get surfaced in conversation and left there.
