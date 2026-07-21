# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **Waitroom** (virtual queue) service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

gRPC service (port **50056**, Redis-backed) that fairly throttles access to checkout under high load. Users join a FIFO **queue** (Redis sorted set); a background **queue processor** admits them as checkout slots free up, mints a short-lived **JWT checkout token**, and publishes a `QUEUE_READY` event to Kafka. It also consumes downstream events (e.g. `CHECKOUT_COMPLETED`) to release slots and admit the next user.

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
- If the DLQ publish also fails, the offset is left uncommitted and the claim stops —
  a stalled partition is recoverable, a committed-but-unprocessed message is not.

Two sarama behaviours drive this design; don't "simplify" it without re-reading them:
offset marks are **monotonic** (`MarkOffset` keeps the highest), so marking a later
message commits past an earlier failure permanently; and returning an error from
`ConsumeClaim` does **not** end the session or trigger redelivery — sarama just closes
that claim, stalling the partition until the next rebalance.

## Notes

- Logging uses the zap wrapper with ctx-first `f`-suffixed methods (`l.Errorf(ctx, ...)`).
- The repo carries several design docs worth reading before changing the admission logic: `QUEUE_PROCESSOR_GUIDE.md`, `REAL_TIME_POSITION.md`, `STREAMING_IMPLEMENTATION_GUIDE.md`, `SYSTEM_FLOW_README.md`. (The ASCII diagram in `README.md` is generic/early and lists wrong ports — trust this file and the configs instead.)
