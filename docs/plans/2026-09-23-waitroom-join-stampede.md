# Waitroom: survive the join stampede, and make the draw fair

**Status: COMPLETE 2026-09-23**, with one check outstanding — `make -C deploy gate1`
has **not** run against these changes. See *Verification*.

**Goal:** The waitroom protected inventory well and protected nothing else. Two
paths melted long before the oversell guard could be stressed, and both were on
the *waiting* side rather than the buying side. Fix those, and stop deciding an
on-sale by round-trip time.

**Derived from:** a read of `services/waitroom-svc/internal/` on 2026-09-23, plus
the rate arithmetic in `2026-09-22-inventory-contention-benchmark.md` that showed
the reserve path's lock is never reached at the shipped admission rate.

## What was wrong

**The position stream was O(N) per tick, and a stampede was quadratic.** Every
waiter held three connections — an SSE connection to the gateway, a gRPC stream
behind it, and a Redis Pub/Sub subscription. Each broadcast made every subscriber
run `GET session` + `ZRANK` + `ZCARD`, and `EnqueueSession` published on *every
join*. At 100k waiters that is ~300k Redis operations per second against a single
thread, plus an O(N) fanout on that same thread.

**`JoinQueue` made two synchronous event-svc calls per joiner**, with no cache, to
answer questions whose answers never change. 500k joiners is a million calls into
event-svc and the Postgres it shares with Temporal at a stock `max_connections=100`.

**Order was `QueuedAt.Unix()`** — arrival order, which makes an on-sale a race
between network paths rather than between buyers.

## What was done

| Phase | Commit | |
|---|---|---|
| 1 | `872858b` | Position stream removed contract-first; clients poll `GetQueueStatus` |
| 2 | `95d4ac7` | One poll: 4 Redis round trips → 2 |
| 3 | `2049d1a` | Event rules cached per event per TTL, misses collapsed |
| — | `0326e9d` | Dead `websocket_url` dropped from `JoinQueueResponse`, field 6 reserved |
| 4 | `a09b859` | Random draw before sale open, arrival order after |
| — | `40d2661` | Stale `waitroom.proto` copies synced; decision register updated |

Three findings worth keeping:

- **A TTL cache alone does not fix phase 3.** The cache is cold at the instant an
  on-sale opens, so every joiner misses together — that *is* the stampede.
  `singleflight` collapses concurrent misses; without it the cache only helps in
  steady state.
- **Verdicts are cacheable; dependency failures are not.** Caching
  `ErrEventServiceUnavailable` would keep a queue shut for the rest of the TTL
  after event-svc recovered.
- **go-redis v9 stamps a sibling command's `redis.Nil` onto every command in a
  pipeline**, while their values stay correct. Trusting that error turned a
  legitimate position of `-1` into a failed poll. Caught by a test, not by review.

## Verification

Passing:

- `go test ./...` in `waitroom-svc`, 7 packages, against real Redis
- `tsc --noEmit` and 20 tests in `api-gateway`
- all 11 assertions in `helm/ticketbottle/tests/assert-render.sh`
- mutation-tested: the two rewritten admission assertions, and the draw's band
  separation (`saleStart-1` → `saleStart` makes it fail, as it must)

**Not run: `make -C deploy gate1`.** It is the check that admission discovery
still works end to end now that nothing is pushed, and it needs the k3s box —
kind was retired for disk. **Run it before trusting phase 1 in a deployment.**

Also untested outside fakes: the event gate against a real event-svc, and the
draw's ordering in a live Redis sorted set.

## Deliberately not done

- **Per-event `maxConcurrent`.** Still open in the root `CLAUDE.md` register. The
  per-event config path now exists (`internal/service/event_gate.go`), so it is a
  field and a knob rather than a new mechanism.
- **Edge rate limiting.** 500k simultaneous HTTP connections overwhelm the gateway
  whatever this service does; that belongs to an ALB or CDN tier.
- **Temporal and Postgres capacity under sustained checkout load.** A different
  failure, partly addressed by `docs/design/eks-stateful-tier.md`.
- **The gateway's `order.pb.ts` drift.** Found while regenerating stubs and left
  alone to keep this scoped: the committed stub still describes orders keyed by
  `id` with offset pagination, while `src/protos/order.proto` — which NestJS loads
  at runtime — is the current cursor-based contract. The gateway's order endpoints
  are sending fields the contract no longer has. Its own work order.
