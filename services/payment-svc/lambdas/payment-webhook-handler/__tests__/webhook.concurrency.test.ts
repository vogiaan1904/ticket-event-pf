import { getDb, closeDb } from '@/common/db/kysely';
import { completePaymentAndEnqueue } from '../handlers/webhook.handler';

afterAll(async () => { await closeDb(); });

beforeEach(async () => {
  const db = getDb();
  await db.deleteFrom('outbox').execute();
  await db.deleteFrom('payments').execute();
  // updatedAt has no DB-level default (Prisma's @updatedAt is client-side only), so it
  // must be supplied explicitly here — same gotcha already documented for `id` in
  // common/db/__tests__/outbox.repo.test.ts.
  await db.insertInto('payments').values({
    id: 'pay-1', orderCode: 'ORD-1', amountCents: 1000, currency: 'VND', provider: 'zalopay',
    providerTransactionId: 'tx-1', idempotencyKey: 'idem-1', redirectUrl: 'x', paymentUrl: 'y', status: 'PENDING',
    updatedAt: new Date(),
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
