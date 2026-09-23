# 0008 — The waitroom enforces session ownership; the gateway passes identity

**Date:** 2026-09-23
**Status:** accepted
**Arc:** waitroom-admission — [plan](../plans/2026-09-23-waitroom-admission-correctness.md)
**Where it lives:** `proto/waitroom.proto:31`, `services/waitroom-svc/internal/service/session_service.go:131`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Who may read or leave a session? | The waitroom checks the owner; the gateway passes `user_id` in the contract | A gateway-only check, or none | A contract change across five services |

## Context

`GetQueueStatus` and `LeaveQueue` took a session id and nothing else. Any
logged-in user holding someone's session id could read their checkout token — a
bearer credential — or remove them from the queue.

## Options

**Check in the gateway.** The gateway knows the user but not the session's owner;
it would need a second call, and any other caller of the waitroom stays unguarded.

**Check in the waitroom.** The service that owns the session enforces it; the
gateway only says who is asking. Costs a contract change.

## Decision

Not put to the architect as a separate choice: it came with the work order the
architect approved.

## Consequences

Every caller of these RPCs must say whom it acts for. The contract and its copies
in five services move together.

## Outcome

`507c1d2`. On k3s a second user got 404 for status and for leave, and the owner
kept their place.
