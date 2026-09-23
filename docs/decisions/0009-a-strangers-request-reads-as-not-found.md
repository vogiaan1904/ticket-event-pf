# 0009 — A stranger's request reads as NOT_FOUND, checked before the session's state

**Date:** 2026-09-23
**Status:** accepted
**Arc:** waitroom-admission — [plan](../plans/2026-09-23-waitroom-admission-correctness.md)
**Where it lives:** `services/waitroom-svc/internal/service/session_service.go:131`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What does a stranger see? | `NOT_FOUND`, with the owner checked before the session's state | `PERMISSION_DENIED` | Bends the error taxonomy's wording so a session id cannot be probed |

## Context

[0008](0008-the-waitroom-enforces-session-ownership.md) added the owner check; the
open question was what a stranger receives.

## Options

**`NOT_FOUND`.** The session is invisible to anyone else. Reuses
`ErrSessionNotFound`; no new error code.

**`PERMISSION_DENIED` (403).** The taxonomy's literal wording — authenticated but
not allowed — but it confirms the session exists.

## Decision

Delegated: "And for D1, and D2, choose what you recommened" — the architect, 2026-09-23.

## Consequences

The owner check must come before anything that reveals state, or the state itself
becomes the oracle.

## Outcome

`507c1d2` checked the owner after the session's state, so an ended session
answered a stranger with 409. Probing the deployed gateway as a second user found
it; `719dda8` moved the check first, and the same probe now gets 404.
