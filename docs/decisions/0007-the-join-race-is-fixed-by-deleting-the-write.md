# 0007 — The join race is fixed by deleting the stale write, not by locking

**Date:** 2026-09-23
**Status:** accepted
**Arc:** waitroom-admission — [plan](../plans/2026-09-23-waitroom-admission-correctness.md)
**Where it lives:** `services/waitroom-svc/internal/service/waitroom_service.go:71`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How is the join race fixed? | Delete `JoinQueue`'s final full-session write | Make every session write atomic (`WATCH` or a Lua script) | The session stays one JSON blob; other read-then-write paths stay unguarded |

## Context

`JoinQueue` enqueued, published `queue.joined` synchronously, then `SET` its
in-memory session. A processor tick inside that Kafka round trip admitted the
session, and the `SET` reverted it: queued, no token, out of the queue, holding a
slot. With polling ([0003](0003-a-waiting-buyer-polls-for-admission.md)) that buyer
saw position `-1` until the slot expired.

## Options

**Delete the write.** It carried only `Position`, which nothing reads from storage.

**Make session writes atomic.** Fixes the whole class — including the latent revert
in `HandleCheckout*` — at the cost of changing every write path.

## Decision

Not put to the architect as a separate choice: the deletion came with the work
order the architect approved.

## Consequences

The join can no longer undo an admission. The class remains: `HandleCheckout*`
still writes back a stale copy, latent only because nothing reads the field it
reverts. A second race of this shape is the signal to make writes atomic.

## Outcome

`3601618`, with a test that forces the tick inside the join against real Redis:
red before, green after.
