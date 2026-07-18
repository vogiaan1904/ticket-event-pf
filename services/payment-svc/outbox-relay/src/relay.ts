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

export interface DrainResult {
  processed: number;
  succeeded: number;
  failed: number;
}

// Deps are injected so this orchestration is unit-testable without a DB/Kafka;
// runtime.ts (Task 5) composes the real claim/publish/mark implementations,
// running claim+mark inside one transaction so SKIP LOCKED rows stay locked
// until the batch is marked.
export const drainOnce = async (deps: DrainDeps): Promise<DrainResult> => {
  const rows = await deps.claim(deps.batchSize, deps.maxRetries);
  if (rows.length === 0) return { processed: 0, succeeded: 0, failed: 0 };

  // allSettled: one row's publish failure must not abort the others in the batch.
  const results = await Promise.allSettled(
    rows.map((row) => deps.publish(row, deps.topicFor(row.eventType))),
  );

  const succeededIds: string[] = [];
  let failed = 0;
  await Promise.all(
    results.map(async (result, i) => {
      if (result.status === 'fulfilled') {
        succeededIds.push(rows[i].id);
      } else {
        failed++;
        await deps.markFailed(rows[i].id, String(result.reason?.message ?? result.reason));
      }
    }),
  );

  await deps.markPublished(succeededIds);
  return { processed: rows.length, succeeded: succeededIds.length, failed };
};
