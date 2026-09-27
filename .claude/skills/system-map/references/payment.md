# Payment — reading guide

*Trust labels checked against the code on 2026-09-23.*

For payment intents and providers, the outbox, and the two workloads that carry a
completed payment to Kafka: `payment-webhook` and `outbox-relay`.

## Read in this order

1. `services/payment-svc/CLAUDE.md` — **current**: what the gRPC service still does
   and what moved out, the Lambdas, the logging rules, the one atomicity invariant.
2. `.claude/skills/outbox-relay/SKILL.md` — **current, the owner of the event path**:
   the transactional outbox, `LISTEN/NOTIFY`, `FOR UPDATE SKIP LOCKED`, the
   idempotent webhook, the DLQ, and the invariants to keep when editing it.
3. `.claude/skills/deployment-architecture/references/k3s-ec2.md`, the
   `payment-webhook` rows — on a cluster the webhook is a simulated provider, not the
   Lambda.
4. `docs/plans/2026-09-20-payment-svc-test-coverage.md` — what the tests pin, which
   paths are latent, and why. A record of that day.
5. `services/payment-svc/lambdas/README.md` — **current**: what each Lambda does, and
   building, testing and deploying them with SAM.

## By question

| Question | Owner | Verify in |
|---|---|---|
| Are a payment and its event one write? | outbox-relay skill, *The core pattern* | `services/payment-svc/src/modules/outbox/outbox.service.ts` |
| How an outbox row reaches Kafka, and on which topic | outbox-relay skill | `services/payment-svc/outbox-relay/src/relay.ts`, `services/payment-svc/outbox-relay/src/kafka.ts` |
| What a duplicate provider webhook does | outbox-relay skill, pattern 4; the cluster's simulated webhook keeps the same guard | `services/payment-svc/lambdas/payment-webhook-handler/handlers/webhook.handler.ts`, `deploy/adapters/payment-events/webhook.js` |
| What happens to an event that never publishes | outbox-relay skill, pattern 5 | `services/payment-svc/lambdas/outbox-cleanup/handlers/cleanup.handler.ts` |
| Adding a provider | service `CLAUDE.md` | `services/payment-svc/src/modules/payment/gateways/gateway.factory.ts` |
| The outbox is backing up | `docs/RUNBOOK.md`, *OutboxBacklogGrowing* | `deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml` |

## Code entry points

| To see | Open |
|---|---|
| The gRPC surface | `services/payment-svc/src/modules/payment/controllers/grpc/payment.controller.ts` |
| Intents and provider calls | `services/payment-svc/src/modules/payment/payment.service.ts` |
| The relay's loop: listen, poll, reconnect | `services/payment-svc/outbox-relay/src/runtime.ts` |
| Schema, partial index, notify trigger | `services/payment-svc/prisma/schema.prisma`, `services/payment-svc/prisma/migrations/` |

## Edges

- **Serves** `proto/payment.proto`, to order-svc
  (`services/order-svc/internal/activities/payment_activity.go`).
- **Kafka** — publishes only through `outbox-relay`; the gRPC service has no Kafka
  client. Order consumes the topics — see [order.md](order.md).
- **Store** — Postgres: the gRPC service via Prisma, the Lambdas and relay via Kysely/`pg`.
- **Deployed by** `deploy/helm/ticketbottle/templates/apps/payment.yaml`,
  `deploy/helm/ticketbottle/templates/apps/outbox-relay.yaml`,
  `deploy/helm/ticketbottle/templates/apps/payment-events.yaml`; Lambdas by
  `services/payment-svc/lambdas/template.yaml`.

## Don't trust for current behaviour

- `services/payment-svc/src/modules/payment/controllers/http/payment.controller.ts` —
  an empty shell; no provider callback reaches the gRPC service.
