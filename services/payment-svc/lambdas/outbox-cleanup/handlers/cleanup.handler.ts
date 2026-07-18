import { EventBridgeEvent } from 'aws-lambda';
import { SQSClient, SendMessageCommand } from '@aws-sdk/client-sqs';
import { CloudWatchClient, PutMetricDataCommand } from '@aws-sdk/client-cloudwatch';
import { getDb } from '@/common/db/kysely';
import { logger } from '@/common/logger';
import { getConfig } from '@/common/config';

const sqsClient = new SQSClient({});
const cloudWatchClient = new CloudWatchClient({});

const METRIC_NAMESPACE = 'TicketBottle/Payment';
const METRIC_NAME = 'OutboxFailedEvents';

export interface FailedRow {
  id: string;
  aggregateId: string;
  eventType: string;
  retryCount: number;
  lastError: string | null;
}

export interface RouteDeps {
  sendMessage: (input: { QueueUrl: string; MessageBody: string }) => Promise<unknown>;
  putMetric: (m: { value: number }) => Promise<unknown>;
  dlqUrl: string;
}

export const routeExhaustedEvents = async (failed: FailedRow[], deps: RouteDeps): Promise<void> => {
  await deps.putMetric({ value: failed.length });
  for (const row of failed) {
    await deps.sendMessage({ QueueUrl: deps.dlqUrl, MessageBody: JSON.stringify(row) });
  }
  if (failed.length > 0) {
    logger.error('Outbox events exhausted retries -> DLQ', { count: failed.length });
  }
};

const getDlqUrl = (): string => {
  const url = process.env.OUTBOX_DLQ_URL;
  if (!url) throw new Error('OUTBOX_DLQ_URL is required');
  return url;
};

const deleteOldEvents = async (): Promise<number> => {
  const config = getConfig();
  const db = getDb();

  const retentionDays = config.outbox.retentionDays;
  const cutoffDate = new Date();
  cutoffDate.setDate(cutoffDate.getDate() - retentionDays);

  const result = await db
    .deleteFrom('outbox')
    .where('publishedAt', 'is not', null)
    .where('publishedAt', '<', cutoffDate)
    .executeTakeFirst();

  const count = Number(result.numDeletedRows);

  logger.info('Old outbox events deleted', { count, retentionDays });

  return count;
};

const findFailedEvents = async (): Promise<FailedRow[]> => {
  const config = getConfig();
  const db = getDb();

  return db
    .selectFrom('outbox')
    .select(['id', 'aggregateId', 'eventType', 'retryCount', 'lastError'])
    .where('publishedAt', 'is', null)
    .where('retryCount', '>=', config.outbox.maxRetries)
    .orderBy('createdAt', 'asc')
    .execute();
};

export interface CleanupStats {
  totalEvents: number;
  publishedEvents: number;
  pendingEvents: number;
  failedEvents: number;
  oldestPendingAge: number | null;
}

const getCleanupStats = async (): Promise<CleanupStats> => {
  const config = getConfig();
  const db = getDb();
  const maxRetries = config.outbox.maxRetries;

  const [totalRow, publishedRow, pendingRow, failedRow] = await Promise.all([
    db
      .selectFrom('outbox')
      .select((eb) => eb.fn.countAll<number>().as('count'))
      .executeTakeFirstOrThrow(),
    db
      .selectFrom('outbox')
      .select((eb) => eb.fn.countAll<number>().as('count'))
      .where('publishedAt', 'is not', null)
      .executeTakeFirstOrThrow(),
    db
      .selectFrom('outbox')
      .select((eb) => eb.fn.countAll<number>().as('count'))
      .where('publishedAt', 'is', null)
      .where('retryCount', '<', maxRetries)
      .executeTakeFirstOrThrow(),
    db
      .selectFrom('outbox')
      .select((eb) => eb.fn.countAll<number>().as('count'))
      .where('publishedAt', 'is', null)
      .where('retryCount', '>=', maxRetries)
      .executeTakeFirstOrThrow(),
  ]);

  const oldestPending = await db
    .selectFrom('outbox')
    .select('createdAt')
    .where('publishedAt', 'is', null)
    .orderBy('createdAt', 'asc')
    .executeTakeFirst();

  const oldestPendingAge = oldestPending
    ? Math.floor((Date.now() - oldestPending.createdAt.getTime()) / 1000 / 60)
    : null;

  return {
    totalEvents: Number(totalRow.count),
    publishedEvents: Number(publishedRow.count),
    pendingEvents: Number(pendingRow.count),
    failedEvents: Number(failedRow.count),
    oldestPendingAge,
  };
};

export const performCleanup = async (): Promise<{
  deleted: number;
  failedCount: number;
  stats: CleanupStats;
}> => {
  const deleted = await deleteOldEvents();
  const failedEvents = await findFailedEvents();

  await routeExhaustedEvents(failedEvents, {
    sendMessage: (input) => sqsClient.send(new SendMessageCommand(input)),
    putMetric: (m) =>
      cloudWatchClient.send(
        new PutMetricDataCommand({
          Namespace: METRIC_NAMESPACE,
          MetricData: [{ MetricName: METRIC_NAME, Value: m.value, Unit: 'Count' }],
        }),
      ),
    dlqUrl: getDlqUrl(),
  });

  const stats = await getCleanupStats();

  logger.info('Outbox cleanup completed', {
    deleted,
    failedCount: failedEvents.length,
    stats,
  });

  return {
    deleted,
    failedCount: failedEvents.length,
    stats,
  };
};

export const handleScheduledEvent = async (
  event: EventBridgeEvent<string, any>,
): Promise<{ statusCode: number; body: string }> => {
  logger.info('Outbox cleanup triggered by EventBridge', {
    source: event.source,
    detailType: event['detail-type'],
    time: event.time,
  });

  try {
    const result = await performCleanup();

    return {
      statusCode: 200,
      body: JSON.stringify({
        message: 'Outbox cleanup completed',
        result,
      }),
    };
  } catch (error) {
    logger.error('Outbox cleanup handler error', {
      error: error instanceof Error ? error.message : String(error),
    });

    return {
      statusCode: 500,
      body: JSON.stringify({
        message: 'Outbox cleanup failed',
        error: error instanceof Error ? error.message : String(error),
      }),
    };
  }
};
