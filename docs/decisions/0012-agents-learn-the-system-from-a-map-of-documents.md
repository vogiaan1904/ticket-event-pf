# 0012 — Agents learn the system from a map of its documents, not a copy of them

**Date:** 2026-09-23
**Status:** proposed
**Arc:** system-map — [plan](../plans/2026-09-23-system-map-skill.md)
**Where it lives:** `.claude/skills/system-map/SKILL.md`, `.claude/skills/system-map/scripts/check_map.py`, `.github/workflows/system-map.yml`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How does an agent find the right source for a part of the system? | One routing skill that holds facts about documents only, with CI failing on a gone path or an uncited document | A skill per service, or a consolidated architecture reference | A map to keep current; the check cannot see a document whose content drifted |

## Context

The design is written across 35 tracked documents of mixed freshness. An agent
loads the root `CLAUDE.md`, and a service's `CLAUDE.md` only once it opens that
service's files; after a compaction or in a subagent it has neither the thread nor
a list of what to read. The architect could not name one either.

Surveying for the map found the other half of the problem: documents that
disagree with the code. `Reserve` was still `SELECT … FOR UPDATE` in the README,
`docs/ARCHITECTURE.md` and a skill; the gateway was described as rate-limiting and
setting security headers, and does neither.

## Options

- **One routing skill, facts about documents only.** One description in the skill
  list; a service's detail loads only when needed; every fact stays in its home.
  Costs a map to maintain — bounded by a check that fails on a gone path or an
  uncited tracked document.
- **A skill per service.** Precise triggering, but seven descriptions always in
  context, overlap with each service's `CLAUDE.md`, and no home for a question that
  crosses services.
- **A consolidated architecture reference in the skill.** Quickest to read once, and
  a fourth copy of every fact — the failure `35e864f` demonstrated.

## Decision

Delegated: "brainstorm and use the /anthropic-skills:skill-creator following the
latest best practice of using claude skill in SDLC. Just brainstorm then plan and
auto execute it". The agent chose the routing skill and put the three options,
with their costs, in the plan before building.

## Consequences

An agent — or the architect briefing one — starts from the register, then the map,
then the owning document, and verifies in the code the map names. Adding a design
document without placing it on the map fails CI, as does renaming a file the map
cites. Content drift is not caught: the skill tells the reader to fix a document
that disagrees with the code in the same change, and whether that holds is what
use will show.
