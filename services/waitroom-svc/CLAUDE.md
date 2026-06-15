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

## Notes

- Logging uses the zap wrapper with ctx-first `f`-suffixed methods (`l.Errorf(ctx, ...)`).
- The repo carries several design docs worth reading before changing the admission logic: `QUEUE_PROCESSOR_GUIDE.md`, `REAL_TIME_POSITION.md`, `STREAMING_IMPLEMENTATION_GUIDE.md`, `SYSTEM_FLOW_README.md`. (The ASCII diagram in `README.md` is generic/early and lists wrong ports — trust this file and the configs instead.)
