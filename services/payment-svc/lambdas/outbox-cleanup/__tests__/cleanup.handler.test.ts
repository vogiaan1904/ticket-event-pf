import { randomUUID } from 'crypto';
import { getDb, closeDb } from '@/common/db/kysely';

const mockSqsSend = jest.fn().mockResolvedValue({});
const mockCwSend = jest.fn().mockResolvedValue({});

// A live outbox-relay may share this Postgres and claim any row with retryCount
// below its real OUTBOX_MAX_RETRIES (5). Seeding at or above that keeps rows out
// of its reach; pending vs exhausted is then decided by this test's mocked 10.
const RELAY_IMMUNE_RETRY_COUNT = 5;
const EXHAUSTED_RETRY_COUNT = 10;

jest.mock('@aws-sdk/client-sqs', () => ({
  SQSClient: jest.fn().mockImplementation(() => ({ send: mockSqsSend })),
  SendMessageCommand: jest.fn().mockImplementation((input) => input),
}));
jest.mock('@aws-sdk/client-cloudwatch', () => ({
  CloudWatchClient: jest.fn().mockImplementation(() => ({ send: mockCwSend })),
  PutMetricDataCommand: jest.fn().mockImplementation((input) => input),
}));
jest.mock('@/common/logger');
jest.mock('@/common/config', () => ({
  getConfig: jest.fn(() => ({
    outbox: {
      batchSize: 10,
      maxRetries: 10,
      retentionDays: 7,
    },
  })),
}));

process.env.OUTBOX_DLQ_URL = 'https://sqs.local/outbox-dlq';

// Imported after the mocks above so the module picks up the mocked AWS clients/config.
import { performCleanup } from '../handlers/cleanup.handler';

type SeedRow = {
  id?: string;
  aggregateId?: string;
  aggregateType?: string;
  eventType?: string;
  retryCount?: number;
  lastError?: string | null;
  publishedAt?: Date | null;
  createdAt?: Date;
};

const seedOutboxRow = ({
  id = randomUUID(),
  aggregateId = 'payment-1',
  aggregateType = 'payment',
  eventType = 'PaymentCompleted',
  retryCount = RELAY_IMMUNE_RETRY_COUNT,
  lastError = null,
  publishedAt = null,
  createdAt = new Date(),
}: SeedRow = {}) =>
  getDb()
    .insertInto('outbox')
    .values({
      id,
      aggregateId,
      aggregateType,
      eventType,
      payload: JSON.stringify({}),
      retryCount,
      lastError,
      publishedAt,
      createdAt,
    })
    .execute();

afterAll(async () => {
  await closeDb();
});

beforeEach(async () => {
  jest.clearAllMocks();
  await getDb().deleteFrom('outbox').execute();
});

describe('performCleanup', () => {
  it('deletes published events older than the retention window', async () => {
    const oldCutoff = new Date();
    oldCutoff.setDate(oldCutoff.getDate() - 10);

    await seedOutboxRow({ id: 'old-published', publishedAt: oldCutoff });
    await seedOutboxRow({ id: 'recent-published', publishedAt: new Date() });
    await seedOutboxRow({ id: 'unpublished', publishedAt: null });

    const result = await performCleanup();

    expect(result.deleted).toBe(1);
    const remainingIds = await getDb().selectFrom('outbox').select('id').execute();
    expect(remainingIds.map((r) => r.id).sort()).toEqual(['recent-published', 'unpublished']);
  });

  it('routes exhausted-retry events to the DLQ and reports the count', async () => {
    await seedOutboxRow({ id: 'exhausted-1', retryCount: EXHAUSTED_RETRY_COUNT, lastError: 'boom' });
    await seedOutboxRow({ id: 'exhausted-2', retryCount: EXHAUSTED_RETRY_COUNT + 2, lastError: 'still boom' });
    await seedOutboxRow({ id: 'still-retrying', retryCount: RELAY_IMMUNE_RETRY_COUNT });

    const result = await performCleanup();

    expect(result.failedCount).toBe(2);
    expect(mockSqsSend).toHaveBeenCalledTimes(2);
    expect(mockCwSend).toHaveBeenCalledWith(
      expect.objectContaining({ MetricData: [expect.objectContaining({ MetricName: 'OutboxFailedEvents', Value: 2 })] }),
    );
  });

  it('emits a zero-value metric and sends nothing when no events are exhausted', async () => {
    await seedOutboxRow({ id: 'still-retrying', retryCount: RELAY_IMMUNE_RETRY_COUNT });

    const result = await performCleanup();

    expect(result.failedCount).toBe(0);
    expect(mockSqsSend).not.toHaveBeenCalled();
    expect(mockCwSend).toHaveBeenCalledWith(
      expect.objectContaining({ MetricData: [expect.objectContaining({ Value: 0 })] }),
    );
  });

  it('reports cleanup stats across published, pending and failed events', async () => {
    const published = new Date();
    await seedOutboxRow({ id: 'published-recent', publishedAt: published });
    await seedOutboxRow({
      id: 'pending-1',
      retryCount: RELAY_IMMUNE_RETRY_COUNT,
      createdAt: new Date(Date.now() - 5 * 60 * 1000),
    });
    await seedOutboxRow({ id: 'failed-1', retryCount: EXHAUSTED_RETRY_COUNT });

    const result = await performCleanup();

    expect(result.stats.totalEvents).toBe(3);
    expect(result.stats.publishedEvents).toBe(1);
    expect(result.stats.pendingEvents).toBe(1);
    expect(result.stats.failedEvents).toBe(1);
    expect(result.stats.oldestPendingAge).toBeGreaterThanOrEqual(4);
  });

  it('reports a null oldestPendingAge when there are no unpublished events', async () => {
    await seedOutboxRow({ id: 'published-only', publishedAt: new Date() });

    const result = await performCleanup();

    expect(result.stats.oldestPendingAge).toBeNull();
  });
});
