# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Waitroom** (virtual queue) service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

gRPC service (port **50056**, Redis-backed) that fairly throttles access to checkout under high load. Users join a FIFO **queue** (Redis sorted set); a background **queue processor** admits them as checkout slots free up, mints a short-lived **JWT checkout token**, and publishes a `queue.ready` event to Kafka. It also consumes downstream events (e.g. `checkout.completed`) to release slots and admit the next user.

Key behaviors (`internal/service/queue_processor.go`):
- Bounded concurrency — at most N users in "checkout" at once (configurable via `Queue` config; ~100 default).
- Checkout tokens are JWTs with ~15-min expiry; positions update in real time.
- Calls the **Event** service over gRPC (config `EVENT_SERVICE_ADDR`, default `localhost:50053`) to validate events before admitting.

## Commands (Makefile is the source of truth — `make help`)

```bash
make run             # run the service
make build
make test            # go test ./...
make test-coverage
make lint
make docker-up       # start local infra (Redis, Kafka, ...)
make kafka-topics-create
make redis-cli       # inspect queue/session state
make protoc / make update-proto
```

`go test ./internal/service/...` runs a single package. There is also a top-level `demo_integration_test.go`.

## Layout

- `cmd/api/main.go` — wiring: config → logger → Redis → repos → Kafka producer/consumer → Event gRPC client → services → queue processor → gRPC server.
- `internal/service/` — `session` (Redis-backed sessions + JWT), `queue` (sorted-set ops), `waitroom` (facade), `queue_processor` (admission loop).
- `internal/delivery/{grpc,http,kafka}` — transports; `internal/repository/redis`, `internal/infra/redis`.
- `pkg/` — shared `kafka`, `redis`, `grpc`, `logger`, `errors`, `response`, `util`.

## Admission is claim/ack, not pop

`ProcessEventQueue` **peeks** the head of the queue (`PeekQueue`) and removes an entry
only once admission reaches a terminal outcome:

- admitted → removed (it now holds a slot in the processing set)
- `ErrSessionNotAdmittable` → removed (session gone, already admitted, abandoned, or
  past its expiry — no longer a queue member)
- anything else is **transient**: the entry stays, and the next tick retries the same
  user at the same position

Never reintroduce a destructive pop here. Removing before admission succeeds means a
transient Redis error silently drops the user from the queue forever — their session
stays `queued` but is absent from the sorted set, so their position reads `-1` and
nothing ever retries them. Ordering also matters: add to the processing set *before*
removing from the queue. Being briefly in both is self-correcting (the next tick sees
the session as not-admittable and drops it); being in neither loses the user.

A session that reads `admitted` but holds **no** slot is a *half-finished* admission —
the token write committed and the slot write did not. `resumeOrReject` finishes it
(reusing the already-issued token) rather than treating it as stale, because treating it
as stale drops the user. This is why `claimSlot` deliberately does **not** roll the
session status back on failure: the old best-effort rollback discarded its own error, and
both calls hit Redis, so the one failure mode that mattered took out both.

### QUEUE_READY publishes are buffered, not dropped

A failed `QUEUE_READY` publish must **not** fail the admission. By that point the
session is already admitted and holds a slot, and reporting failure sends the caller
down the `ErrSessionNotAdmittable` path — which would drop the user out of the position
broadcast, the channel they actually receive their checkout token on. Instead the event
is parked on the `waitroom:queue_ready:pending` list and republished at the head of the
next tick (`drainBufferedQueueReady`), peeked and trimmed only once settled.

Note `queue.ready` currently has **no consumer** anywhere in the repo; the user-facing
notification is the Redis pub/sub position update consumed over SSE.

## Kafka consumer delivery semantics

`internal/delivery/kafka/consumer` is at-least-once and **never skips a message**. A
committed offset means "this will never need to be seen again", so:

- Transient failures are retried **in place** (`KAFKA_CONSUMER_RETRY_MAX`, exponential
  backoff from `KAFKA_CONSUMER_RETRY_BACKOFF`).
- Failures that survive the retries, and any `Permanent(err)` (a payload the handler
  cannot decode), are parked on `<topic>.dlq` with the failure context in headers, then
  committed so the partition keeps moving.
- If the DLQ publish also fails, the offset is left uncommitted and the claim returns,
  which ends the session and redelivers the message after a backed-off rejoin. Retrying
  is recoverable; a committed-but-unprocessed message is not.

Two sarama behaviours drive this design; don't "simplify" it without re-reading them:

- Offset marks are **monotonic** — `MarkOffset` keeps the highest (`offset_manager.go:587`)
  and `MarkMessage` marks `Offset+1`. Marking a later message therefore commits *past* an
  earlier failure, permanently. This is why a failed message must never be skipped.
- Returning from `ConsumeClaim` — error *or* nil — **cancels the whole session**
  (`defer sess.cancel()`, `consumer_group.go:871`), so `Consume` returns and `Start`
  rejoins the group. That does redeliver uncommitted messages, but at the cost of a full
  group rebalance across every partition, which is why transient failures are retried in
  place rather than by returning. `Start`'s rejoin loop backs off exponentially so a
  persistent failure can't turn into a rebalance storm.

Nothing consumes the `.dlq` topics yet. A dead-lettered slot-release is therefore only
tolerable because slots self-expire (above) — the two mechanisms are load-bearing for
each other. Add DLQ-depth alerting before relying on either alone.

## Single-replica constraint

The admission loop is **not safe above `replicas: 1`** (which is what
`deploy/helm/ticketbottle/templates/apps/_appservice.tpl` sets). Two known races:

- `PeekQueue` is a plain `ZRANGE`, so every replica would peek the same head and admit
  the same users concurrently. (The `GetProcessingCount` → admit sequence was already
  check-then-act, so replicas could over-admit even before the claim/ack change; the
  change additionally lost the atomicity that stopped *the same session* being claimed
  twice.)
- `waitroom:queue_ready:pending` is a single global list drained with a non-atomic
  `LRANGE` + `LTRIM`. Two drainers can trim entries the other never published.

Scaling this service out needs a per-event lock (or an atomic Lua claim into a pending
set) first.

## Deploy note: the `:checkouts` key rename

The processing set moved from `waitroom:{event}:processing` (SET) to
`waitroom:{event}:checkouts` (sorted set). Redis is persistent in the chart, so the type
could not change in place without `WRONGTYPE`. The old key carries a TTL and ages out on
its own, but **at cutover every in-flight slot is forgotten** and `:checkouts` starts
empty — the service can briefly admit up to `MaxConcurrent` extra users on top of those
already checking out. Bounded (<=100/event, <=15 min) but real: deploy during a quiet
period.

## Notes

- Logging uses the zap wrapper with ctx-first `f`-suffixed methods (`l.Errorf(ctx, ...)`).
- The repo carries several design docs worth reading before changing the admission logic, under `docs/`: `docs/QUEUE_PROCESSOR_GUIDE.md`, `docs/STREAMING_IMPLEMENTATION_GUIDE.md`, `docs/SYSTEM_FLOW_README.md`. (Trust this file and the configs for ports/behaviour — the design docs are background, not the source of truth.)
