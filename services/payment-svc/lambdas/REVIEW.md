# Payment Lambdas — Production Readiness Review

> A study document. Each finding has: **where** (file:line), **what's wrong**, **why it
> matters for *this* domain** (high-traffic ticket sales, user waiting in a virtual queue),
> and **the fix direction**. Severity is ranked for this domain, not in the abstract.
>
> The three Lambdas under review:
> - `payment-webhook-handler` — receives ZaloPay/PayOS callbacks, marks payment COMPLETED, writes an outbox row. API Gateway triggered.
> - `outbox-processor` — publishes pending outbox rows to Kafka. EventBridge `rate(1 minute)`.
> - `outbox-cleanup` — deletes old published rows, flags exhausted-retry rows. EventBridge `rate(1 day)`.

---

## The one-paragraph takeaway

The single highest-value change is **not** the ORM — it's that the **outbox relay should not be a
scheduled Lambda at all.** A once-a-minute, 50-rows-per-run, single-invocation drain is structurally
incapable of keeping up with a flash sale and adds up to 60s of latency to a flow where the user is
actively waiting. Moving the relay to a long-lived consumer collapses three P0 problems at once
(throughput ceiling, confirmation latency, Kafka-in-Lambda). The ORM swap (Prisma → typed query
builder) is real and worth doing, but it rides along with that reshaping — its main correctness payoff
is unlocking `SELECT … FOR UPDATE SKIP LOCKED`, which the current stack can't express cleanly.

---

## The Prisma question (why it's over-weight here)

The reason Prisma is a poor fit is **not** "few tables." Even one table pays the same tax. The real costs:

1. **Cold-start weight.** `prisma/schema.prisma:9` (`binaryTargets = ["native", "rhel-openssl-3.0.x"]`)
   ships the Rust query engine (~14–16 MB) into the dependencies layer. It loads on first query —
   hundreds of ms added to every cold start, on a webhook a user is waiting on.
2. **Connection model fights serverless.** Each Lambda instance opens its own Prisma pool. A flash-sale
   burst scales the webhook handler to hundreds of concurrent executions → hundreds of Postgres
   connections → `max_connections` exhaustion. No RDS Proxy / pooler in `template.yaml`.
3. **It blocks the correct outbox query.** The right concurrent-drain pattern is
   `SELECT … FOR UPDATE SKIP LOCKED`. Prisma can't express that without `$queryRaw`, so the code
   doesn't — and that's a real bug (P0 #3).

"2 tables, ~6 query shapes" is a *symptom*: the ORM's actual value (relations, complex mapping) is
barely used while you pay all its serverless costs.

**Options, ranked:**

| Option | Cold start | Type-safe | `SKIP LOCKED` | Notes |
|---|---|---|---|---|
| **Kysely / Drizzle + `pg`** ✅ | Fast (no engine) | Yes | Yes | Best fit. Keep the Prisma schema in the *main* payment-svc as migration source of truth; codegen Kysely types from the DB so they stay in sync. |
| Prisma driver adapters + `queryCompiler` | Better (drops Rust engine) | Yes | Awkward (`$queryRaw`) | Lightest touch; keeps the schema. Doesn't fix the `SKIP LOCKED` ergonomics. |
| Hand-rolled `pg` raw SQL | Fastest | **No** | Yes | Only acceptable with codegen'd types; otherwise a maintainability step back. |
| Prisma as-is | Slowest | Yes | No | Current state. |

**Verdict:** drop Prisma in the Lambdas, but move to a *typed query builder* (Kysely), not raw
strings. Raw SQL would help cold-start/bundle and unlock `SKIP LOCKED`, but you'd lose type safety and
the shared-schema story for no reason when Kysely gives you both.

---

## P0 — will break under a flash sale

### P0-1 · Outbox drains at a hard 50 events/minute
**Where:** `template.yaml:206–216` (`OutboxProcessorFunction`: `rate(1 minute)`, `OUTBOX_BATCH_SIZE=50`, single invocation).
**What:** One scheduled invocation per minute, 50 rows each. When a sale opens, the outbox *fills* at
thousands/min and *drains* at 50/min.
**Why it matters:** Backlog grows unbounded; confirmation latency becomes `backlog / 50` **minutes**.
For a platform whose entire reason to exist is high-traffic sales, this is the headline risk.
**Fix direction:** Run the relay as a **long-lived consumer** (short-poll or Postgres `LISTEN/NOTIFY`),
not a 1/min scheduled Lambda. Drain scales with backlog; latency drops to sub-second.

### P0-2 · Up-to-60s confirmation latency by design
**Where:** `template.yaml:215` (`Schedule: rate(1 minute)`).
**What:** Even with zero backlog, a completed payment waits up to 60s before `PAYMENT_COMPLETED` is
published → Order's `ConfirmOrder` runs → the user's Waitroom checkout slot frees.
**Why it matters:** The user is sitting on the confirmation page holding a scarce queue slot. 60s of
dead time is a UX failure and ties up inventory-holding slots longer than necessary.
**Fix direction:** Same continuous relay as P0-1, or CDC (Debezium) off the outbox table.

### P0-3 · Concurrent processor runs double-publish
**Where:** `outbox-processor/handlers/processor.handler.ts:96–105` (`findMany({ where: { publishedAt: null }})`, no locking).
**What:** `Timeout` is 300s but the schedule fires every 60s. EventBridge invokes independently, so a
run exceeding 60s (easy under backlog) overlaps the next tick → two invocations select the **same**
pending rows → both publish the same events.
**Why it matters:** Kafka's `idempotent: true` only dedupes within one producer session, not across two
Lambda instances → **real duplicate events** reach Kafka.
**Fix direction:** Claim rows with `SELECT … FOR UPDATE SKIP LOCKED` (needs Kysely/raw). Make Order's
`ConfirmOrder` idempotent regardless (defence in depth).

### P0-4 · Concurrent webhook deliveries create duplicate PAYMENT_COMPLETED
**Where:** `payment-webhook-handler/handlers/webhook.handler.ts:116–164` (findUnique → check status → update → outbox.create, no row lock).
**What:** Providers retry webhooks and can deliver duplicates concurrently. Under READ COMMITTED, two
transactions both read `PENDING`, both proceed, both insert an outbox row → two completion events for
one payment.
**Why it matters:** Duplicate `PAYMENT_COMPLETED` for a single charge; downstream must dedupe or the
saga mis-fires.
**Fix direction:** `SELECT … FOR UPDATE` on the payment row, **or** conditional
`UPDATE … WHERE status = 'PENDING'` and only write the outbox row when `rowcount == 1`.

---

## P1 — correctness / operability

### P1-5 · The outbox index doesn't serve the hot query
**Where:** `prisma/schema.prisma:62` (`@@index([published, createdAt])`) vs. `processor.handler.ts:96–105`
(filters `publishedAt IS NULL AND retryCount < N ORDER BY createdAt`).
**What:** The index is on `published` (a boolean) but the query filters on `publishedAt` (a timestamp) —
different column, so the index is unusable → seq scan + sort that worsens as the table grows. Worse,
the `published` boolean (`schema.prisma:56`) is **never set to true** anywhere; the code keys entirely
off `publishedAt`. It's a dead column.
**Fix direction:** Partial index `CREATE INDEX ... ON outbox (created_at) WHERE published_at IS NULL;`
and drop the `published` column.

### P1-6 · Exhausted-retry events only get logged — no DLQ, no alarm
**Where:** `outbox-cleanup/handlers/cleanup.handler.ts:102–122` (`monitorFailedEvents` — alerting is TODOs, just `logger.error`).
**What:** Rows past `maxRetries` are found and logged, never routed anywhere. No Lambda
`DeadLetterConfig` / `OnFailure` destination either.
**Why it matters:** A stuck `PAYMENT_COMPLETED` = **a customer charged but their order never confirms.**
That must page a human / hit a DLQ / raise a CloudWatch alarm — not sit in a log line.
**Fix direction:** Emit a CloudWatch metric + alarm on failed-count > 0; route exhausted rows to a DLQ
(SQS) for manual replay.

### P1-7 · Kafka producer inside Lambda is an anti-pattern
**Where:** `common/kafka/producer.ts:4–53` (module-global producer, cached across invocations).
**What:** Lambda freezes the execution environment between invocations; the cached TCP connection goes
half-open across freeze/thaw, so the first publish after a thaw can fail and force a reconnect (paying
bootstrap + metadata + TLS/SASL each cold start). `disconnectKafka` is effectively never called, so
brokers accumulate half-open connections under concurrency.
**Why it matters:** Latency spikes and broker connection churn exactly under load.
**Fix direction:** Move the relay to a long-lived process (also fixes P0-1/2). If it must stay on
Lambda, detect a stale producer on thaw and reconnect explicitly.

### P1-8 · Sequential publish + N+1 status updates
**Where:** `processor.handler.ts:118–140` (awaits each publish in series, then a per-row `update`).
**What:** 50 sequential Kafka round-trips + 50 sequential UPDATEs per run.
**Why it matters:** Structurally caps throughput, compounding P0-1.
**Fix direction:** Publish with bounded concurrency (`Promise.all` in chunks); batch the DB marks with a
single `updateMany` over succeeded IDs.

---

## P2 — hardening

### P2-9 · Secrets in Lambda env vars
**Where:** `template.yaml:83–86` (`DATABASE_URL`, Kafka creds) and `:164–169` (ZaloPay/PayOS signing keys).
**What:** Injected as plain env vars — readable via `lambda:GetFunctionConfiguration`, only default-KMS
encrypted at rest.
**Why it matters:** These are **payment-signing keys**. Env vars are the wrong home for them.
**Fix direction:** Secrets Manager / SSM Parameter Store, fetched at runtime and cached in the module scope.

### P2-10 · Provider detection re-sniffs the request body
**Where:** `webhook.handler.ts:12–34` (`detectProvider` parses `body.data && body.mac`, etc.) while the route is already `/webhook/{provider}` (`template.yaml:174`).
**What:** Redundant and fragile guessing from body shape.
**Fix direction:** Use `event.pathParameters.provider`; drop the body-shape heuristic (also a spoofing surface).

### P2-11 · Broken `$connect` error handling
**Where:** `common/database/prisma.ts:16–20`.
**What:** `prisma.$connect()` is called without `await`; the client is returned immediately, and the
`.catch` runs on a floating promise → a connect failure becomes an unhandledRejection the caller never
sees. Prisma connects lazily anyway, so the block is mostly theater.
**Fix direction:** Remove it (moot after the Prisma → Kysely migration).

### P2-12 · Over-broad egress + blanket HTTP 200 on provider errors
**Where:** `template.yaml:112–127` (SG egress to `0.0.0.0/0` on 5432/9092) and `webhook.handler.ts:183–191` (returns 200 for every `PaymentProviderError`).
**What:** Egress is wider than needed; returning 200 for all provider errors is correct for ZaloPay's
ack contract but means genuine failures never trigger provider retries and look like success to
monitoring.
**Fix direction:** Scope egress to the DB/broker CIDR; make the ack status **provider-specific and deliberate**.

---

## Cross-cutting note: at-least-once is fine, but the contract must be explicit

The outbox pattern is intentionally **at-least-once**: the processor publishes to Kafka *then* marks
`publishedAt` (`processor.handler.ts:119` then `:122`). If the Lambda dies between those, the row
republishes next run → duplicate. That's correct and acceptable **iff** every downstream consumer
(notably Order's `ConfirmOrder`) is idempotent. P0-3 and P0-4 make duplicates *more* likely than the
baseline; even after fixing them, the idempotent-consumer contract must hold and be documented.

---

## Fix dependency map (why order matters)

```
P0-1 relay throughput ┐
P0-2 relay latency    ├─► move relay to long-lived consumer ──┐
P1-7 kafka-in-lambda  ┘                                       │
                                                              ├─► then P0-3 SKIP LOCKED claim
Prisma → Kysely (typed, no engine) ───────────────────────────┘   (needs the query builder)

P0-4 webhook dup ──► independent (row lock / conditional update) — can land first
P1-5 partial index ─► independent (migration) — can land first, cheap, high value
P1-6 DLQ/alarm ─────► independent (infra) — after relay reshaping is decided
P2-* ───────────────► hardening, after the P0/P1 core
```
