# Waitroom — reading guide

*Trust labels checked against the code on 2026-09-23.*

For anything touching the queue, admission, sessions, checkout tokens, the draw, or
the waitroom's Kafka consumer.

## Read in this order

1. `services/waitroom-svc/CLAUDE.md` — **current, and the owner of almost every
   waitroom question**: claim/ack admission, buffered `queue.ready`, the draw, the
   per-event cache, polling instead of push, consumer delivery semantics, the
   single-replica constraint, the `:checkouts` key rename.
2. The decisions of the `waitroom-stampede` and `waitroom-admission` arcs, from
   `docs/decisions/README.md` — why each of those rules beat its alternative.
3. `docs/plans/2026-09-23-waitroom-admission-correctness.md` — the five defects found
   end to end on k3s, and what each fix proved. A record of that day.
4. `services/waitroom-svc/docs/SYSTEM_FLOW_README.md` — background: the flow in
   prose and the Redis key layout. Written before the draw and the removal of the
   push stream; where it disagrees with the service `CLAUDE.md`, that file wins.
5. `services/waitroom-svc/docs/QUEUE_PROCESSOR_GUIDE.md` — background: operating the
   processor with `redis-cli`. Same caveat.

## By question

| Question | Owner | Verify in |
|---|---|---|
| Who is admitted next, and when | service `CLAUDE.md`, *Order is a draw…* | `services/waitroom-svc/internal/models/session.go`, `services/waitroom-svc/internal/repository/redis/queue_repository.go` |
| What admission does to a queue entry | service `CLAUDE.md`, *Admission is claim/ack* | `services/waitroom-svc/internal/service/queue_processor.go` |
| How many buyers may check out at once | root `CLAUDE.md`, register — **open** | `services/waitroom-svc/internal/service/queue_processor.go`, `services/waitroom-svc/config/config.go` |
| What a join asks event-svc | service `CLAUDE.md`, *JoinQueue asks event-svc…* | `services/waitroom-svc/internal/service/event_gate.go` |
| How a client learns its position and token | service `CLAUDE.md`, *Admission is discovered by polling* | `services/waitroom-svc/internal/service/waitroom_service.go` |
| Who may read or leave a session | `docs/decisions/0008-the-waitroom-enforces-session-ownership.md`, `docs/decisions/0009-a-strangers-request-reads-as-not-found.md` | `services/waitroom-svc/internal/service/waitroom_service.go` |
| What happens to a Kafka message that fails | service `CLAUDE.md`, *Kafka consumer delivery semantics* | `services/waitroom-svc/internal/delivery/kafka/consumer/consumer.go` |
| Can it run two replicas | service `CLAUDE.md`, *Single-replica constraint* | `deploy/helm/ticketbottle/templates/apps/_appservice.tpl` |
| What the waitroom publishes to Prometheus | `docs/METRICS.md` | `services/waitroom-svc/internal/metrics/metrics.go` |

## Code entry points

| To see | Open |
|---|---|
| Wiring, start to finish | `services/waitroom-svc/cmd/api/main.go` |
| The gRPC surface | `services/waitroom-svc/internal/delivery/grpc/service.go` |
| Sessions and checkout tokens | `services/waitroom-svc/internal/service/session_service.go` |
| Queue operations | `services/waitroom-svc/internal/service/queue_service.go` |
| Kafka in and out | `services/waitroom-svc/internal/delivery/kafka/consumer/handlers.go`, `services/waitroom-svc/internal/delivery/kafka/producer/producer.go` |

## Edges

- **Serves** `proto/waitroom.proto`, to the gateway — `services/api-gateway/src/modules/waitroom/waitroom.controller.ts`.
- **Calls** event-svc, only through `services/waitroom-svc/internal/service/event_gate.go`.
- **Kafka** — topic names in `services/waitroom-svc/internal/delivery/kafka/constants.go`;
  the slot-freeing events come from order-svc (see [order.md](order.md)).
- **Store** — Redis; key layout in the service `CLAUDE.md` and the repositories.
- **Deployed by** `deploy/helm/ticketbottle/templates/apps/waitroom.yaml`.

## Don't trust for current behaviour

- Either `services/waitroom-svc/docs/` file on queue scores, client streaming, or
  Redis Pub/Sub — the service `CLAUDE.md` and decisions 0003 and 0005 replaced them.
