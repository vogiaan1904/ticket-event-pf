---
name: outbox-relay
description: Use when working on, extending, or debugging the payment outbox → Kafka event path in TicketBottle (the transactional-outbox + long-lived relay design), or to study the system-design patterns behind it — transactional outbox, LISTEN/NOTIFY, FOR UPDATE SKIP LOCKED work-claiming, idempotent compare-and-set webhooks, DLQ + alarm, and partial indexes.
---

# The payment outbox → Kafka relay

This is how a completed payment reliably becomes a `PAYMENT_COMPLETED` Kafka event that Order can confirm. It is the reference implementation of the **transactional outbox** pattern in this repo. Study it here; the same shape applies to any "change DB state *and* tell the world" problem.

## File map

| Piece | Where |
|-------|-------|
| Outbox write (joins caller's tx) | `services/payment-svc/src/modules/outbox/outbox.service.ts` — `saveEvent(..., tx?)` |
| Work-claiming query | `services/payment-svc/lambdas/common/db/outbox.repo.ts` — `claimBatch` (`.forUpdate().skipLocked()`) |
| Long-lived relay worker | `services/payment-svc/outbox-relay/src/` — `relay.ts` (drain), `db.ts` (cycle), `runtime.ts` (LISTEN + scheduler + reconnect), `kafka.ts` (`topicFor`) |
| Idempotent webhook completion | `services/payment-svc/lambdas/payment-webhook-handler/handlers/webhook.handler.ts` — conditional `UPDATE … WHERE status='PENDING'` |
| Exhausted-retry → DLQ + metric | `services/payment-svc/lambdas/outbox-cleanup/handlers/cleanup.handler.ts` — `routeExhaustedEvents` |
| Schema (partial index, notify trigger) | `services/payment-svc/prisma/migrations/20260718075919_outbox_relay/migration.sql` |
| Infra (DLQ + CloudWatch alarm) | `services/payment-svc/lambdas/template.yaml` |

## The core pattern: transactional outbox

**Problem — the dual-write.** Completing a payment means doing two things: update `payments.status = COMPLETED` **and** publish to Kafka so Order confirms. Two independent writes are never crash-safe: a crash between them leaves the DB paid but the event lost (order stuck forever), or the event sent but the DB rolled back (phantom completion).

**Fix — make it one write.** Insert the business row and an `outbox` row in the **same DB transaction** (that's why `saveEvent` takes an optional `tx`). Atomic: both commit or neither does. A separate **relay** then reads the outbox and publishes to Kafka. The event can't be lost because it committed alongside the state that produced it.

Everything below is in service of doing the relay side well.

## The patterns worth learning (each with its "why")

**1 — Long-lived relay, not a scheduled Lambda.** The old design polled the outbox with a 1/min `outbox-processor` Lambda (now retired). That's a compute-model mismatch: a relay wants a *persistent* Kafka producer (the TCP + metadata handshake is expensive to redo), a reactive loop (react in ms, not up-to-60s), and a reused pg pool. Lambda gives the opposite. **Lesson:** FaaS fits *spiky, event-triggered, stateless* work (keep the webhook handler on Lambda). A *continuous, connection-heavy, low-latency* loop belongs in a long-lived worker (`outbox-relay` is a k8s Deployment).

**2 — LISTEN/NOTIFY push, polling as a safety net.** The migration adds an `outbox_notify()` trigger firing `pg_notify('outbox_new', …)` on insert; `runtime.ts` holds a dedicated `LISTEN` connection and triggers a drain on each notification. This gives sub-second latency without busy-polling. A periodic safety-poll remains as a fallback so a dropped notification can't strand rows. **Lesson:** push for latency, poll for correctness — keep both.

**3 — `FOR UPDATE SKIP LOCKED` work-claiming.** `claimBatch` selects unpublished rows `ORDER BY createdAt LIMIT n FOR UPDATE SKIP LOCKED`. This turns a plain table into a concurrent queue: `FOR UPDATE` locks the claimed rows; `SKIP LOCKED` lets other relay replicas skip already-claimed rows and grab the next free ones instead of blocking. N workers claim *disjoint* batches and scale horizontally with no external queue. **This is the idiom for a job queue on Postgres.**

**4 — Idempotent compare-and-set (the crux of correctness).** The relay is **at-least-once** — on a crash mid-publish an event can go to Kafka twice. That is a property to design around, not a bug to fix: *at-least-once delivery ⇒ consumers must be idempotent.* The webhook models it:
```sql
UPDATE payments SET status='COMPLETED'
WHERE orderCode = ? AND status='PENDING' RETURNING …
```
`WHERE status='PENDING'` is a compare-and-set: the first webhook flips PENDING→COMPLETED and gets a row; a duplicate matches 0 rows and returns nothing. The outbox insert is gated on "did I get a row?", so a replay is a silent no-op instead of a double completion. **Idempotency enforced by the database, not by hoping messages arrive once.**

**5 — DLQ + alarm for poison messages.** Retry with backoff → a *bounded* retry count → then `routeExhaustedEvents` ships the event to an SQS DLQ and emits the `OutboxFailedEvents` CloudWatch metric (alarm in `template.yaml`). **Lesson:** "retry forever" lets one poison message stall the pipeline. Give failures an exit ramp *and* make them visible.

**6 — Partial index on the hot set.** `idx_outbox_unpublished ON outbox("createdAt") WHERE "publishedAt" IS NULL` indexes only *unpublished* rows — the working set the relay scans. It stays tiny even as the outbox grows to millions of published rows. Pairs with dropping the old `published boolean` for `publishedAt IS NULL`: one nullable timestamp is a flag *and* an audit time *and* the index predicate.

**7 — Coalesced drain + reconnect-with-backoff.** `runtime.ts`'s scheduler serializes drains and collapses triggers that arrive mid-drain into a single follow-up pass (never concurrent drains over the same rows); the LISTEN connection reconnects with exponential backoff on drop. **Lesson:** debounce bursty signals; assume every long-lived connection will drop and self-heal.

## Invariants to preserve when editing this path

- **Never split the outbox write from the business write.** They must share one transaction (`saveEvent(..., tx)`). Breaking this reintroduces the dual-write bug.
- **Consumers/handlers stay idempotent.** Delivery is at-least-once. Any new event handler must tolerate seeing the same event twice (compare-and-set, dedupe key, or upsert).
- **Don't reintroduce the polling `outbox-processor` Lambda** or a `published boolean`. Publishing is the `outbox-relay` worker; unpublished = `publishedAt IS NULL`.
- **New event types need a `topicFor` mapping** in `outbox-relay/src/kafka.ts`, keyed by the `EventType` enum value actually stored in `outbox.eventType` (a raw/unmapped value routes to the default topic — the pre-existing `PaymentCancelled` misroute is exactly this trap).
- The payment gRPC service only **writes** the outbox — editing it does not change publishing behavior.

## Debugging
Order stuck / not confirming? Check, in order: outbox rows with `publishedAt IS NULL` piling up (relay not draining) → `kubectl -n ticketbottle logs deploy/outbox-relay` → Kafka UI (`localhost:8090`) for the `payment.*` topic → the order consumer. A single stuck event that exhausted retries lands in the SQS DLQ and trips the `OutboxFailedEvents` alarm.

## Further study
Chris Richardson, [Transactional Outbox](https://microservices.io/patterns/data/transactional-outbox.html) and Polling Publisher vs Transaction Log Tailing — the latter (CDC / Debezium reading the WAL) is the one variant this repo deliberately did *not* use.
