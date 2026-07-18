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
// Looping inside composeDrain (rather than relying solely on LISTEN/safety-poll
// retriggers) means a single wakeup fully drains a large backlog immediately.
// A batch with any publish failures stops the loop instead of re-claiming
// instantly: the failed rows are still SKIP LOCKED-eligible, so an instant
// re-claim would just re-fail them back-to-back with no delay, burning through
// maxRetries in milliseconds. Stopping here defers the retry to the next
// NOTIFY/safety-poll trigger, which spreads retries out over time.
export const composeDrain = () => async (): Promise<void> => {
  for (;;) {
    const result = await getDb()
      .transaction()
      .execute((trx) =>
        drainOnce({
          claim: (limit, mr) => claimBatch(trx, limit, mr),
          publish: (row: OutboxRow, topic: string) => publishRow(topic, row),
          markPublished: (ids) => markPublished(trx, ids),
          markFailed: (id, err) => markFailed(trx, id, err),
          topicFor,
          batchSize: BATCH,
          maxRetries: MAX_RETRIES,
        }),
      );
    if (result.processed === 0 || result.failed > 0) break;
  }
};
