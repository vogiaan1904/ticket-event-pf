export { getDb, getPool, closeDb } from '../../lambdas/common/db/kysely';
export { claimBatch, markPublished, markFailed } from '../../lambdas/common/db/outbox.repo';
export type { OutboxRow } from '../../lambdas/common/db/outbox.repo';

import { getDb } from '../../lambdas/common/db/kysely';
import { claimBatch, markPublished, markFailed, OutboxRow } from '../../lambdas/common/db/outbox.repo';
import { drainOnce } from './relay';
import { publishRow, topicFor } from './kafka';

const BATCH = Number(process.env.OUTBOX_BATCH_SIZE ?? 100);
const MAX_RETRIES = Number(process.env.OUTBOX_MAX_RETRIES ?? 5);

// One cycle drains batches until one comes back empty, so a single wakeup clears
// a whole backlog. Each batch is one transaction: claim (FOR UPDATE SKIP LOCKED)
// -> publish -> mark -> commit.
// Stop on any publish failure: those rows stay claimable, so re-claiming at once
// would burn maxRetries in milliseconds. The next NOTIFY/poll spreads the retry.
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
