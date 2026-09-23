# 0011 — Decisions are recorded when made, and reviewed from a generated table

**Date:** 2026-09-23
**Status:** proposed
**Arc:** working-agreement — [plan](../plans/2026-09-23-decision-records.md)
**Where it lives:** `docs/decisions/index.py`, `.claude/skills/decision-records/SKILL.md`, `.claude/hooks/check-decision-records.py`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How does a decision reach a reviewable record? | The agent drafts it when the call is made and confirms it when the work lands; a review table is generated from the records | Recording when an arc closes, or only on request | One record per decision to write and keep current |

## Context

The waitroom arc made seven architectural calls and none reached this folder,
which held only [0001](0001-decisions-are-surfaced-then-owned.md) and
[0002](0002-the-agent-is-a-mentor-not-a-gate.md). They lived in plans, a service
`CLAUDE.md` and the register: findable, not reviewable. 0001 had tried a gate and
was replaced within a day.

## Options

When a record is written:

- **At the call, confirmed when the work lands.** Reasoning is captured while
  present; the Outcome turns intent into evidence.
- **When an arc closes.** Fewer touch-points; the reasoning is reconstructed, and an
  abandoned arc records nothing.
- **On request.** No automation; depends on remembering, the gap this closes.

How records are grouped: one per decision, one log per arc, or columns added to
the register in `CLAUDE.md` — which every session loads.

## Decision

Chosen from options the agent put, 2026-09-23: "Both, one source" for who reads
the records; "At decision, confirmed at close" for when; "A. One per decision" for
grouping; "Yes, all of it" for scope.

## Consequences

Three layers, none of them a gate: a trigger line in the stance injected on every
message, the `decision-records` skill, and an advisory hook on plan edits. CI
checks only that the index matches the records. A call made in chat with no plan
is covered by the stance line alone; the records will show whether that is enough.
