# 0003 — A waiting buyer polls for admission; nothing is pushed

**Date:** 2026-09-23
**Status:** accepted
**Arc:** waitroom-stampede — [plan](../plans/2026-09-23-waitroom-join-stampede.md)
**Where it lives:** `services/api-gateway/src/modules/waitroom/waitroom.controller.ts:41`, `services/waitroom-svc/internal/service/queue_service.go:91`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How does a waiting buyer learn they are admitted? | The client polls `GetQueueStatus` | A push stream: SSE, a gRPC stream and a Redis subscription per waiter | Admission is seen on the next poll, seconds into a 15-minute token |

## Context

Every waiter held three connections — SSE to the gateway, a gRPC stream behind
it, and a Redis Pub/Sub subscription — and every broadcast made each subscriber
run `GET session` + `ZRANK` + `ZCARD`. `EnqueueSession` broadcast on every join,
so the work was quadratic in queue depth: ~300k Redis operations per second at
100k waiters, plus an O(N) fanout, on Redis's single thread.

## Options

**Keep the stream.** Admission is seen at once; every waiter costs three held
connections and a share of every broadcast.

**Poll `GetQueueStatus`.** It already returned what a waiting client needs:
position, and once admitted the token. A poll costs two Redis round trips and
nothing between polls.

## Decision

Reconstructed after the fact from the stampede plan (phase 1) and commit
`872858b`; the decider's words were not recorded.

## Consequences

The queue's cost stops scaling with its depth, and admission latency becomes the
poll interval. The stored session becomes the only way a buyer learns their
token, so anything that corrupts it strands them — which is why the join race in
[0007](0007-the-join-race-is-fixed-by-deleting-the-write.md) mattered.

## Outcome

`872858b` removed the stream contract-first. On k3s on 2026-09-23, `k3s-load` ran
310 purchases that each found its admission by polling; all 310 completed.
