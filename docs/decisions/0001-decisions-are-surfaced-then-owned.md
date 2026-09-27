# 0001 — Decisions are surfaced, chosen by the architect, and recorded

**Date:** 2026-09-22
**Status:** superseded by [0002](0002-the-agent-is-a-mentor-not-a-gate.md) on 2026-09-22
**Arc:** working-agreement

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How are decisions made between the architect and the agent? | Options, then the architect restates the reasoning before any work (teach-back) | Options with no recommendation, or with the agent's lean marked | A stop on every trade-off; abandoned the same day |

## Context

This platform is built by one engineer working with an AI agent. Over the
sessions preceding this record, the agent researched the options, weighed them,
arrived at a recommendation, and asked for approval. Approval was given each
time.

That produces correct code and no ownership. A decision reached by someone else
and approved is a decision you can describe but not defend: the constraint that
produced it was never held. The test is an interview question — "why three CI
jobs instead of a matrix?" — answered by recitation rather than reasoning.

The failure is not that the agent has opinions. It is the **ordering**: a
recommendation arriving before the option space closes the question before it is
open.

## Options

**1. Options with no recommendation.** The agent presents forces and two or
three real options, then stops. No recommendation unless asked. Costs one
round-trip per decision; leaves the choice genuinely open.

**2. Options plus teach-back.** As above, and before implementation the
architect states the reasoning back in one line. If it does not come out, the
decision is not yet theirs. The only option that tests ownership rather than
asserting it. Slowest.

**3. Options plus the agent's lean, marked as such.** Both presented, the
recommendation flagged as the agent's with its reasoning exposed to attack.
Fastest; closest to the status quo, with the reasoning made visible instead of
assumed.

Rejected without being offered: *the agent gathers facts and does not propose at
all.* You cannot choose an option you have never heard of. Suppressing the
agent's view does not transfer the decision — it deletes it.

## Decision

Option **2, options plus teach-back**, scoped to **anything with a trade-off**
— any choice where a competent engineer could pick differently.

> _No rationale was ever recorded here._ The ritual that asked for it was the
> part of this decision that did not work, and [0002](0002-the-agent-is-a-mentor-not-a-gate.md)
> replaced it before the gap was filled. The empty field is the evidence.

## Consequences

Teach-back against the broadest possible scope is the heaviest combination
available, and taken literally it is roughly ten stops per session — enough that
the protocol would be abandoned within a week. It is therefore implemented in
two tiers, specified in `docs/design/decision-protocol.md`: trade-offs are
**surfaced** inline as they arise, and only decisions outliving the session are
**gated** behind the full stop and teach-back.

This makes the scope honest — nothing is decided silently — while spending the
round-trips where they buy something.

What has to stay true: the agent must keep surfacing options it expects to lose.
A protocol where only the agent's preferred option is ever named in full is the
original failure wearing the protocol's clothes.
