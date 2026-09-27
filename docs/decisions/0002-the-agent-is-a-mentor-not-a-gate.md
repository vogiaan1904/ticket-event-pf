# 0002 — The agent is a mentor, not a decision gate

**Date:** 2026-09-22
**Status:** accepted. Supersedes [0001](0001-decisions-are-surfaced-then-owned.md).
**Arc:** working-agreement
**Where it lives:** `.claude/hooks/working-stance.md`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What is the agent's role in a decision? | A mentor: research, verify, explain briefly, name every trade-off; no gate | Enforcing 0001 harder, or dropping the protocol | Ownership rests on explanations good enough to argue with, not on a check |

## Context

[0001](0001-decisions-are-surfaced-then-owned.md) chose teach-back against the
broadest scope: every trade-off gated, the architect restating the reasoning in
one line before implementation, that line recorded verbatim.

It did not survive first contact. The teach-back step read as ceremony rather
than as a check, and its record sat with a `pending` rationale because the ritual
asking for that rationale was itself the part that did not land. A protocol whose
first application stalls on its own paperwork is not a protocol.

The underlying goal did not change: the architect owns the architecture. What
changed is the belief that a gate produces ownership. What produces it is
explanation good enough to argue with.

## Options

**1. Keep 0001 and enforce it harder.** Add the hook, insist on the teach-back.
Risks turning every decision into a quiz and being abandoned entirely.

**2. Mentor stance.** The agent researches, verifies against the repo, and
explains — short, and filtered for what is scarce for an engineer in 2026 rather
than for what is merely true. Trade-offs are still named, never made silently.
No gate, no teach-back.

**3. Drop the protocol.** Return to recommendation-then-approval. Rejected: that
is the failure 0001 was written against, and it has not stopped being one.

## Decision

Option 2.

> "i want you as a mentor for me, when i have problems to ask you, you just
> analyze, research and explain to me as usual, but instead of answering too
> long, you must know what is valuable for a SWE pattern in this 2026 SWE with
> AI era."

## Consequences

The stance lives in `.claude/hooks/working-stance.md` and is injected by a
`UserPromptSubmit` hook on every message, because a file read once at session
start demonstrably fades — the session that produced this record drifted from
an always-loaded instruction inside an hour.

What survives from 0001: trade-offs are named with the option not taken and what
it costs, and architecture calls belong to the architect. What is gone: the
gate, the teach-back, and the requirement that a record carry the architect's
verbatim rationale. This record carries one because it was given unprompted,
which is the form that was always going to work.

The filter is the load-bearing part. "Short" alone produces thin answers; short
*and* selected for judgment, verification, evidence and failure modes is the
thing worth having when code itself is cheap.
