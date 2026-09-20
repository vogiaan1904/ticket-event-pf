# payment-svc `src/` Test Coverage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put the NestJS half of the money path under test, and fix the five defects the tests expose.

**Architecture:** Unit tests with mocked collaborators, under `src/`, run by the jest config already in `package.json`. Prisma, the repository, the outbox and the gateways are all injected, so every decision in `PaymentService` is reachable without a database. Each task writes a failing test against real behaviour, then fixes the behaviour.

**Tech Stack:** jest 29, ts-jest 29, `@nestjs/testing` 11 — all three already installed in every TS service.

**Spec:** the ranked backlog in the 2026-09-16 scope pivot, item 2: *"Test the TypeScript services. `api-gateway` and `payment-svc` carry exactly two files, both NestJS scaffold specs. Payment is the money path — webhook idempotency, the outbox relay, the compare-and-set."*

## Scope

**This plan covers `services/payment-svc/src/` only.** `api-gateway`, `user-svc` and `event-svc` are independent subsystems and get their own plans. Payment goes first because it is the only one where a bug moves money.

Out of scope, deliberately:
- `lambdas/` and `outbox-relay/` — they already have 8 tests under their own jest configs.
- Making the existing lambda integration tests pass. **4 of their 5 suites currently fail** (21 tests) because they need a live Postgres on `localhost:5432` that nothing provisions. That is a real problem and it is its own task; do not let it block this one.

## Global Constraints

- **Error taxonomy** (root `CLAUDE.md`, binding): `INTERNAL` means we have a bug. A business outcome must never map to it. `NOT_FOUND` = the entity does not exist. `INVALID_ARGUMENT` = the request is malformed. `FAILED_PRECONDITION` = valid request, wrong world state.
- **The gRPC code is required, not defaulted.** TS services carry it as the third element of the `ErrorCode` tuple: `[message, httpStatus, grpcCode]`.
- **Comment budget** (root `CLAUDE.md`): 3 lines inline, 5 on a symbol, 8 for a file header. No paragraphs.
- **Commit messages describe the platform.** No mention of plans, phases or task numbers.
- **Tests must run with no infrastructure.** No database, no Kafka, no network. If a test needs any of those, it belongs in `lambdas/`, not here.
- Run every command from `services/payment-svc/`.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `package.json` | **Modify.** The `jest.moduleNameMapper` that makes `@/…` resolve under `rootDir: src`. | 1 |
| `src/modules/payment/payment.service.spec.ts` | **Create.** Every `PaymentService` test. One file: these tests share one set of mocks and change together. | 1–5 |
| `src/modules/payment/payment.service.ts` | **Modify.** Four fixes. | 1, 2, 4, 5 |
| `src/shared/constants/error-code.constant.ts` | **Modify.** Adds `InvalidCallback`. | 5 |
| `src/modules/payment/repository/payment.repository.spec.ts` | **Create.** The repository's own contract. | 3, 6 |
| `src/modules/payment/repository/payment.repository.ts` | **Modify.** `create` returns the row it created. | 6 |
| `src/modules/payment/repository/payment.mapper.ts` | **Modify.** Maps the three fields it silently drops. | 3 |
| `.github/workflows/ts-tests.yml` | **Create.** Runs the suite on every `services/**` change. | 7 |

---

### Task 1: Make `src/` testable, and fix the wrong code on a missing payment

`npm test` today reports `Pattern: - 0 matches`. The config has `rootDir: src` and `testRegex: .*\.spec\.ts$`, and there are no spec files under `src/`. It also has **no `moduleNameMapper`**, so the first test that imports `@/shared/...` fails to resolve before it runs a single assertion. That is this task's real prerequisite.

The first behaviour under test is a taxonomy violation. `findByIdempotencyKey` throws `PermissionDenied` (403 / `PERMISSION_DENIED`) when a payment does not exist. `PaymentNotFound` (404 / `NOT_FOUND`) is already defined one line above it in the same file and is what the taxonomy requires. A caller told "403 Forbidden" stops and escalates; a caller told "404" refetches.

**Files:**
- Modify: `package.json` (the `jest` block)
- Create: `src/modules/payment/payment.service.spec.ts`
- Modify: `src/modules/payment/payment.service.ts:149`

**Interfaces:**
- Consumes: nothing.
- Produces: `src/modules/payment/payment.service.spec.ts` exporting nothing, but defining the `buildService()` helper that Tasks 2–4 reuse. Its shape:
  `buildService(): Promise<{ service: PaymentService; repo; outbox; prisma; tx; gateway }>` where `repo`, `outbox`, `gateway` are objects of `jest.fn()`, `prisma.$transaction` invokes its callback with `tx`, and `tx.payment` carries `update`, `updateMany` and `findUnique`.

- [ ] **Step 1: Teach jest the path aliases**

In `package.json`, inside the `jest` object, after `"rootDir": "src",` add:

```json
  "moduleNameMapper": {
    "^@/(.*)$": "<rootDir>/$1",
    "^@modules/(.*)$": "<rootDir>/modules/$1",
    "^@common/(.*)$": "<rootDir>/common/$1",
    "^@infra/(.*)$": "<rootDir>/infra/$1",
    "^@shared/(.*)$": "<rootDir>/shared/$1",
    "^@interfaces/(.*)$": "<rootDir>/shared/interfaces/$1",
    "^@protogen/(.*)$": "<rootDir>/protogen/$1"
  },
```

`rootDir` is `src`, so every target is relative to `src/` — that is why `@/` maps to `<rootDir>/` and not `<rootDir>/src/`. The seven aliases mirror `tsconfig.json`'s `paths` exactly; a mapping that drifts from tsconfig fails only at test time, which is the confusing way to find out.

- [ ] **Step 2: Write the failing test**

Create `src/modules/payment/payment.service.spec.ts`:

```ts
import { status as grpcStatus } from '@grpc/grpc-js';
import { Test } from '@nestjs/testing';
import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { OutboxService } from '@/modules/outbox/outbox.service';
import { LoggerService } from '@/shared/services/logger.service';
import { PaymentGatewayFactory } from './gateways/gateway.factory';
import { PaymentService } from './payment.service';
import { PaymentRepository } from './repository/payment.repository';

// Every collaborator is mocked: these tests are about what PaymentService
// decides, not what Prisma does.
async function buildService() {
  const repo = {
    findByIdempotencyKey: jest.fn(),
    findByProviderTransactionId: jest.fn(),
    create: jest.fn(),
  };
  const outbox = {
    savePaymentCompletedEvent: jest.fn(),
    savePaymentFailedEvent: jest.fn(),
    saveEvent: jest.fn(),
  };
  const tx = {
    payment: { update: jest.fn(), updateMany: jest.fn(), findUnique: jest.fn() },
  };
  const prisma = { $transaction: jest.fn(async (fn: any) => fn(tx)) };
  const gateway = { createPaymentLink: jest.fn(), handleCallback: jest.fn() };
  const factory = { getGateway: jest.fn().mockReturnValue(gateway) };
  const logger = {
    setContext: jest.fn(), log: jest.fn(), info: jest.fn(),
    error: jest.fn(), warn: jest.fn(), debug: jest.fn(),
  };

  const moduleRef = await Test.createTestingModule({
    providers: [
      PaymentService,
      { provide: PaymentRepository, useValue: repo },
      { provide: OutboxService, useValue: outbox },
      { provide: PrismaService, useValue: prisma },
      { provide: PaymentGatewayFactory, useValue: factory },
      { provide: LoggerService, useValue: logger },
    ],
  }).compile();

  return { service: moduleRef.get(PaymentService), repo, outbox, prisma, tx, gateway };
}

describe('PaymentService', () => {
  describe('findByIdempotencyKey', () => {
    it('reports a missing payment as NOT_FOUND', async () => {
      const { service, repo } = await buildService();
      repo.findByIdempotencyKey.mockResolvedValue(null);

      const err: any = await service.findByIdempotencyKey('no-such-key').catch((e) => e);

      // PERMISSION_DENIED tells a caller to stop; NOT_FOUND tells it to refetch.
      expect(err.getError()).toMatchObject({ code: grpcStatus.NOT_FOUND });
    });
  });
});
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
npm test
```

Expected: one failing test, `Received: {"code": 7, ...}` against `"code": 5`. gRPC 7 is `PERMISSION_DENIED`, 5 is `NOT_FOUND`. A resolution error naming `@/infra/...` instead means Step 1's mapper is wrong.

- [ ] **Step 4: Fix the code**

In `src/modules/payment/payment.service.ts:149`, change:

```ts
    if (!payment) throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
```

to:

```ts
    if (!payment) throw new RpcBusinessException(ErrorCodeEnum.PaymentNotFound);
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
npm test
```

Expected: `Tests: 1 passed`.

- [ ] **Step 6: Commit**

```bash
git add package.json src/modules/payment/payment.service.spec.ts src/modules/payment/payment.service.ts
git commit -m "fix(payment): report a missing payment as NOT_FOUND

A lookup that finds nothing answered PERMISSION_DENIED, so a caller was told
it was forbidden from reading a payment that simply does not exist -- stop
rather than refetch. PaymentNotFound was already defined and unused.

The jest config gains the tsconfig path aliases; without them no test under
src/ can resolve an import."
```

---

### Task 2: A duplicate callback must not complete a payment twice

`handleSuccessPayment` reads the payment, then unconditionally sets `COMPLETED` and writes a `PaymentCompleted` row to the outbox. Nothing checks the status it is moving *from*. A provider that retries its callback — which every provider does — produces a second outbox row, the relay publishes it, and order-svc runs `ConfirmOrder` twice for one payment.

The lambda path already gets this right: `lambdas/payment-webhook-handler/__tests__/webhook.concurrency.test.ts` asserts that two concurrent completions produce exactly one outbox row. This task brings the NestJS path to the same guarantee.

**Files:**
- Modify: `src/modules/payment/payment.service.spec.ts`
- Modify: `src/modules/payment/payment.service.ts:26-48`

**Interfaces:**
- Consumes: `buildService()` from Task 1.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append inside the top-level `describe('PaymentService', ...)` in `payment.service.spec.ts`:

```ts
  describe('handleSuccessPayment, via handleCallback', () => {
    it('emits one completed event when the provider retries its callback', async () => {
      const { service, repo, outbox, tx, gateway } = await buildService();
      gateway.handleCallback.mockResolvedValue({
        success: true, providerTransactionId: 'tx-1', response: { ok: true },
      });
      repo.findByProviderTransactionId.mockResolvedValue({ orderCode: 'ORD-1' });
      // First call transitions PENDING -> COMPLETED; the retry matches no PENDING row.
      tx.payment.updateMany
        .mockResolvedValueOnce({ count: 1 })
        .mockResolvedValueOnce({ count: 0 });
      tx.payment.findUnique.mockResolvedValue({ id: 'pay-1', orderCode: 'ORD-1' });

      await service.handleCallback('zalopay' as any, {});
      await service.handleCallback('zalopay' as any, {});

      expect(outbox.savePaymentCompletedEvent).toHaveBeenCalledTimes(1);
    });
  });
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
npm test
```

Expected: `Expected number of calls: 1, Received number of calls: 2`. The current code never calls `updateMany`, so both passes reach the outbox.

- [ ] **Step 3: Fix the code**

Replace the body of `handleSuccessPayment` in `src/modules/payment/payment.service.ts:26-48` with:

```ts
  private async handleSuccessPayment(providerTransactionId: string): Promise<void> {
    const now = new Date();

    const existingPayment = await this.repo.findByProviderTransactionId(providerTransactionId);
    if (!existingPayment) {
      this.logger.error(`Payment not found for providerTransactionId: ${providerTransactionId}`);
      throw new Error(`Payment not found for providerTransactionId: ${providerTransactionId}`);
    }

    await this.prisma.$transaction(async (tx) => {
      // Compare-and-set: only a PENDING payment may complete. Providers retry
      // callbacks, and a second COMPLETED write would publish a second event.
      const claimed = await tx.payment.updateMany({
        where: { orderCode: existingPayment.orderCode, status: PaymentStatus.PENDING },
        data: { status: PaymentStatus.COMPLETED, completedAt: now },
      });
      if (claimed.count === 0) return;

      const payment = await tx.payment.findUnique({
        where: { orderCode: existingPayment.orderCode },
      });
      await this.outboxService.savePaymentCompletedEvent(payment, tx);
    });

    this.logger.info(
      `Payment completed: orderCode=${existingPayment.orderCode}, providerTransactionId=${providerTransactionId}`,
    );
  }
```

`updateMany` is what makes this atomic: it applies the `status` predicate inside the same statement that writes, so two concurrent callbacks cannot both see `PENDING`. `update` takes a unique `where` and cannot carry a status guard.

- [ ] **Step 4: Run the test to verify it passes**

```bash
npm test
```

Expected: `Tests: 2 passed`.

- [ ] **Step 5: Commit**

```bash
git add src/modules/payment/payment.service.spec.ts src/modules/payment/payment.service.ts
git commit -m "fix(payment): complete a payment only from PENDING

The completion wrote COMPLETED and an outbox event without checking the status
it was moving from, so a retried provider callback published a second
PaymentCompleted and order-svc confirmed the same order twice. The guard is the
same compare-and-set the webhook lambda already uses."
```

---

### Task 3: The mapper silently drops `paymentUrl`

`toPaymentEntity` assigns 13 of `PaymentEntity`'s 17 fields. `paymentUrl`, `redirectUrl` and `cancelledAt` are never assigned, so they are `undefined` on every entity the repository returns — and `PaymentEntity implements Payment` makes TypeScript believe otherwise.

This is live, not latent. `createPaymentIntent:126` returns `existing.paymentUrl` on an idempotency-key hit, so **every repeat request for a key already in the database answers `undefined` instead of a payment url.** Task 4 depends on this being fixed: its whole fix is returning the winner's `paymentUrl`.

**Files:**
- Create: `src/modules/payment/repository/payment.repository.spec.ts`
- Modify: `src/modules/payment/repository/payment.mapper.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `toPaymentEntity` populating `paymentUrl`, `redirectUrl` and `cancelledAt`. Task 4 relies on `paymentUrl`.

- [ ] **Step 1: Write the failing test**

Create `src/modules/payment/repository/payment.repository.spec.ts`:

```ts
import { Test } from '@nestjs/testing';
import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { PaymentRepository } from './payment.repository';

// One row, shaped like Prisma's Payment, reused by every case here.
const row = {
  id: 'pay-1', orderCode: 'ORD-1', amountCents: 1000, currency: 'VND',
  provider: 'zalopay', providerTransactionId: 'tx-1', idempotencyKey: 'idem-1',
  redirectUrl: 'https://shop/return', paymentUrl: 'https://pay/1', status: 'PENDING',
  metadata: null, completedAt: null, failedAt: null, cancelledAt: null,
  createdAt: new Date(), updatedAt: new Date(),
};

async function buildRepo(prisma: any) {
  const moduleRef = await Test.createTestingModule({
    providers: [PaymentRepository, { provide: PrismaService, useValue: prisma }],
  }).compile();
  return moduleRef.get(PaymentRepository);
}

describe('PaymentRepository', () => {
  it('keeps the payment url a lookup is asked for', async () => {
    const repo = await buildRepo({ payment: { findUnique: jest.fn().mockResolvedValue(row) } });

    const found = await repo.findByIdempotencyKey('idem-1');

    // createPaymentIntent returns this field directly on an idempotency hit.
    expect(found?.paymentUrl).toBe('https://pay/1');
    expect(found?.redirectUrl).toBe('https://shop/return');
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
npm test
```

Expected: `Expected: "https://pay/1", Received: undefined`.

- [ ] **Step 3: Fix the mapper**

In `src/modules/payment/repository/payment.mapper.ts`, add the three missing assignments after `entity.status`:

```ts
  entity.redirectUrl = prismaPayment.redirectUrl;
  entity.paymentUrl = prismaPayment.paymentUrl;
```

and after `entity.failedAt`:

```ts
  entity.cancelledAt = prismaPayment.cancelledAt;
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
npm test
```

Expected: `Tests: 3 passed`, across 2 suites.

- [ ] **Step 5: Commit**

```bash
git add src/modules/payment/repository/payment.repository.spec.ts src/modules/payment/repository/payment.mapper.ts
git commit -m "fix(payment): carry the payment url out of the mapper

toPaymentEntity assigned 13 of the entity's 17 fields and left paymentUrl,
redirectUrl and cancelledAt undefined, while 'implements Payment' told the
compiler they were populated. A repeat request for an idempotency key already
in the database therefore answered undefined instead of the url to pay at."
```

---

### Task 4: Two requests with one idempotency key must not buy two payment links

`createPaymentIntent` reads by idempotency key, and on a miss calls the provider and inserts. Two concurrent requests with the same key both miss, both create a payment link at the provider, and then one insert loses to the unique constraint — leaving an orphaned link the buyer can still pay against.

The insert is the only real arbiter, because the database holds the unique constraint. This task makes the loser recover the winner's URL instead of propagating a Prisma error.

**Files:**
- Modify: `src/modules/payment/payment.service.spec.ts`
- Modify: `src/modules/payment/payment.service.ts:122-145`

**Interfaces:**
- Consumes: `buildService()` from Task 1.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append inside the top-level `describe`:

```ts
  describe('createPaymentIntent', () => {
    it('returns the winner\'s url when the idempotency key is already taken', async () => {
      const { service, repo, gateway } = await buildService();
      repo.findByIdempotencyKey
        .mockResolvedValueOnce(null)                            // the pre-check misses
        .mockResolvedValueOnce({ paymentUrl: 'https://pay/winner' }); // the re-read after P2002
      gateway.createPaymentLink.mockResolvedValue({
        url: 'https://pay/loser', transactionId: 'tx-loser',
      });
      // Prisma's unique-constraint violation.
      repo.create.mockRejectedValue(Object.assign(new Error('unique'), { code: 'P2002' }));

      const url = await service.createPaymentIntent({
        idempotencyKey: 'idem-1', provider: 'zalopay', amountCents: 1000,
        orderCode: 'ORD-1', currency: 'VND', redirectUrl: 'r', timeoutSeconds: 60,
      } as any);

      expect(url).toBe('https://pay/winner');
    });
  });
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
npm test
```

Expected: the test rejects with `unique` — the P2002 propagates, because nothing catches it.

- [ ] **Step 3: Fix the code**

In `src/modules/payment/payment.service.ts`, replace `await this.repo.create(dto);` and the `return url;` that follows it with:

```ts
    try {
      await this.repo.create(dto);
    } catch (error) {
      // P2002 = the unique idempotencyKey is taken, so a concurrent request won.
      // Its URL is the one the buyer must be sent to; ours is now orphaned.
      if ((error as { code?: string }).code !== 'P2002') throw error;
      const winner = await this.repo.findByIdempotencyKey(dto.idempotencyKey);
      if (winner) return winner.paymentUrl;
      throw error;
    }

    return url;
```

**Known residual, deliberately not fixed here:** both requests still call the provider, so a second payment link exists at the gateway even though only one is persisted. Closing that needs the row reserved before the gateway call, which changes the schema (`paymentUrl` must become nullable) and belongs in its own change.

- [ ] **Step 4: Run the test to verify it passes**

```bash
npm test
```

Expected: `Tests: 4 passed`.

- [ ] **Step 5: Commit**

```bash
git add src/modules/payment/payment.service.spec.ts src/modules/payment/payment.service.ts
git commit -m "fix(payment): return the winning payment url on a duplicate key

The idempotency check was a read followed by an unguarded insert, so two
concurrent requests for one key both reached the provider and the loser then
died on the unique constraint. The loser now recovers the winner's url, which
is the one the buyer must be sent to."
```

---

### Task 5: A callback with no transaction id must fail loudly

`handleCallback` logs an error when the gateway returns no `providerTransactionId`, then falls through and returns `output.response` — the provider is told the callback succeeded while nothing was recorded. A malformed callback is a malformed request: `INVALID_ARGUMENT`, per the taxonomy.

**Files:**
- Modify: `src/shared/constants/error-code.constant.ts`
- Modify: `src/modules/payment/payment.service.spec.ts`
- Modify: `src/modules/payment/payment.service.ts:154-168`

**Interfaces:**
- Consumes: `buildService()` from Task 1.
- Produces: `ErrorCodeEnum.InvalidCallback`, mapped to `['Invalid callback payload', 400, grpcStatus.INVALID_ARGUMENT]`.

- [ ] **Step 1: Write the failing test**

Append inside the top-level `describe`:

```ts
  describe('handleCallback', () => {
    it('rejects a callback that carries no transaction id', async () => {
      const { service, outbox, gateway } = await buildService();
      gateway.handleCallback.mockResolvedValue({
        success: true, providerTransactionId: undefined, response: { ok: true },
      });

      const err: any = await service.handleCallback('zalopay' as any, {}).catch((e) => e);

      expect(err.getError()).toMatchObject({ code: grpcStatus.INVALID_ARGUMENT });
      expect(outbox.savePaymentCompletedEvent).not.toHaveBeenCalled();
    });
  });
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
npm test
```

Expected: `err.getError is not a function` — nothing is thrown, so `err` is the returned response object.

- [ ] **Step 3: Add the error code**

In `src/shared/constants/error-code.constant.ts`, add to the enum:

```ts
  InvalidCallback = 20400,
```

and to the `ErrorCode` map:

```ts
  [ErrorCodeEnum.InvalidCallback]: ['Invalid callback payload', 400, grpcStatus.INVALID_ARGUMENT],
```

- [ ] **Step 4: Fix the code**

In `src/modules/payment/payment.service.ts`, replace the `if (!output.providerTransactionId)` branch with:

```ts
    if (!output.providerTransactionId) {
      this.logger.error('Callback handling failed - missing providerTransactionId');
      throw new RpcBusinessException(ErrorCodeEnum.InvalidCallback);
    }
```

and drop the now-dead `else` on the following branch, leaving `if (output.success) { … } else { … }`.

- [ ] **Step 5: Run the test to verify it passes**

```bash
npm test
```

Expected: `Tests: 5 passed`.

- [ ] **Step 6: Commit**

```bash
git add src/shared/constants/error-code.constant.ts src/modules/payment/payment.service.spec.ts src/modules/payment/payment.service.ts
git commit -m "fix(payment): reject a callback with no transaction id

A callback the gateway could not attribute was logged and then answered as if
it had succeeded, so the provider stopped retrying a payment nothing recorded.
It is a malformed request and now answers INVALID_ARGUMENT."
```

---

### Task 6: The repository must return the row it created

`PaymentRepository.create` inserts and then returns `{} as PaymentEntity` — a cast over an empty object. Every field a caller reads is `undefined`, and TypeScript is silent because the cast asserts otherwise. `createPaymentIntent` happens to ignore the return today, so this is latent rather than live; it is the kind of latent that becomes a null-pointer the first time someone trusts the signature.

**Files:**
- Modify: `src/modules/payment/repository/payment.repository.spec.ts` (created in Task 3)
- Modify: `src/modules/payment/repository/payment.repository.ts:12-27`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append a second case inside the existing `describe('PaymentRepository', ...)` in `src/modules/payment/repository/payment.repository.spec.ts`. It reuses the `row` fixture and `buildRepo` helper Task 3 created:

```ts
  it('returns the created payment, not an empty object', async () => {
    const repo = await buildRepo({ payment: { create: jest.fn().mockResolvedValue(row) } });

    const created = await repo.create({
      idempotencyKey: 'idem-1', orderCode: 'ORD-1',
    } as any);

    expect(created.orderCode).toBe('ORD-1');
  });
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
npm test
```

Expected: `Expected: "ORD-1", Received: undefined`.

- [ ] **Step 3: Fix the code**

In `src/modules/payment/repository/payment.repository.ts`, change `create` to keep and map the row:

```ts
  async create(dto: CreatePaymentIntentDto): Promise<PaymentEntity> {
    const payment = await this.prisma.payment.create({
      data: {
        amountCents: dto.amountCents,
        currency: dto.currency,
        idempotencyKey: dto.idempotencyKey,
        orderCode: dto.orderCode,
        redirectUrl: dto.redirectUrl,
        status: PaymentStatus.PENDING,
        provider: dto.provider,
        providerTransactionId: dto.transactionId,
        paymentUrl: dto.paymentUrl,
      },
    });
    return toPaymentEntity(payment);
  }
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
npm test
```

Expected: `Tests: 6 passed`, across 2 suites.

- [ ] **Step 5: Commit**

```bash
git add src/modules/payment/repository/payment.repository.spec.ts src/modules/payment/repository/payment.repository.ts
git commit -m "fix(payment): return the created row from the repository

create inserted the payment and returned an empty object cast to PaymentEntity,
so the signature promised a payment and delivered undefined on every field. No
caller reads it yet, which is the only reason this has not surfaced."
```

---

### Task 7: CI runs the suite

Nothing runs any TypeScript test. `build-push-ecr.yml` builds images and `chart-assertions.yml` renders the chart; neither executes jest. A test nobody runs is documentation.

Scope this to `payment-svc` only. Adding the other three services now would wire up three suites that contain no tests, and a green check that asserts nothing is worse than no check.

**Files:**
- Create: `.github/workflows/ts-tests.yml`

**Interfaces:**
- Consumes: the spec files from Tasks 1–5.
- Produces: nothing new.

- [ ] **Step 1: Write the workflow**

Create `.github/workflows/ts-tests.yml` **at the repository root** (not in the service directory):

```yaml
name: ts-tests

# Unit tests only: they mock every collaborator, so the runner needs no
# database, broker or network.
on:
  push:
    branches: [main, dev]
    paths:
      - "services/payment-svc/**"
      - ".github/workflows/ts-tests.yml"
  pull_request:
    paths:
      - "services/payment-svc/**"
  workflow_dispatch: {}

permissions:
  contents: read

jobs:
  payment-svc:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: services/payment-svc
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: "22"

      - run: npm ci

      - name: Generate the Prisma client
        run: npx prisma generate

      - run: npm test
```

`prisma generate` is not optional: `payment.service.ts` imports `PaymentStatus` from `@prisma/client`, which does not exist in a fresh `node_modules` until the client is generated from `prisma/schema.prisma`.

- [ ] **Step 2: Verify the whole suite passes from a clean install**

Reproduce what the runner does, in a scratch copy so the working tree is untouched:

```bash
cd "$(mktemp -d)" && git clone --depth 1 file://$HOME/coding/projects/TicketEventPF r \
  && cd r/services/payment-svc && npm ci && npx prisma generate && npm test
```

Expected: `Tests: 6 passed`. A failure here is a failure CI will hit; fix it before pushing.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ts-tests.yml
git commit -m "ci: run the payment service's unit tests

The money path's tests ran only when someone remembered to. They mock every
collaborator, so a bare runner with node and a generated Prisma client is
enough. Scoped to payment-svc: the other three services have no tests yet, and
a green check over an empty suite asserts nothing."
```

- [ ] **Step 4: Push and confirm the run is green**

```bash
git push origin dev
gh run list --branch dev --limit 1
```

Expected: `ts-tests` completed / success.

---

## Done when

- [ ] `npm test` in `services/payment-svc` reports 6 passing tests across 2 suites.
- [ ] The same passes from a clean `npm ci` in a fresh clone.
- [ ] `ts-tests` is green on `dev`.
- [ ] No test requires a database, a broker or the network.
- [ ] No `INTERNAL` is reachable from a business outcome in the paths touched.

## Not in this plan

- `api-gateway`, `user-svc`, `event-svc` — one plan each, same shape.
- The 21 failing lambda integration tests, which need a Postgres nothing provisions.
- Reserving the payment row before the gateway call, which would close the duplicate-link residual in Task 3 and needs a schema change.
- The `PaymentCancelled` event's routing, which is a known deferred follow-up.
