# Payment Lambda Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the payment outbox path so it survives a flash sale — move the outbox relay off a 1/min Lambda onto a long-lived consumer, swap Prisma for Kysely, and close the concurrent-duplicate and index gaps.

**Architecture:** Three changes, one theme (throughput + correctness under concurrency): (1) the `outbox-processor` Lambda is **deleted** and replaced by a long-lived `outbox-relay` worker Deployment that drains via `LISTEN/NOTIFY` + `SELECT … FOR UPDATE SKIP LOCKED`, publishing in parallel with a persistent Kafka producer; (2) the `payment-webhook-handler` and `outbox-cleanup` Lambdas stay serverless but move Prisma → **Kysely + pg** (no Rust engine, faster cold start), and the webhook gains a single-transition conditional update so concurrent provider retries can't double-emit; (3) the outbox table gets a partial index and a `pg_notify` trigger, and the cleanup Lambda gains a real DLQ + CloudWatch alarm.

**Tech Stack:** TypeScript (Node 20), Kysely + `pg`, kafkajs, AWS Lambda (webhook + cleanup), Kubernetes Deployment (relay) via Helm/kind, Prisma (migrations only, in the main payment-svc), SAM (Lambda infra), Jest.

## Global Constraints

- **DB column names are camelCase, quoted.** Prisma maps model fields to columns verbatim (no `@map` on columns), so every raw identifier is `"publishedAt"`, `"retryCount"`, `"createdAt"`, `"aggregateId"`, `"aggregateType"`, `"eventType"`, `"lastError"`, `"orderCode"`, `"providerTransactionId"`, `"completedAt"`, `"updatedAt"`. Table names are lowercase: `outbox`, `payments`.
- **Migrations are owned by `services/payment-svc/prisma/`** (the main gRPC service), never by `lambdas/prisma/`. After this plan `lambdas/prisma/` is deleted.
- **Outbox is at-least-once by contract.** Publish-then-mark means a crash republishes; every consumer (Order `ConfirmOrder`) must stay idempotent. Do not "fix" this into exactly-once.
- **Node 20, `engines.node >= 20`.** Match the existing `lambdas/package.json`.
- **The main payment gRPC service must NOT publish to Kafka** (per `services/payment-svc/CLAUDE.md`). The relay is a *separate* Deployment, not code added to the gRPC service.
- **Local loop:** build/deploy is `make -C deploy apps-up`; acceptance is `make -C deploy gate1`. The relay image is built by `deploy/scripts/build-images.sh` and kind-loaded.
- Prisma clients in the Lambdas ship `binaryTargets = ["native","rhel-openssl-3.0.x"]` today; the Kysely swap must remove that engine from the built layer.
- **Test DB access:** integration tests run on the host against the kind Postgres. kind only host-maps port 30000, so NodePort 31432 is NOT on `localhost`. Before running any DB test, start a port-forward and use it as `DATABASE_URL`:
  ```bash
  kubectl port-forward -n ticketbottle svc/postgres 5432:5432 >/tmp/pf-pg.log 2>&1 &
  export DATABASE_URL="postgresql://root:root@localhost:5432/ticketbottle_payment"
  ```
- **Code style (user rule):** clean code only — no comments that restate what the code does (keep only non-obvious *why*/invariant comments); when a change supersedes a file/module/deployment, delete it in the same commit rather than leaving it dormant. The example code in tasks below may carry explanatory comments — strip the redundant ones when transcribing.

---

## File Structure

**New — shared DB layer (used by relay, webhook, cleanup):**
- `services/payment-svc/lambdas/common/db/types.ts` — hand-written Kysely `Database` interface for `outbox` + `payments`.
- `services/payment-svc/lambdas/common/db/kysely.ts` — `getDb()` / `getPool()` / `closeDb()` singletons over a `pg.Pool`.
- `services/payment-svc/lambdas/common/db/outbox.repo.ts` — `claimBatch`, `markPublished`, `markFailed` (the SKIP LOCKED claim + batch marks).

**New — long-lived relay worker:**
- `services/payment-svc/outbox-relay/package.json`
- `services/payment-svc/outbox-relay/tsconfig.json`
- `services/payment-svc/outbox-relay/src/relay.ts` — drain cycle (claim→publish→mark in one tx).
- `services/payment-svc/outbox-relay/src/runtime.ts` — LISTEN/NOTIFY listener, safety poll, drain coalescing, Kafka producer lifecycle, SIGTERM shutdown.
- `services/payment-svc/outbox-relay/src/index.ts` — entrypoint (`node dist/index.js`).
- `services/payment-svc/outbox-relay/Dockerfile`
- `services/payment-svc/outbox-relay/__tests__/relay.test.ts`
- `services/payment-svc/outbox-relay/__tests__/runtime.test.ts`

**Modified:**
- `services/payment-svc/prisma/schema.prisma` — drop `published` field + composite index (partial index & trigger added by raw migration).
- `services/payment-svc/prisma/migrations/<new>/migration.sql` — drop column, partial index, `pg_notify` trigger.
- `services/payment-svc/lambdas/payment-webhook-handler/handlers/webhook.handler.ts` — Kysely + conditional single-transition update.
- `services/payment-svc/lambdas/outbox-cleanup/handlers/cleanup.handler.ts` — Kysely + CloudWatch metric + SQS DLQ.
- `services/payment-svc/lambdas/template.yaml` — remove `OutboxProcessorFunction`; add DLQ queue + alarm for cleanup.
- `services/payment-svc/lambdas/package.json` — add `kysely`,`pg`; remove `@prisma/client`,`prisma`,`prisma:gen`.
- `services/payment-svc/lambdas/scripts/build-layers.js` — stop bundling the Prisma engine.
- `deploy/helm/ticketbottle/templates/apps/outbox-relay.yaml` — new Deployment (replaces the dormant `outbox-publisher` block).
- `deploy/helm/ticketbottle/values.yaml` / `values-localstack.yaml` — `outboxRelay.enabled` toggle.
- `deploy/scripts/build-images.sh` — build + kind-load `ticketbottle/outbox-relay:local`.

**Deleted:**
- `services/payment-svc/lambdas/outbox-processor/` (entire dir — reborn as the relay).
- `services/payment-svc/lambdas/prisma/` (Kysely replaces it).
- `services/payment-svc/lambdas/common/database/prisma.ts`.

---

## Task 1: Shared Kysely DB layer

**Files:**
- Create: `services/payment-svc/lambdas/common/db/types.ts`
- Create: `services/payment-svc/lambdas/common/db/kysely.ts`
- Create: `services/payment-svc/lambdas/common/db/outbox.repo.ts`
- Test: `services/payment-svc/lambdas/common/db/__tests__/outbox.repo.test.ts`
- Modify: `services/payment-svc/lambdas/package.json`

**Interfaces:**
- Produces: `getDb(): Kysely<Database>`, `getPool(): Pool`, `closeDb(): Promise<void>` (from `kysely.ts`); `type Database` (from `types.ts`); `claimBatch(db, limit, maxRetries): Promise<OutboxRow[]>`, `markPublished(db, ids: string[]): Promise<void>`, `markFailed(db, id: string, error: string): Promise<void>`, `type OutboxRow` (from `outbox.repo.ts`).

- [ ] **Step 1: Add deps**

```bash
cd services/payment-svc/lambdas
npm install kysely pg
npm install -D @types/pg
```

- [ ] **Step 2: Write `types.ts`**

```typescript
// services/payment-svc/lambdas/common/db/types.ts
import { ColumnType, Generated, JSONColumnType } from 'kysely';

export type PaymentStatus = 'PENDING' | 'COMPLETED' | 'CANCELLED' | 'FAILED';

export interface OutboxTable {
  id: Generated<string>;
  aggregateId: string;
  aggregateType: string;
  eventType: string;
  payload: JSONColumnType<unknown>;
  publishedAt: ColumnType<Date | null, Date | null, Date | null>;
  createdAt: Generated<Date>;
  retryCount: Generated<number>;
  lastError: string | null;
}

export interface PaymentsTable {
  id: Generated<string>;
  orderCode: string;
  amountCents: number;
  currency: string;
  provider: string;
  providerTransactionId: string;
  idempotencyKey: string;
  redirectUrl: string;
  paymentUrl: string;
  status: PaymentStatus;
  metadata: JSONColumnType<unknown> | null;
  createdAt: Generated<Date>;
  updatedAt: ColumnType<Date, Date | undefined, Date>;
  completedAt: ColumnType<Date | null, Date | null, Date | null>;
  failedAt: ColumnType<Date | null, Date | null, Date | null>;
  cancelledAt: ColumnType<Date | null, Date | null, Date | null>;
}

export interface Database {
  outbox: OutboxTable;
  payments: PaymentsTable;
}
```

- [ ] **Step 3: Write `kysely.ts`**

```typescript
// services/payment-svc/lambdas/common/db/kysely.ts
import { Kysely, PostgresDialect } from 'kysely';
import { Pool } from 'pg';
import { Database } from './types';

let db: Kysely<Database> | null = null;
let pool: Pool | null = null;

export const getPool = (): Pool => {
  if (!pool) {
    if (!process.env.DATABASE_URL) throw new Error('DATABASE_URL is required');
    pool = new Pool({
      connectionString: process.env.DATABASE_URL,
      max: Number(process.env.PG_POOL_MAX ?? 5),
    });
  }
  return pool;
};

export const getDb = (): Kysely<Database> => {
  if (!db) db = new Kysely<Database>({ dialect: new PostgresDialect({ pool: getPool() }) });
  return db;
};

export const closeDb = async (): Promise<void> => {
  if (db) { await db.destroy(); db = null; pool = null; }
};
```

- [ ] **Step 4: Write the failing test** (requires a Postgres at `DATABASE_URL`; use the kind Postgres via `postgres-ext` NodePort 31432, or a throwaway container)

```typescript
// services/payment-svc/lambdas/common/db/__tests__/outbox.repo.test.ts
import { getDb, closeDb } from '../kysely';
import { claimBatch, markPublished } from '../outbox.repo';

const seed = async () => {
  const db = getDb();
  await db.deleteFrom('outbox').execute();
  await db.insertInto('outbox').values([
    { aggregateId: 'p1', aggregateType: 'payment', eventType: 'PaymentCompleted', payload: JSON.stringify({}), retryCount: 0 },
    { aggregateId: 'p2', aggregateType: 'payment', eventType: 'PaymentCompleted', payload: JSON.stringify({}), retryCount: 0 },
  ]).execute();
};

afterAll(async () => { await closeDb(); });

test('claimBatch returns unpublished rows oldest-first', async () => {
  await seed();
  const rows = await claimBatch(getDb(), 10, 5);
  expect(rows.map((r) => r.aggregateId)).toEqual(['p1', 'p2']);
});

test('markPublished sets publishedAt so rows are no longer claimable', async () => {
  await seed();
  const rows = await claimBatch(getDb(), 10, 5);
  await markPublished(getDb(), rows.map((r) => r.id));
  const again = await claimBatch(getDb(), 10, 5);
  expect(again).toHaveLength(0);
});

test('two concurrent claims never return the same row (SKIP LOCKED)', async () => {
  await seed();
  const db = getDb();
  const [a, b] = await Promise.all([
    db.transaction().execute(async (trx) => claimBatch(trx, 10, 5)),
    db.transaction().execute(async (trx) => claimBatch(trx, 10, 5)),
  ]);
  const ids = [...a, ...b].map((r) => r.id);
  expect(new Set(ids).size).toBe(ids.length); // no overlap
});
```

- [ ] **Step 5: Run it — expect FAIL** (`claimBatch` not defined)

```bash
DATABASE_URL="postgres://root:root@localhost:5432/ticketbottle_payment" npx jest common/db/__tests__/outbox.repo.test.ts
```
Expected: FAIL — "Cannot find module '../outbox.repo'".

- [ ] **Step 6: Write `outbox.repo.ts`**

```typescript
// services/payment-svc/lambdas/common/db/outbox.repo.ts
import { Kysely, sql } from 'kysely';
import { Database } from './types';

export interface OutboxRow {
  id: string;
  aggregateId: string;
  aggregateType: string;
  eventType: string;
  payload: unknown;
}

// Claim unpublished rows oldest-first, locking them so a sibling worker skips them.
// MUST run inside a transaction — the lock is held until commit.
export const claimBatch = (
  db: Kysely<Database>,
  limit: number,
  maxRetries: number,
): Promise<OutboxRow[]> =>
  db
    .selectFrom('outbox')
    .select(['id', 'aggregateId', 'aggregateType', 'eventType', 'payload'])
    .where('publishedAt', 'is', null)
    .where('retryCount', '<', maxRetries)
    .orderBy('createdAt', 'asc')
    .limit(limit)
    .forUpdate()
    .skipLocked()
    .execute() as Promise<OutboxRow[]>;

export const markPublished = async (db: Kysely<Database>, ids: string[]): Promise<void> => {
  if (ids.length === 0) return;
  await db.updateTable('outbox').set({ publishedAt: new Date() }).where('id', 'in', ids).execute();
};

export const markFailed = async (db: Kysely<Database>, id: string, error: string): Promise<void> => {
  await db
    .updateTable('outbox')
    .set({ retryCount: sql`"retryCount" + 1`, lastError: error.slice(0, 500) })
    .where('id', '=', id)
    .execute();
};
```

- [ ] **Step 7: Run tests — expect PASS**

```bash
DATABASE_URL="postgres://root:root@localhost:5432/ticketbottle_payment" npx jest common/db/__tests__/outbox.repo.test.ts
```
Expected: 3 passing. (If the `payments`/`outbox` tables don't exist yet locally, run Task 2's migration first, then re-run.)

- [ ] **Step 8: Commit**

```bash
git add services/payment-svc/lambdas/common/db services/payment-svc/lambdas/package.json services/payment-svc/lambdas/package-lock.json
git commit -m "feat(payment-lambdas): add Kysely DB layer with SKIP LOCKED outbox claim"
```

---

## Task 2: Outbox migration — partial index, drop `published`, notify trigger

**Files:**
- Modify: `services/payment-svc/prisma/schema.prisma:50-64`
- Create: `services/payment-svc/prisma/migrations/<timestamp>_outbox_relay/migration.sql`

**Interfaces:**
- Produces: partial index `idx_outbox_unpublished`, channel `outbox_new` emitting `NEW.id` on every outbox insert (consumed by Task 5).

- [ ] **Step 1: Edit the schema** — remove the `published` field and the composite index in `services/payment-svc/prisma/schema.prisma`

Replace the `Outbox` model's fields/index so it reads:
```prisma
model Outbox {
  id            String    @id @default(uuid())
  aggregateId   String
  aggregateType String
  eventType     String
  payload       Json
  publishedAt   DateTime?
  createdAt     DateTime  @default(now())
  retryCount    Int       @default(0)
  lastError     String?

  @@map("outbox")
}
```
(The partial index and trigger are not expressible in Prisma schema — they go in the raw migration below.)

- [ ] **Step 2: Generate an empty migration to edit**

```bash
cd services/payment-svc
DATABASE_URL="postgres://root:root@localhost:5432/ticketbottle_payment" \
  npx prisma migrate dev --create-only --name outbox_relay
```

- [ ] **Step 3: Replace the generated `migration.sql`** with (drop column + its old index, add partial index, add notify trigger)

```sql
-- DropIndex (Prisma's old composite index name)
DROP INDEX IF EXISTS "outbox_published_createdAt_idx";

-- DropColumn
ALTER TABLE "outbox" DROP COLUMN IF EXISTS "published";

-- Partial index that actually serves the relay's hot query
CREATE INDEX IF NOT EXISTS "idx_outbox_unpublished"
  ON "outbox" ("createdAt")
  WHERE "publishedAt" IS NULL;

-- NOTIFY on every insert so the long-lived relay wakes immediately
CREATE OR REPLACE FUNCTION outbox_notify() RETURNS trigger AS $$
BEGIN
  PERFORM pg_notify('outbox_new', NEW.id::text);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS outbox_notify_trg ON "outbox";
CREATE TRIGGER outbox_notify_trg
  AFTER INSERT ON "outbox"
  FOR EACH ROW EXECUTE FUNCTION outbox_notify();
```

- [ ] **Step 4: Apply and verify the index is used**

```bash
cd services/payment-svc
DATABASE_URL="postgres://root:root@localhost:5432/ticketbottle_payment" npx prisma migrate deploy
psql "postgres://root:root@localhost:5432/ticketbottle_payment" -c \
  'EXPLAIN SELECT id FROM outbox WHERE "publishedAt" IS NULL AND "retryCount" < 5 ORDER BY "createdAt" LIMIT 50;'
```
Expected: plan contains `Index Scan using idx_outbox_unpublished` (not `Seq Scan`).

- [ ] **Step 5: Verify NOTIFY fires**

```bash
psql "postgres://root:root@localhost:5432/ticketbottle_payment" -c "LISTEN outbox_new;" \
  -c "INSERT INTO outbox (id,\"aggregateId\",\"aggregateType\",\"eventType\",payload,\"retryCount\") VALUES (gen_random_uuid(),'x','payment','PaymentCompleted','{}',0);" \
  -c "SELECT 1;"
```
Expected: an `Asynchronous notification "outbox_new"` line in psql output.

- [ ] **Step 6: Commit**

```bash
git add services/payment-svc/prisma
git commit -m "feat(payment): partial outbox index + pg_notify trigger; drop dead published column"
```

---

## Task 3: Webhook handler — Kysely + single-transition conditional update (P0-4)

**Files:**
- Modify: `services/payment-svc/lambdas/payment-webhook-handler/handlers/webhook.handler.ts`
- Test: `services/payment-svc/lambdas/payment-webhook-handler/__tests__/webhook.concurrency.test.ts`

**Interfaces:**
- Consumes: `getDb`, `closeDb` (Task 1); `Database` (Task 1).
- Produces: unchanged handler export `handleWebhook(event): Promise<APIGatewayProxyResult>`.

- [ ] **Step 1: Write the failing concurrency test** (requires Postgres + the Task 2 schema)

```typescript
// services/payment-svc/lambdas/payment-webhook-handler/__tests__/webhook.concurrency.test.ts
import { getDb, closeDb } from '@/common/db/kysely';
import { completePaymentAndEnqueue } from '../handlers/webhook.handler';

afterAll(async () => { await closeDb(); });

beforeEach(async () => {
  const db = getDb();
  await db.deleteFrom('outbox').execute();
  await db.deleteFrom('payments').execute();
  await db.insertInto('payments').values({
    id: 'pay-1', orderCode: 'ORD-1', amountCents: 1000, currency: 'VND', provider: 'zalopay',
    providerTransactionId: 'tx-1', idempotencyKey: 'idem-1', redirectUrl: 'x', paymentUrl: 'y', status: 'PENDING',
  }).execute();
});

test('two concurrent completions produce exactly one outbox row', async () => {
  await Promise.all([
    completePaymentAndEnqueue('ORD-1', 'tx-1'),
    completePaymentAndEnqueue('ORD-1', 'tx-1'),
  ]);
  const rows = await getDb().selectFrom('outbox').selectAll().execute();
  expect(rows).toHaveLength(1);
  const pay = await getDb().selectFrom('payments').select('status').where('orderCode', '=', 'ORD-1').executeTakeFirst();
  expect(pay?.status).toBe('COMPLETED');
});
```

- [ ] **Step 2: Run it — expect FAIL** (`completePaymentAndEnqueue` not exported)

```bash
DATABASE_URL="postgres://root:root@localhost:5432/ticketbottle_payment" npx jest webhook.concurrency
```
Expected: FAIL — "completePaymentAndEnqueue is not a function".

- [ ] **Step 3: Extract the completion into a Kysely conditional transaction** — add to `webhook.handler.ts`, replacing the Prisma `$transaction` block (`webhook.handler.ts:114-170`)

```typescript
import { getDb } from '@/common/db/kysely';
import { EventType, PaymentCompletedEvent } from '@/common/types/event.types';

// Returns true if THIS call performed the transition (and wrote the outbox row).
// Concurrent duplicates hit the `status = 'PENDING'` guard, match 0 rows, and no-op.
export const completePaymentAndEnqueue = async (
  orderCode: string,
  providerTransactionId: string | undefined,
): Promise<boolean> =>
  getDb().transaction().execute(async (trx) => {
    const now = new Date();
    const updated = await trx
      .updateTable('payments')
      .set({
        status: 'COMPLETED',
        completedAt: now,
        updatedAt: now,
        ...(providerTransactionId ? { providerTransactionId } : {}),
      })
      .where('orderCode', '=', orderCode)
      .where('status', '=', 'PENDING')
      .returning(['id', 'orderCode', 'amountCents', 'currency', 'provider', 'providerTransactionId'])
      .executeTakeFirst();

    if (!updated) return false; // already completed OR not found -> idempotent no-op

    const payload: PaymentCompletedEvent = {
      payment_id: updated.id,
      order_code: updated.orderCode,
      amount_cents: updated.amountCents,
      currency: updated.currency,
      provider: updated.provider,
      transaction_id: updated.providerTransactionId ?? '',
      completed_at: now.toISOString(),
    };

    await trx
      .insertInto('outbox')
      .values({
        aggregateId: updated.id,
        aggregateType: 'payment',
        eventType: EventType.PAYMENT_COMPLETED,
        payload: JSON.stringify(payload),
        retryCount: 0,
      })
      .execute();

    return true;
  });
```

- [ ] **Step 4: Rewire `handleWebhook`** to call it — replace the old `prisma.$transaction(...)` call site (`webhook.handler.ts:114-170`) with:

```typescript
    const didComplete = await completePaymentAndEnqueue(orderCode, callbackResult.providerTransactionId);
    if (!didComplete) {
      logger.warn('Payment already completed or not found, skipping', { orderCode, requestId });
    } else {
      logger.info('Payment completed', { orderCode, requestId });
    }
```
Also replace the PayOS lookup at `webhook.handler.ts:96-101` (`prisma.payment.findFirst`) with:
```typescript
      const payment = await getDb()
        .selectFrom('payments')
        .select('orderCode')
        .where('providerTransactionId', '=', callbackResult.providerTransactionId!)
        .executeTakeFirst();
      if (!payment) throw new ValidationError(`Payment not found for transaction ${callbackResult.providerTransactionId}`);
      orderCode = payment.orderCode;
```
Remove the `import { getPrismaClient } ... prisma` usages and the `PaymentStatus` import from `@prisma/client` (use the string literal `'COMPLETED'`).

- [ ] **Step 5: Run tests — expect PASS**

```bash
DATABASE_URL="postgres://root:root@localhost:5432/ticketbottle_payment" npx jest payment-webhook-handler
```
Expected: the concurrency test passes (exactly one outbox row) and the existing webhook tests still pass.

- [ ] **Step 6: Commit**

```bash
git add services/payment-svc/lambdas/payment-webhook-handler
git commit -m "fix(payment-webhook): single-transition conditional update prevents duplicate completion events"
```

---

## Task 4: Relay core — claim → parallel publish → batch mark (P0-1, P0-3, P1-8)

**Files:**
- Create: `services/payment-svc/outbox-relay/package.json`
- Create: `services/payment-svc/outbox-relay/tsconfig.json`
- Create: `services/payment-svc/outbox-relay/src/relay.ts`
- Test: `services/payment-svc/outbox-relay/__tests__/relay.test.ts`

**Interfaces:**
- Consumes: `getDb` (Task 1); `claimBatch`, `markPublished`, `markFailed`, `OutboxRow` (Task 1); `publishWithRetry`, `getKafkaProducer` (existing `common/kafka/producer.ts`); `getTopicForEventType` (moved from the deleted processor).
- Produces: `drainOnce(deps): Promise<{ processed: number; succeeded: number; failed: number }>`.

- [ ] **Step 1: Scaffold the package** (reuses the lambdas' `common/` via a path alias)

```jsonc
// services/payment-svc/outbox-relay/package.json
{
  "name": "ticketbottle-outbox-relay",
  "version": "1.0.0",
  "private": true,
  "main": "dist/index.js",
  "scripts": { "build": "tsc", "start": "node dist/index.js", "test": "jest" },
  "dependencies": { "kafkajs": "^2.2.4", "kysely": "^0.27.0", "pg": "^8.11.0", "winston": "^3.18.3" },
  "engines": { "node": ">=20.0.0" }
}
```
```jsonc
// services/payment-svc/outbox-relay/tsconfig.json
{
  "compilerOptions": {
    "target": "ES2022", "module": "CommonJS", "outDir": "dist", "rootDir": "src",
    "strict": true, "esModuleInterop": true, "skipLibCheck": true, "resolveJsonModule": true
  },
  "include": ["src/**/*"]
}
```
> The relay's `src/db.ts`, `src/kafka.ts`, `src/topics.ts` re-export from the lambdas' `common/` (copied or symlinked at build time in the Dockerfile, Task 6). For unit testing, import them directly with a relative path to `../../lambdas/common/...`.

- [ ] **Step 2: Write the failing test** (Kafka + DB mocked — pure orchestration test)

```typescript
// services/payment-svc/outbox-relay/__tests__/relay.test.ts
import { drainOnce } from '../src/relay';

const rows = [
  { id: 'a', aggregateId: 'p1', aggregateType: 'payment', eventType: 'PaymentCompleted', payload: {} },
  { id: 'b', aggregateId: 'p2', aggregateType: 'payment', eventType: 'PaymentCompleted', payload: {} },
];

test('publishes each claimed row and marks the successful ones in one batch', async () => {
  const published: string[] = [];
  const markedPublished: string[][] = [];
  const deps = {
    claim: jest.fn().mockResolvedValueOnce(rows),
    publish: jest.fn(async (r: any) => { published.push(r.id); }),
    markPublished: jest.fn(async (ids: string[]) => { markedPublished.push(ids); }),
    markFailed: jest.fn(),
    topicFor: () => 'payment-completed',
    batchSize: 50, maxRetries: 3,
  };
  const res = await drainOnce(deps as any);
  expect(published.sort()).toEqual(['a', 'b']);
  expect(markedPublished).toEqual([['a', 'b']]);   // single batched mark
  expect(res).toEqual({ processed: 2, succeeded: 2, failed: 0 });
});

test('a publish failure increments retry, does not mark published', async () => {
  const deps = {
    claim: jest.fn().mockResolvedValueOnce(rows),
    publish: jest.fn(async (r: any) => { if (r.id === 'b') throw new Error('kafka down'); }),
    markPublished: jest.fn(),
    markFailed: jest.fn(),
    topicFor: () => 'payment-completed',
    batchSize: 50, maxRetries: 3,
  };
  const res = await drainOnce(deps as any);
  expect(deps.markPublished).toHaveBeenCalledWith(['a']);
  expect(deps.markFailed).toHaveBeenCalledWith('b', expect.stringContaining('kafka down'));
  expect(res).toEqual({ processed: 2, succeeded: 1, failed: 1 });
});
```

- [ ] **Step 3: Run it — expect FAIL**

```bash
cd services/payment-svc/outbox-relay && npx jest
```
Expected: FAIL — cannot find `../src/relay`.

- [ ] **Step 4: Write `relay.ts`** — claim + publish (parallel) + batch mark, inside one transaction

```typescript
// services/payment-svc/outbox-relay/src/relay.ts
import type { OutboxRow } from './db';

export interface DrainDeps {
  claim: (limit: number, maxRetries: number) => Promise<OutboxRow[]>;
  publish: (row: OutboxRow, topic: string) => Promise<void>;
  markPublished: (ids: string[]) => Promise<void>;
  markFailed: (id: string, error: string) => Promise<void>;
  topicFor: (eventType: string) => string;
  batchSize: number;
  maxRetries: number;
}

export interface DrainResult { processed: number; succeeded: number; failed: number; }

export const drainOnce = async (deps: DrainDeps): Promise<DrainResult> => {
  const rows = await deps.claim(deps.batchSize, deps.maxRetries);
  if (rows.length === 0) return { processed: 0, succeeded: 0, failed: 0 };

  const results = await Promise.allSettled(
    rows.map((r) => deps.publish(r, deps.topicFor(r.eventType))),
  );

  const succeededIds: string[] = [];
  let failed = 0;
  await Promise.all(results.map(async (res, i) => {
    if (res.status === 'fulfilled') { succeededIds.push(rows[i].id); }
    else { failed++; await deps.markFailed(rows[i].id, String(res.reason?.message ?? res.reason)); }
  }));

  await deps.markPublished(succeededIds);
  return { processed: rows.length, succeeded: succeededIds.length, failed };
};
```
> `drainOnce` takes injected deps so the real wiring (transaction-wrapped `claimBatch` + `publishWithRetry` + `markPublished`) is composed in `runtime.ts` (Task 5) and unit-tested here without a DB/Kafka. The real `claim`/`markPublished`/`markFailed` will run inside one `db.transaction().execute(...)` so the SKIP LOCKED locks are released only after marking.

- [ ] **Step 5: Run tests — expect PASS**

```bash
cd services/payment-svc/outbox-relay && npx jest
```
Expected: 2 passing.

- [ ] **Step 6: Commit**

```bash
git add services/payment-svc/outbox-relay
git commit -m "feat(outbox-relay): drain core — parallel publish + batched mark over a claimed batch"
```

---

## Task 5: Relay runtime — LISTEN/NOTIFY, coalesced drain, Kafka lifecycle, graceful shutdown (P0-2, P0-7)

**Files:**
- Create: `services/payment-svc/outbox-relay/src/db.ts` (re-exports Task 1 layer + a transaction-wrapped drain composer)
- Create: `services/payment-svc/outbox-relay/src/runtime.ts`
- Create: `services/payment-svc/outbox-relay/src/index.ts`
- Test: `services/payment-svc/outbox-relay/__tests__/runtime.test.ts`

**Interfaces:**
- Consumes: `drainOnce`, `DrainResult` (Task 4); `getDb`, `getPool`, `closeDb` (Task 1); `getKafkaProducer`, `disconnectKafka`, `publishWithRetry` (existing kafka producer).
- Produces: `createDrainScheduler(drain: () => Promise<void>): { trigger: () => void }` (coalescing), `startRuntime(): Promise<() => Promise<void>>` (returns a shutdown fn).

- [ ] **Step 1: Write the failing coalescing test** (pure logic, no IO)

```typescript
// services/payment-svc/outbox-relay/__tests__/runtime.test.ts
import { createDrainScheduler } from '../src/runtime';

const tick = () => new Promise((r) => setImmediate(r));

test('overlapping triggers coalesce into exactly one follow-up drain', async () => {
  let active = 0; let maxActive = 0; let calls = 0;
  let release!: () => void;
  const drain = jest.fn(async () => {
    calls++; active++; maxActive = Math.max(maxActive, active);
    await new Promise<void>((r) => { release = r; });
    active--;
  });
  const sched = createDrainScheduler(drain);
  sched.trigger();           // starts drain #1
  await tick();
  sched.trigger();           // arrives mid-drain -> should set pending, not start #2
  sched.trigger();           // another -> still just one pending
  expect(maxActive).toBe(1); // never concurrent
  release(); await tick();   // #1 finishes -> one coalesced follow-up runs
  release(); await tick();   // follow-up finishes
  expect(calls).toBe(2);     // exactly two drains total, not three
});
```

- [ ] **Step 2: Run it — expect FAIL**

```bash
cd services/payment-svc/outbox-relay && npx jest runtime
```
Expected: FAIL — cannot find `createDrainScheduler`.

- [ ] **Step 3: Write `runtime.ts`**

```typescript
// services/payment-svc/outbox-relay/src/runtime.ts
import { Client } from 'pg';
import { getPool, closeDb } from './db';
import { composeDrain } from './db';
import { getKafkaProducer, disconnectKafka } from './kafka';
import { logger } from './logger';

// Serialize drains; coalesce any triggers that arrive while one is running into a single follow-up.
export const createDrainScheduler = (drain: () => Promise<void>) => {
  let running = false;
  let pending = false;
  const run = async () => {
    if (running) { pending = true; return; }
    running = true;
    try { await drain(); }
    catch (e) { logger.error('drain cycle failed', { error: (e as Error).message }); }
    finally {
      running = false;
      if (pending) { pending = false; void run(); }
    }
  };
  return { trigger: () => void run() };
};

const SAFETY_POLL_MS = Number(process.env.RELAY_SAFETY_POLL_MS ?? 5000);

export const startRuntime = async (): Promise<() => Promise<void>> => {
  await getKafkaProducer(); // connect the long-lived producer once at boot

  // Fully drains the backlog each cycle (keeps claiming until a batch is empty).
  const drainAll = composeDrain();
  const scheduler = createDrainScheduler(drainAll);

  // Dedicated LISTEN connection (not from the pool).
  const listener = new Client({ connectionString: process.env.DATABASE_URL });
  await listener.connect();
  await listener.query('LISTEN outbox_new');
  listener.on('notification', () => scheduler.trigger());
  listener.on('error', (e) => logger.error('listener error; relying on safety poll', { error: e.message }));

  const safety = setInterval(() => scheduler.trigger(), SAFETY_POLL_MS);
  scheduler.trigger(); // drain whatever is already pending at boot

  logger.info('outbox-relay started', { safetyPollMs: SAFETY_POLL_MS });

  return async () => {
    clearInterval(safety);
    await listener.end().catch(() => {});
    await disconnectKafka();
    await closeDb();
    logger.info('outbox-relay stopped');
  };
};
```

- [ ] **Step 4: Write `db.ts`** — re-exports + the transaction-wrapped `composeDrain()` that binds Task 4's `drainOnce` to the real DB/Kafka

```typescript
// services/payment-svc/outbox-relay/src/db.ts
export { getDb, getPool, closeDb } from '../../lambdas/common/db/kysely';
export { claimBatch, markPublished, markFailed } from '../../lambdas/common/db/outbox.repo';
export type { OutboxRow } from '../../lambdas/common/db/outbox.repo';

import { getDb } from '../../lambdas/common/db/kysely';
import { claimBatch, markPublished, markFailed, OutboxRow } from '../../lambdas/common/db/outbox.repo';
import { drainOnce } from './relay';
import { publishRow, topicFor } from './kafka';

const BATCH = Number(process.env.OUTBOX_BATCH_SIZE ?? 100);
const MAX_RETRIES = Number(process.env.OUTBOX_MAX_RETRIES ?? 5);

// One cycle = keep draining batches until an empty batch. Each batch is one
// transaction: claim (FOR UPDATE SKIP LOCKED) -> publish -> mark, then commit.
export const composeDrain = () => async (): Promise<void> => {
  for (;;) {
    const processed = await getDb().transaction().execute((trx) =>
      drainOnce({
        claim: (limit, mr) => claimBatch(trx, limit, mr),
        publish: (row: OutboxRow, topic: string) => publishRow(topic, row),
        markPublished: (ids) => markPublished(trx, ids),
        markFailed: (id, err) => markFailed(trx, id, err),
        topicFor, batchSize: BATCH, maxRetries: MAX_RETRIES,
      }).then((r) => r.processed),
    );
    if (processed === 0) break;
  }
};
```
And `src/kafka.ts` / `src/logger.ts` re-export from the lambdas' `common/kafka/producer.ts` and `common/logger`, plus:
```typescript
// services/payment-svc/outbox-relay/src/kafka.ts
export { getKafkaProducer, disconnectKafka, publishWithRetry } from '../../lambdas/common/kafka/producer';
import { publishWithRetry } from '../../lambdas/common/kafka/producer';
import { KAFKA_TOPICS } from '../../lambdas/common/constants/kafka-topics';
import type { OutboxRow } from '../../lambdas/common/db/outbox.repo';

export const topicFor = (eventType: string): string => {
  switch (eventType) {
    case 'PaymentCompleted': return KAFKA_TOPICS.PAYMENT_COMPLETED;
    case 'PaymentFailed':    return KAFKA_TOPICS.PAYMENT_FAILED;
    case 'PaymentCancelled': return KAFKA_TOPICS.PAYMENT_CANCELLED;
    default:                 return KAFKA_TOPICS.PAYMENT_FAILED;
  }
};
export const publishRow = async (topic: string, row: OutboxRow): Promise<void> => {
  await publishWithRetry(topic, row.payload, row.aggregateId, { eventType: row.eventType, aggregateType: row.aggregateType, eventId: row.id });
};
```

- [ ] **Step 5: Write `index.ts`** — boot + SIGTERM handling (k8s sends SIGTERM on pod stop)

```typescript
// services/payment-svc/outbox-relay/src/index.ts
import { startRuntime } from './runtime';
import { logger } from './logger';

(async () => {
  const shutdown = await startRuntime();
  for (const sig of ['SIGTERM', 'SIGINT'] as const) {
    process.on(sig, async () => { logger.info(`received ${sig}`); await shutdown(); process.exit(0); });
  }
})().catch((e) => { logger.error('relay failed to start', { error: e.message }); process.exit(1); });
```

- [ ] **Step 6: Run tests — expect PASS**

```bash
cd services/payment-svc/outbox-relay && npx jest runtime
```
Expected: the coalescing test passes (`calls === 2`, never concurrent).

- [ ] **Step 7: Commit**

```bash
git add services/payment-svc/outbox-relay
git commit -m "feat(outbox-relay): LISTEN/NOTIFY runtime with coalesced drain, persistent Kafka producer, graceful shutdown"
```

---

## Task 6: Deploy the relay; delete the processor Lambda

**Files:**
- Create: `services/payment-svc/outbox-relay/Dockerfile`
- Create: `deploy/helm/ticketbottle/templates/apps/outbox-relay.yaml`
- Modify: `deploy/helm/ticketbottle/values.yaml`, `deploy/helm/ticketbottle/values-localstack.yaml`
- Modify: `deploy/scripts/build-images.sh`
- Modify: `services/payment-svc/lambdas/template.yaml` (remove `OutboxProcessorFunction`)
- Delete: `services/payment-svc/lambdas/outbox-processor/`
- Remove: the dormant `outbox-publisher` block in `deploy/helm/ticketbottle/templates/apps/payment-events.yaml`

**Interfaces:**
- Consumes: the built image `ticketbottle/outbox-relay:local`; the `payment-config` ConfigMap (must contain `DATABASE_URL`, `KAFKA_BROKERS`, `OUTBOX_BATCH_SIZE`, `OUTBOX_MAX_RETRIES`).

- [ ] **Step 1: Dockerfile** (bundles the relay + the shared `lambdas/common/`; build context is `services/payment-svc`)

```dockerfile
# services/payment-svc/outbox-relay/Dockerfile  (build context: services/payment-svc)
FROM node:20-slim AS build
WORKDIR /app
COPY outbox-relay/package*.json outbox-relay/
COPY lambdas/common lambdas/common
RUN cd outbox-relay && npm install
COPY outbox-relay/tsconfig.json outbox-relay/
COPY outbox-relay/src outbox-relay/src
RUN cd outbox-relay && npm run build

FROM node:20-slim
WORKDIR /app
COPY --from=build /app/outbox-relay/node_modules ./node_modules
COPY --from=build /app/outbox-relay/dist ./dist
COPY --from=build /app/lambdas/common ./lambdas/common
USER node
CMD ["node", "dist/index.js"]
```

- [ ] **Step 2: Helm Deployment**

```yaml
# deploy/helm/ticketbottle/templates/apps/outbox-relay.yaml
{{- if and .Values.apps.enabled .Values.outboxRelay.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: outbox-relay
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  replicas: {{ .Values.outboxRelay.replicas | default 1 }}
  selector:
    matchLabels: { app: outbox-relay }
  template:
    metadata:
      labels: { app: outbox-relay }
    spec:
      containers:
        - name: outbox-relay
          image: ticketbottle/outbox-relay:local
          imagePullPolicy: IfNotPresent
          envFrom:
            - configMapRef: { name: payment-config }
{{- end }}
```

- [ ] **Step 3: Values toggle** — add to `values.yaml` and enable in `values-localstack.yaml`

```yaml
# values.yaml
outboxRelay:
  enabled: true
  replicas: 1
```
```yaml
# values-localstack.yaml  (relay replaces the outbox-processor Lambda in Rung 1.5)
outboxRelay:
  enabled: true
```
Verify `payment-config` in `deploy/helm/ticketbottle/templates/apps/config.yaml` carries `DATABASE_URL`, `KAFKA_BROKERS`, `OUTBOX_BATCH_SIZE`, `OUTBOX_MAX_RETRIES`; add any missing key.

- [ ] **Step 4: Build script** — add the relay image build+load to `deploy/scripts/build-images.sh` (follow the existing per-image pattern; context `services/payment-svc`, dockerfile `outbox-relay/Dockerfile`, tag `ticketbottle/outbox-relay:local`, then `kind load docker-image ... --name ticketbottle`).

- [ ] **Step 5: Delete the Lambda + dormant publisher**

```bash
git rm -r services/payment-svc/lambdas/outbox-processor
# In services/payment-svc/lambdas/template.yaml: delete the OutboxProcessorFunction resource (lines ~196-217)
#   and its OutboxProcessorArn output (lines ~260-262).
# In deploy/helm/.../apps/payment-events.yaml: delete the outbox-publisher Deployment block.
```

- [ ] **Step 6: Deploy and run the acceptance gate**

```bash
make -C deploy apps-up
kubectl -n ticketbottle get deploy outbox-relay        # 1/1 READY
kubectl -n ticketbottle logs deploy/outbox-relay | grep 'outbox-relay started'
make -C deploy gate1                                    # full purchase flow, order reaches COMPLETED
```
Expected: `gate1` passes and the order confirms in **well under a second** after payment (not up to 60s). Confirm by timing the outbox row's `createdAt` vs the relay log.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat(deploy): run outbox-relay as a long-lived Deployment; remove outbox-processor Lambda"
```

---

## Task 7: Cleanup Lambda — Kysely + DLQ + CloudWatch alarm (P1-6)

**Files:**
- Modify: `services/payment-svc/lambdas/outbox-cleanup/handlers/cleanup.handler.ts`
- Modify: `services/payment-svc/lambdas/template.yaml` (add SQS DLQ + alarm; grant cleanup `sqs:SendMessage` + `cloudwatch:PutMetricData`)
- Test: `services/payment-svc/lambdas/outbox-cleanup/__tests__/cleanup.dlq.test.ts`

**Interfaces:**
- Consumes: `getDb` (Task 1).
- Produces: exhausted rows (`retryCount >= maxRetries`, `publishedAt IS NULL`) are sent to the DLQ and counted via a `OutboxFailedEvents` metric.

- [ ] **Step 1: Write the failing test** (SDK + DB mocked)

```typescript
// services/payment-svc/lambdas/outbox-cleanup/__tests__/cleanup.dlq.test.ts
import { routeExhaustedEvents } from '../handlers/cleanup.handler';

test('sends each exhausted row to the DLQ and emits a metric with the count', async () => {
  const sendMessage = jest.fn().mockResolvedValue({});
  const putMetric = jest.fn().mockResolvedValue({});
  const failed = [{ id: 'x', aggregateId: 'p1', eventType: 'PaymentCompleted', retryCount: 3, lastError: 'boom' }];
  await routeExhaustedEvents(failed as any, { sendMessage, putMetric, dlqUrl: 'q' });
  expect(sendMessage).toHaveBeenCalledTimes(1);
  expect(putMetric).toHaveBeenCalledWith(expect.objectContaining({ value: 1 }));
});

test('no failed rows => no DLQ sends, metric value 0', async () => {
  const sendMessage = jest.fn(); const putMetric = jest.fn().mockResolvedValue({});
  await routeExhaustedEvents([], { sendMessage, putMetric, dlqUrl: 'q' });
  expect(sendMessage).not.toHaveBeenCalled();
  expect(putMetric).toHaveBeenCalledWith(expect.objectContaining({ value: 0 }));
});
```

- [ ] **Step 2: Run it — expect FAIL** (`routeExhaustedEvents` not exported).

```bash
cd services/payment-svc/lambdas && npx jest cleanup.dlq
```

- [ ] **Step 3: Implement `routeExhaustedEvents`** and call it from `performCleanup`, replacing the log-only `monitorFailedEvents` (`cleanup.handler.ts:87-123`)

```typescript
export interface FailedRow { id: string; aggregateId: string; eventType: string; retryCount: number; lastError: string | null; }
export interface RouteDeps {
  sendMessage: (input: { QueueUrl: string; MessageBody: string }) => Promise<unknown>;
  putMetric: (m: { value: number }) => Promise<unknown>;
  dlqUrl: string;
}

export const routeExhaustedEvents = async (failed: FailedRow[], deps: RouteDeps): Promise<void> => {
  await deps.putMetric({ value: failed.length });
  for (const row of failed) {
    await deps.sendMessage({ QueueUrl: deps.dlqUrl, MessageBody: JSON.stringify(row) });
  }
  if (failed.length > 0) {
    logger.error('Outbox events exhausted retries -> DLQ', { count: failed.length });
  }
};
```
Wire real deps in `performCleanup` using `@aws-sdk/client-sqs` (`SendMessageCommand`) and `@aws-sdk/client-cloudwatch` (`PutMetricDataCommand`, namespace `TicketBottle/Payment`, metric `OutboxFailedEvents`), reading `OUTBOX_DLQ_URL` from env. Migrate the cleanup's `deleteOldEvents`/`findFailedEvents`/`getCleanupStats` Prisma calls to Kysely (`deleteFrom('outbox').where('publishedAt','is not',null).where('createdAt','<',cutoff)`, etc.).

- [ ] **Step 4: Add DLQ + alarm to `template.yaml`**

```yaml
  OutboxDlq:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: !Sub ticketbottle-outbox-dlq-${Environment}
      MessageRetentionPeriod: 1209600   # 14 days

  OutboxFailedAlarm:
    Type: AWS::CloudWatch::Alarm
    Properties:
      AlarmName: !Sub ticketbottle-outbox-failed-${Environment}
      Namespace: TicketBottle/Payment
      MetricName: OutboxFailedEvents
      Statistic: Maximum
      Period: 300
      EvaluationPeriods: 1
      Threshold: 0
      ComparisonOperator: GreaterThanThreshold
      TreatMissingData: notBreaching
```
Add `OUTBOX_DLQ_URL: !Ref OutboxDlq` to the cleanup function env, and grant it `SQSSendMessagePolicy` + a `cloudwatch:PutMetricData` statement.

- [ ] **Step 5: Run tests — expect PASS**

```bash
cd services/payment-svc/lambdas && npx jest cleanup
```

- [ ] **Step 6: Commit**

```bash
git add services/payment-svc/lambdas/outbox-cleanup services/payment-svc/lambdas/template.yaml
git commit -m "feat(outbox-cleanup): route exhausted events to DLQ + CloudWatch alarm; migrate to Kysely"
```

---

## Task 8: Remove Prisma from the Lambdas (cold-start win)

**Files:**
- Delete: `services/payment-svc/lambdas/prisma/`, `services/payment-svc/lambdas/common/database/prisma.ts`
- Modify: `services/payment-svc/lambdas/package.json`, `services/payment-svc/lambdas/scripts/build-layers.js`

**Interfaces:**
- Consumes: nothing new. All prior Prisma call sites now use the Task 1 Kysely layer.

- [ ] **Step 1: Confirm no Prisma imports remain**

```bash
cd services/payment-svc/lambdas
grep -rn "@prisma/client\|getPrismaClient\|PrismaClient" --include=*.ts . | grep -v node_modules
```
Expected: no output. (If any remain, migrate them to `getDb()` before proceeding.)

- [ ] **Step 2: Delete Prisma artifacts + deps**

```bash
git rm -r services/payment-svc/lambdas/prisma services/payment-svc/lambdas/common/database/prisma.ts
npm pkg delete scripts.prisma:gen
npm uninstall @prisma/client prisma
```

- [ ] **Step 3: Slim the dependencies layer** — in `scripts/build-layers.js`, remove the Prisma-engine copy step (the code that bundles `@prisma/client`, `.prisma`, and the `rhel-openssl-3.0.x` engine binary). The dependencies layer now contains `kysely`, `pg`, `kafkajs`, `@payos/node`, `axios`, `dayjs`, `winston` only.

- [ ] **Step 4: Rebuild layers + verify size dropped**

```bash
cd services/payment-svc/lambdas
npm run build:layers
du -sh build/dependencies-layer/    # expect materially smaller than before (no ~14-16MB engine)
```
Expected: no `libquery_engine-*.node` under `build/dependencies-layer/`.

- [ ] **Step 5: Redeploy the two remaining Lambdas + smoke**

```bash
cd services/payment-svc/lambdas && npm run local:deploy
awslocal lambda invoke --function-name outbox-cleanup --cli-binary-format raw-in-base64-out --payload '{}' /tmp/c.json && cat /tmp/c.json
# drive a webhook through the gate and confirm the payment completes + relay publishes
cd ../../../deploy/localstack && make gate
```
Expected: cleanup returns 200; the gate purchase completes end-to-end (webhook Lambda → outbox row → relay → Kafka → order COMPLETED).

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore(payment-lambdas): drop Prisma; Kysely-only data layer slims the dependencies layer"
```

---

## Deferred to backlog (P2 — out of scope for this plan)

Tracked in `services/payment-svc/lambdas/REVIEW.md`; do after this plan lands:
- **P2-9** secrets → Secrets Manager/SSM (payment-signing keys out of Lambda env vars).
- **P2-10** webhook provider from `event.pathParameters.provider`, drop body-shape sniffing.
- **P2-11** already removed with `prisma.ts` in Task 8 — verify no dangling `$connect`.
- **P2-12** scope SG egress to DB/broker CIDR; make provider-error ack status provider-specific.

---

## Self-Review

**Spec coverage** (against `REVIEW.md`): P0-1 → Tasks 4/6 (relay drains continuously, batch scales); P0-2 → Task 5 (LISTEN/NOTIFY, sub-second); P0-3 → Task 1 (SKIP LOCKED) + Task 5 (per-batch tx); P0-4 → Task 3 (conditional single-transition update); P1-5 → Task 2 (partial index + drop `published`); P1-6 → Task 7 (DLQ + alarm); P1-7 → Task 5 (persistent producer, graceful shutdown); P1-8 → Task 4 (parallel publish + batch mark). All P0/P1 covered. P2 explicitly deferred.

**Type consistency:** `OutboxRow` defined in Task 1, re-exported in Task 5's `db.ts`, consumed in Tasks 4/5. `claimBatch`/`markPublished`/`markFailed` signatures identical across Tasks 1, 4, 5. `drainOnce`/`DrainDeps` defined Task 4, used Task 5. `completePaymentAndEnqueue` defined + used in Task 3. `routeExhaustedEvents`/`RouteDeps` defined + used in Task 7. Column identifiers are quoted camelCase everywhere (Global Constraints).

**Placeholder scan:** every code step carries real code; commands have expected output. Task 6 Step 4 (build-images.sh edit) and Task 7 Step 3 (SDK deps wiring) describe following an existing pattern rather than pasting the whole file — acceptable because they mirror code already in the repo; the executor reads the neighbouring entries.
