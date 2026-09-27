import { Histogram, Gauge, collectDefaultMetrics, register } from 'prom-client';
import { createServer, Server } from 'http';
import { getDb } from '../../lambdas/common/db/kysely';
import { countUnpublished } from '../../lambdas/common/db/outbox.repo';
import { MAX_RETRIES } from './config';
import { logger } from './logger';

const METRICS_PORT = Number(process.env.SERVER_METRICS_PORT || 2112);
const PENDING_REFRESH_MS = Number(process.env.RELAY_PENDING_REFRESH_MS ?? 15000);

collectDefaultMetrics();

export const outboxPendingRows = new Gauge({
  name: 'tb_outbox_pending_rows',
  help: 'Unpublished outbox rows the relay will still try to publish.',
});

export const outboxExhaustedRows = new Gauge({
  name: 'tb_outbox_exhausted_rows',
  help: 'Unpublished outbox rows past OUTBOX_MAX_RETRIES, which the relay never claims again.',
});

// Buckets span a NOTIFY-driven publish (tens of ms) to the safety poll and a
// backlog drain. Past 60s the relay is not keeping up, which is the alert.
export const outboxPublishLag = new Histogram({
  name: 'tb_outbox_publish_lag_seconds',
  help: 'Seconds from an outbox row being written to its Kafka ack.',
  buckets: [0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120],
});

export const observePublishLag = (createdAt: Date): void => {
  outboxPublishLag.observe(Math.max(0, (Date.now() - createdAt.getTime()) / 1000));
};

// Refreshed on a timer because the relay has no request to hang it on: with a
// per-cycle update only, an idle relay reports the last cycle's backlog, and a
// stale gauge reads exactly like a healthy one.
export const refreshOutboxRows = async (db: ReturnType<typeof getDb>): Promise<void> => {
  const { pending, exhausted } = await countUnpublished(db, MAX_RETRIES);
  outboxPendingRows.set(pending);
  outboxExhaustedRows.set(exhausted);
};

// startMetrics starts the :port/metrics listener and the outbox-rows poll.
// Returns the stop function the caller adds to its SIGTERM path -- an
// un-cleared interval keeps the process from exiting.
export const startMetrics = (): (() => Promise<void>) => {
  const server: Server = createServer((req, res) => {
    if (req.url !== '/metrics') {
      res.writeHead(404).end();
      return;
    }
    register
      .metrics()
      .then((body) => res.writeHead(200, { 'Content-Type': register.contentType }).end(body))
      .catch(() => res.writeHead(500).end());
  }).listen(METRICS_PORT);

  const poll = setInterval(() => {
    void refreshOutboxRows(getDb()).catch((e) =>
      logger.warn('outbox-rows refresh failed', { error: (e as Error).message }),
    );
  }, PENDING_REFRESH_MS);

  logger.info('outbox-relay metrics started', { port: METRICS_PORT });

  return async () => {
    clearInterval(poll);
    await new Promise<void>((resolve) => server.close(() => resolve()));
  };
};
