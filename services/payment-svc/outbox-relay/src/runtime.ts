import { Client } from 'pg';
import { closeDb, composeDrain } from './db';
import { getKafkaProducer, disconnectKafka } from './kafka';
import { logger } from './logger';

// Serializes drains and coalesces any triggers that arrive mid-drain into a single
// follow-up run, so a burst of NOTIFYs (or NOTIFY + safety-poll firing together)
// never launches concurrent drains against the same rows.
export const createDrainScheduler = (drain: () => Promise<void>): { trigger: () => void } => {
  let running = false;
  let pending = false;

  const run = async (): Promise<void> => {
    if (running) {
      pending = true;
      return;
    }
    running = true;
    try {
      await drain();
    } catch (e) {
      logger.error('drain cycle failed', { error: (e as Error).message });
    } finally {
      running = false;
      if (pending) {
        pending = false;
        void run();
      }
    }
  };

  return { trigger: () => void run() };
};

const SAFETY_POLL_MS = Number(process.env.RELAY_SAFETY_POLL_MS ?? 5000);

export const startRuntime = async (): Promise<() => Promise<void>> => {
  await getKafkaProducer(); // connect the long-lived producer once at boot

  const scheduler = createDrainScheduler(composeDrain());

  // Dedicated LISTEN connection: a pooled client would be handed back to the pool
  // between queries, silently dropping the LISTEN registration.
  const listener = new Client({ connectionString: process.env.DATABASE_URL });
  await listener.connect();
  await listener.query('LISTEN outbox_new');
  listener.on('notification', () => scheduler.trigger());
  listener.on('error', (e) => logger.error('listener error; relying on safety poll', { error: e.message }));

  // Safety poll covers any missed NOTIFY (e.g. the listener reconnecting after an error).
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
