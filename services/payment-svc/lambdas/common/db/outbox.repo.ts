import { Kysely, sql } from 'kysely';
import { Database } from './types';

export interface OutboxRow {
  id: string;
  aggregateId: string;
  aggregateType: string;
  eventType: string;
  payload: unknown;
}

// MUST run inside a transaction: FOR UPDATE SKIP LOCKED only excludes rows
// locked by other transactions while the lock is held, i.e. until commit/rollback.
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
