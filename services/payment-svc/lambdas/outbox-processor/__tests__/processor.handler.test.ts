/**
 * Unit tests for outbox processor handler
 */

import { EventBridgeEvent } from 'aws-lambda';
import { processOutbox } from '../handlers/processor.handler';
import { getPrismaClient } from '@/common/database/prisma';
import { getKafkaProducer, publishWithRetry } from '@/common/kafka/producer';
import { EventType } from '@/common/types/event.types';
import { createMockEventBridgeEvent } from '../../__tests__/utils/mock-helpers';

// Mock dependencies
jest.mock('@/common/database/prisma');
jest.mock('@/common/kafka/producer');
jest.mock('@/common/logger');
jest.mock('@/common/config', () => ({
  getConfig: jest.fn(() => ({
    outbox: {
      batchSize: 10,
      maxRetries: 3,
      retentionDays: 7,
    },
  })),
}));

const mockPrismaClient = {
  outboxEvent: {
    findMany: jest.fn(),
    update: jest.fn(),
  },
};

const mockKafkaProducer = {
  connect: jest.fn(),
  disconnect: jest.fn(),
  send: jest.fn(),
};

(getPrismaClient as jest.Mock).mockReturnValue(mockPrismaClient);
(getKafkaProducer as jest.Mock).mockReturnValue(mockKafkaProducer);
(publishWithRetry as jest.Mock).mockImplementation(() =>
  Promise.resolve([
    {
      partition: 0,
      offset: '12345',
      topicName: 'payment.events',
    },
  ])
);

describe('Outbox Processor Handler', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('processOutbox', () => {
    const mockOutboxEvents = [
      {
        id: 'event-1',
        aggregateId: 'payment-1',
        aggregateType: 'payment',
        eventType: EventType.PAYMENT_COMPLETED,
        payload: {
          type: EventType.PAYMENT_COMPLETED,
          timestamp: '2025-01-01T00:00:00Z',
          data: {
            paymentId: 'payment-1',
            orderCode: 'ORDER123',
            amount: 100000,
          },
        },
        createdAt: new Date('2025-01-01T00:00:00Z'),
        publishedAt: null,
        retryCount: 0,
        lastError: null,
      },
      {
        id: 'event-2',
        aggregateId: 'payment-2',
        aggregateType: 'payment',
        eventType: EventType.PAYMENT_FAILED,
        payload: {
          type: EventType.PAYMENT_FAILED,
          timestamp: '2025-01-01T00:00:00Z',
          data: {
            paymentId: 'payment-2',
            orderCode: 'ORDER456',
            reason: 'Insufficient funds',
          },
        },
        createdAt: new Date('2025-01-01T00:00:00Z'),
        publishedAt: null,
        retryCount: 0,
        lastError: null,
      },
    ];

    it('should successfully process pending outbox events', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue(mockOutboxEvents);
      mockPrismaClient.outboxEvent.update.mockResolvedValue({});
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      const result = await processOutbox();

      expect(result.processed).toBe(2);
      expect(result.succeeded).toBe(2);
      expect(result.failed).toBe(0);

      // Verify Kafka producer was connected
      expect(mockKafkaProducer.connect).toHaveBeenCalled();

      // Verify events were fetched
      expect(mockPrismaClient.outboxEvent.findMany).toHaveBeenCalledWith({
        where: {
          publishedAt: null,
          retryCount: { lt: 3 },
        },
        orderBy: {
          createdAt: 'asc',
        },
        take: 10,
      });

      // Verify events were published to Kafka
      expect(publishWithRetry).toHaveBeenCalledTimes(2);
      expect(publishWithRetry).toHaveBeenCalledWith(
        'payment.events',
        mockOutboxEvents[0].payload,
        mockOutboxEvents[0].aggregateId,
        expect.objectContaining({
          eventType: EventType.PAYMENT_COMPLETED,
          aggregateType: 'payment',
          eventId: 'event-1',
        })
      );

      // Verify events were marked as published
      expect(mockPrismaClient.outboxEvent.update).toHaveBeenCalledTimes(2);
      expect(mockPrismaClient.outboxEvent.update).toHaveBeenCalledWith({
        where: { id: 'event-1' },
        data: {
          publishedAt: expect.any(Date),
          updatedAt: expect.any(Date),
        },
      });
    });

    it('should handle empty outbox', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      const result = await processOutbox();

      expect(result.processed).toBe(0);
      expect(result.succeeded).toBe(0);
      expect(result.failed).toBe(0);

      // Verify Kafka producer was connected
      expect(mockKafkaProducer.connect).toHaveBeenCalled();

      // Verify no events were published
      expect(publishWithRetry).not.toHaveBeenCalled();
      expect(mockPrismaClient.outboxEvent.update).not.toHaveBeenCalled();
    });

    it('should increment retry count on publishing failure', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([mockOutboxEvents[0]]);
      mockPrismaClient.outboxEvent.update.mockResolvedValue({});
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      // Mock Kafka publish failure
      (publishWithRetry as jest.Mock).mockRejectedValueOnce(new Error('Kafka connection failed'));

      const result = await processOutbox();

      expect(result.processed).toBe(1);
      expect(result.succeeded).toBe(0);
      expect(result.failed).toBe(1);

      // Verify retry count was incremented
      expect(mockPrismaClient.outboxEvent.update).toHaveBeenCalledWith({
        where: { id: 'event-1' },
        data: {
          retryCount: { increment: 1 },
          lastError: 'Failed to publish to Kafka',
          updatedAt: expect.any(Date),
        },
      });
    });

    it('should process multiple events and track success/failure', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue(mockOutboxEvents);
      mockPrismaClient.outboxEvent.update.mockResolvedValue({});
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      // Mock first event succeeds, second fails
      (publishWithRetry as jest.Mock)
        .mockResolvedValueOnce([{ partition: 0, offset: '12345', topicName: 'payment.events' }])
        .mockRejectedValueOnce(new Error('Kafka timeout'));

      const result = await processOutbox();

      expect(result.processed).toBe(2);
      expect(result.succeeded).toBe(1);
      expect(result.failed).toBe(1);

      // Verify first event was marked as published
      expect(mockPrismaClient.outboxEvent.update).toHaveBeenCalledWith({
        where: { id: 'event-1' },
        data: {
          publishedAt: expect.any(Date),
          updatedAt: expect.any(Date),
        },
      });

      // Verify second event had retry count incremented
      expect(mockPrismaClient.outboxEvent.update).toHaveBeenCalledWith({
        where: { id: 'event-2' },
        data: {
          retryCount: { increment: 1 },
          lastError: 'Failed to publish to Kafka',
          updatedAt: expect.any(Date),
        },
      });
    });

    it('should respect batch size configuration', async () => {
      const manyEvents = Array.from({ length: 20 }, (_, i) => ({
        ...mockOutboxEvents[0],
        id: `event-${i}`,
        aggregateId: `payment-${i}`,
      }));

      mockPrismaClient.outboxEvent.findMany.mockResolvedValue(manyEvents);
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      // Verify batch size is respected in query
      expect(mockPrismaClient.outboxEvent.findMany).toHaveBeenCalledWith(
        expect.objectContaining({
          take: 10, // Batch size from config
        })
      );
    });

    it('should filter events by max retries', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      await processOutbox();

      // Verify max retries filter is applied
      expect(mockPrismaClient.outboxEvent.findMany).toHaveBeenCalledWith(
        expect.objectContaining({
          where: expect.objectContaining({
            retryCount: { lt: 3 }, // Max retries from config
          }),
        })
      );
    });

    it('should handle database errors gracefully', async () => {
      mockKafkaProducer.connect.mockResolvedValue(undefined);
      mockPrismaClient.outboxEvent.findMany.mockRejectedValue(
        new Error('Database connection failed')
      );

      await expect(processOutbox()).rejects.toThrow('Database connection failed');
    });

    it('should handle Kafka connection errors', async () => {
      mockKafkaProducer.connect.mockRejectedValue(new Error('Kafka broker unavailable'));

      await expect(processOutbox()).rejects.toThrow('Kafka broker unavailable');
    });

    it('should continue processing even if update fails', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([mockOutboxEvents[0]]);
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      // Mock successful publish but failed update
      mockPrismaClient.outboxEvent.update.mockRejectedValue(
        new Error('Database constraint violation')
      );

      const result = await processOutbox();

      // Event should be counted as failed since update failed
      expect(result.processed).toBe(1);
      expect(result.failed).toBe(1);

      // Verify event was still published to Kafka
      expect(publishWithRetry).toHaveBeenCalled();
    });

    it('should order events by creation time', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      await processOutbox();

      // Verify ordering
      expect(mockPrismaClient.outboxEvent.findMany).toHaveBeenCalledWith(
        expect.objectContaining({
          orderBy: {
            createdAt: 'asc',
          },
        })
      );
    });

    it('should use aggregate ID as partition key', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([mockOutboxEvents[0]]);
      mockPrismaClient.outboxEvent.update.mockResolvedValue({});
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      await processOutbox();

      // Verify partition key is aggregate ID
      expect(publishWithRetry).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(Object),
        'payment-1', // Aggregate ID as partition key
        expect.any(Object)
      );
    });

    it('should include event metadata in Kafka headers', async () => {
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([mockOutboxEvents[0]]);
      mockPrismaClient.outboxEvent.update.mockResolvedValue({});
      mockKafkaProducer.connect.mockResolvedValue(undefined);

      await processOutbox();

      // Verify headers
      expect(publishWithRetry).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(Object),
        expect.any(String),
        expect.objectContaining({
          eventType: EventType.PAYMENT_COMPLETED,
          aggregateType: 'payment',
          eventId: 'event-1',
        })
      );
    });
  });
});
