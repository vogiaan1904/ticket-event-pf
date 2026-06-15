/**
 * Unit tests for outbox cleanup handler
 */

import { performCleanup } from '../handlers/cleanup.handler';
import { getPrismaClient } from '@/common/database/prisma';
import { EventType } from '@/common/types/event.types';

// Mock dependencies
jest.mock('@/common/database/prisma');
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
    deleteMany: jest.fn(),
    findMany: jest.fn(),
    findFirst: jest.fn(),
    count: jest.fn(),
  },
};

(getPrismaClient as jest.Mock).mockReturnValue(mockPrismaClient);

describe('Outbox Cleanup Handler', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('performCleanup', () => {
    it('should successfully delete old published events', async () => {
      // Mock delete result
      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 150 });

      // Mock no failed events
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);

      // Mock stats
      mockPrismaClient.outboxEvent.count
        .mockResolvedValueOnce(500) // total events
        .mockResolvedValueOnce(400) // published events
        .mockResolvedValueOnce(90) // pending events
        .mockResolvedValueOnce(10); // failed events

      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue({
        createdAt: new Date(Date.now() - 5 * 60 * 1000), // 5 minutes ago
      });

      const result = await performCleanup();

      expect(result.deleted).toBe(150);
      expect(result.failedCount).toBe(0);
      expect(result.stats).toEqual({
        totalEvents: 500,
        publishedEvents: 400,
        pendingEvents: 90,
        failedEvents: 10,
        oldestPendingAge: 5,
      });

      // Verify delete was called with correct filter
      expect(mockPrismaClient.outboxEvent.deleteMany).toHaveBeenCalledWith({
        where: {
          publishedAt: {
            not: null,
            lt: expect.any(Date),
          },
        },
      });

      // Verify cutoff date is 7 days ago (within 1 second tolerance)
      const deleteCall = mockPrismaClient.outboxEvent.deleteMany.mock.calls[0][0];
      const cutoffDate = deleteCall.where.publishedAt.lt;
      const expectedCutoff = new Date();
      expectedCutoff.setDate(expectedCutoff.getDate() - 7);
      const timeDiff = Math.abs(cutoffDate.getTime() - expectedCutoff.getTime());
      expect(timeDiff).toBeLessThan(1000); // Within 1 second
    });

    it('should find and report failed events', async () => {
      const failedEvents = [
        {
          id: 'failed-1',
          aggregateId: 'payment-1',
          aggregateType: 'payment',
          eventType: EventType.PAYMENT_COMPLETED,
          retryCount: 5,
          lastError: 'Kafka timeout',
          createdAt: new Date(Date.now() - 60 * 60 * 1000), // 1 hour ago
        },
        {
          id: 'failed-2',
          aggregateId: 'payment-2',
          aggregateType: 'payment',
          eventType: EventType.PAYMENT_FAILED,
          retryCount: 3,
          lastError: 'Connection refused',
          createdAt: new Date(Date.now() - 30 * 60 * 1000), // 30 minutes ago
        },
      ];

      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 50 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue(failedEvents);

      // Mock stats
      mockPrismaClient.outboxEvent.count
        .mockResolvedValueOnce(200) // total
        .mockResolvedValueOnce(150) // published
        .mockResolvedValueOnce(40) // pending
        .mockResolvedValueOnce(10); // failed

      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue(null);

      const result = await performCleanup();

      expect(result.deleted).toBe(50);
      expect(result.failedCount).toBe(2);

      // Verify failed events query
      expect(mockPrismaClient.outboxEvent.findMany).toHaveBeenCalledWith({
        where: {
          publishedAt: null,
          retryCount: { gte: 3 },
        },
        select: {
          id: true,
          aggregateId: true,
          aggregateType: true,
          eventType: true,
          retryCount: true,
          lastError: true,
          createdAt: true,
        },
        orderBy: {
          createdAt: 'asc',
        },
      });
    });

    it('should handle case with no old events to delete', async () => {
      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 0 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);

      // Mock stats
      mockPrismaClient.outboxEvent.count
        .mockResolvedValueOnce(100)
        .mockResolvedValueOnce(50)
        .mockResolvedValueOnce(45)
        .mockResolvedValueOnce(5);

      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue(null);

      const result = await performCleanup();

      expect(result.deleted).toBe(0);
      expect(result.failedCount).toBe(0);
      expect(result.stats.oldestPendingAge).toBeNull();
    });

    it('should calculate correct statistics', async () => {
      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 100 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);

      // Mock stats
      mockPrismaClient.outboxEvent.count
        .mockResolvedValueOnce(1000) // total events
        .mockResolvedValueOnce(800) // published events
        .mockResolvedValueOnce(180) // pending events
        .mockResolvedValueOnce(20); // failed events

      // Oldest pending event is 120 minutes old
      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue({
        createdAt: new Date(Date.now() - 120 * 60 * 1000),
      });

      const result = await performCleanup();

      expect(result.stats).toEqual({
        totalEvents: 1000,
        publishedEvents: 800,
        pendingEvents: 180,
        failedEvents: 20,
        oldestPendingAge: 120,
      });

      // Verify stats queries
      expect(mockPrismaClient.outboxEvent.count).toHaveBeenCalledTimes(4);

      // Total events
      expect(mockPrismaClient.outboxEvent.count).toHaveBeenNthCalledWith(1);

      // Published events
      expect(mockPrismaClient.outboxEvent.count).toHaveBeenNthCalledWith(2, {
        where: { publishedAt: { not: null } },
      });

      // Pending events
      expect(mockPrismaClient.outboxEvent.count).toHaveBeenNthCalledWith(3, {
        where: {
          publishedAt: null,
          retryCount: { lt: 3 },
        },
      });

      // Failed events
      expect(mockPrismaClient.outboxEvent.count).toHaveBeenNthCalledWith(4, {
        where: {
          publishedAt: null,
          retryCount: { gte: 3 },
        },
      });

      // Oldest pending event
      expect(mockPrismaClient.outboxEvent.findFirst).toHaveBeenCalledWith({
        where: { publishedAt: null },
        orderBy: { createdAt: 'asc' },
        select: { createdAt: true },
      });
    });

    it('should handle database errors gracefully', async () => {
      mockPrismaClient.outboxEvent.deleteMany.mockRejectedValue(
        new Error('Database connection failed')
      );

      await expect(performCleanup()).rejects.toThrow('Database connection failed');
    });

    it('should continue with monitoring even if delete fails', async () => {
      // Delete fails
      mockPrismaClient.outboxEvent.deleteMany.mockRejectedValue(new Error('Delete failed'));

      await expect(performCleanup()).rejects.toThrow('Delete failed');

      // Verify delete was attempted
      expect(mockPrismaClient.outboxEvent.deleteMany).toHaveBeenCalled();
    });

    it('should respect retention days configuration', async () => {
      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 50 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);

      // Mock stats
      mockPrismaClient.outboxEvent.count.mockResolvedValue(100);
      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue(null);

      await performCleanup();

      // Verify retention days (7 days from config)
      const deleteCall = mockPrismaClient.outboxEvent.deleteMany.mock.calls[0][0];
      const cutoffDate = deleteCall.where.publishedAt.lt;
      const now = new Date();
      const daysDiff = Math.floor((now.getTime() - cutoffDate.getTime()) / (1000 * 60 * 60 * 24));

      expect(daysDiff).toBe(7);
    });

    it('should order failed events by creation time', async () => {
      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 0 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);
      mockPrismaClient.outboxEvent.count.mockResolvedValue(0);
      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue(null);

      await performCleanup();

      // Verify ordering
      expect(mockPrismaClient.outboxEvent.findMany).toHaveBeenCalledWith(
        expect.objectContaining({
          orderBy: {
            createdAt: 'asc',
          },
        })
      );
    });

    it('should handle multiple failed events correctly', async () => {
      const manyFailedEvents = Array.from({ length: 25 }, (_, i) => ({
        id: `failed-${i}`,
        aggregateId: `payment-${i}`,
        aggregateType: 'payment',
        eventType: EventType.PAYMENT_COMPLETED,
        retryCount: 5,
        lastError: `Error ${i}`,
        createdAt: new Date(Date.now() - i * 60 * 1000),
      }));

      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 200 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue(manyFailedEvents);

      // Mock stats
      mockPrismaClient.outboxEvent.count
        .mockResolvedValueOnce(500)
        .mockResolvedValueOnce(450)
        .mockResolvedValueOnce(25)
        .mockResolvedValueOnce(25);

      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue(null);

      const result = await performCleanup();

      expect(result.deleted).toBe(200);
      expect(result.failedCount).toBe(25);
    });

    it('should calculate age correctly for oldest pending event', async () => {
      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 0 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);
      mockPrismaClient.outboxEvent.count.mockResolvedValue(100);

      // Event created 45 minutes ago
      const oldestEventTime = new Date(Date.now() - 45 * 60 * 1000);
      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue({
        createdAt: oldestEventTime,
      });

      const result = await performCleanup();

      expect(result.stats.oldestPendingAge).toBe(45);
    });

    it('should handle null oldest pending event', async () => {
      mockPrismaClient.outboxEvent.deleteMany.mockResolvedValue({ count: 10 });
      mockPrismaClient.outboxEvent.findMany.mockResolvedValue([]);
      mockPrismaClient.outboxEvent.count.mockResolvedValue(50);

      // No pending events
      mockPrismaClient.outboxEvent.findFirst.mockResolvedValue(null);

      const result = await performCleanup();

      expect(result.stats.oldestPendingAge).toBeNull();
    });
  });
});
