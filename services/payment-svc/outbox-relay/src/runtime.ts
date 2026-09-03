import { Client } from 'pg';
import { closeDb, composeDrain } from './db';
import { getKafkaProducer, disconnectKafka } from './kafka';
import { logger } from './logger';

// Serializes drains, coalescing mid-drain triggers into one follow-up run, so a
// burst of NOTIFYs never launches concurrent drains against the same rows.
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
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;

export const startRuntime = async (): Promise<() => Promise<void>> => {
  await getKafkaProducer(); // connect the long-lived producer once at boot

  const scheduler = createDrainScheduler(composeDrain());

  // Dedicated LISTEN connection: a pooled client returns to the pool between
  // queries and silently drops the registration. scheduleReconnect below revives
  // it with backoff; the safety poll covers the gap while disconnected.
  let listener: Client | undefined;
  let stopped = false;
  // True from queueing a reconnect until that attempt settles, so an error/end
  // firing mid-attempt cannot queue an overlapping one.
  let reconnecting = false;
  let backoffMs = RECONNECT_BASE_MS;
  let reconnectTimer: NodeJS.Timeout | undefined;

  const connectListener = async (): Promise<Client> => {
    const client = new Client({ connectionString: process.env.DATABASE_URL });
    client.on('notification', () => scheduler.trigger());
    client.on('error', (e) => {
      logger.error('listener error; scheduling reconnect', { error: e.message });
      scheduleReconnect();
    });
    client.on('end', () => scheduleReconnect());
    await client.connect();
    try {
      await client.query('LISTEN outbox_new');
    } catch (e) {
      await client.end().catch(() => {}); // don't leak the socket if LISTEN itself fails
      throw e;
    }
    return client;
  };

  const scheduleReconnect = (): void => {
    if (stopped || reconnecting) return;
    reconnecting = true;
    const delay = backoffMs;
    backoffMs = Math.min(backoffMs * 2, RECONNECT_MAX_MS);
    reconnectTimer = setTimeout(() => {
      if (stopped) {
        reconnecting = false;
        return;
      }
      connectListener()
        .then((client) => {
          if (stopped) { client.end().catch(() => {}); return; }
          listener = client;
          backoffMs = RECONNECT_BASE_MS;
          reconnecting = false;
          scheduler.trigger(); // catch any NOTIFY missed while disconnected
        })
        .catch((e) => {
          logger.error('listener reconnect attempt failed', { error: (e as Error).message });
          reconnecting = false;
          scheduleReconnect();
        });
    }, delay);
  };

  listener = await connectListener();

  // Safety poll covers any NOTIFY missed while the listener is down/reconnecting.
  const safety = setInterval(() => scheduler.trigger(), SAFETY_POLL_MS);
  scheduler.trigger(); // drain whatever is already pending at boot

  logger.info('outbox-relay started', { safetyPollMs: SAFETY_POLL_MS });

  return async () => {
    stopped = true; // stop scheduling further reconnects during shutdown
    clearInterval(safety);
    if (reconnectTimer) clearTimeout(reconnectTimer);
    try {
      await listener?.end();
    } catch (e) {
      logger.warn('listener end failed during shutdown', { error: (e as Error).message });
    }
    try {
      await disconnectKafka();
    } catch (e) {
      logger.warn('kafka disconnect failed during shutdown', { error: (e as Error).message });
    }
    try {
      await closeDb();
    } catch (e) {
      logger.warn('db close failed during shutdown', { error: (e as Error).message });
    }
    logger.info('outbox-relay stopped');
  };
};
