import { randomUUID } from 'crypto';
import { getDb, closeDb } from '../kysely';
import { claimBatch, markPublished } from '../outbox.repo';

// id has no DB default (Prisma's @default(uuid()) is client-side), and createdAt
// must differ per row or the oldest-first ordering ties at statement timestamp.
const seed = async () => {
  const db = getDb();
  await db.deleteFrom('outbox').execute();
  await db
    .insertInto('outbox')
    .values([
      {
        id: randomUUID(),
        aggregateId: 'p1',
        aggregateType: 'payment',
        eventType: 'PaymentCompleted',
        payload: JSON.stringify({}),
        retryCount: 0,
        createdAt: new Date(Date.now() - 1000),
      },
      {
        id: randomUUID(),
        aggregateId: 'p2',
        aggregateType: 'payment',
        eventType: 'PaymentCompleted',
        payload: JSON.stringify({}),
        retryCount: 0,
        createdAt: new Date(),
      },
    ])
    .execute();
};

afterAll(async () => {
  await closeDb();
});

test('claimBatch returns unpublished rows oldest-first', async () => {
  await seed();
  const rows = await claimBatch(getDb(), 10, 5);
  expect(rows.map((r) => r.aggregateId)).toEqual(['p1', 'p2']);
});

test('markPublished sets publishedAt so rows are no longer claimable', async () => {
  await seed();
  const rows = await claimBatch(getDb(), 10, 5);
  await markPublished(
    getDb(),
    rows.map((r) => r.id),
  );
  const again = await claimBatch(getDb(), 10, 5);
  expect(again).toHaveLength(0);
});

test('two concurrent claims never return the same row (SKIP LOCKED)', async () => {
  await seed();
  const db = getDb();
  // Hold each transaction open after claiming so the two FOR UPDATE statements
  // genuinely overlap; otherwise one commits before the other queries and
  // SKIP LOCKED is never exercised.
  const claimAndHold = () =>
    db.transaction().execute(async (trx) => {
      const rows = await claimBatch(trx, 10, 5);
      await new Promise((resolve) => setTimeout(resolve, 50));
      return rows;
    });
  const [a, b] = await Promise.all([claimAndHold(), claimAndHold()]);
  const ids = [...a, ...b].map((r) => r.id);
  expect(new Set(ids).size).toBe(ids.length);
});
