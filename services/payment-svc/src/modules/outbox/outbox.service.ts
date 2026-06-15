import { PrismaService } from '@infra/database/prisma/prisma.service';
import { Injectable, Logger } from '@nestjs/common';
import { Outbox, Prisma } from '@prisma/client';
import { PaymentEntity } from '../payment/entities/payment.entity';
import { EventType } from './enums/event-type.enum';
import { PaymentCompletedEvent, PaymentFailedEvent } from './events/payment.event';

export interface OutboxEventPayload {
  [key: string]: any;
}

@Injectable()
export class OutboxService {
  private readonly logger = new Logger(OutboxService.name);

  constructor(private readonly prisma: PrismaService) {}

  /**
   * Save an event to the outbox table
   * Must be called within a transaction context
   */
  async saveEvent(
    aggregateId: string,
    aggregateType: string,
    eventType: string,
    payload: OutboxEventPayload,
    tx?: Prisma.TransactionClient,
  ): Promise<Outbox> {
    const prismaClient = tx || this.prisma;

    try {
      const outboxEntry = await prismaClient.outbox.create({
        data: {
          aggregateId,
          aggregateType,
          eventType,
          payload: payload as Prisma.JsonObject,
          published: false,
          retryCount: 0,
        },
      });

      this.logger.debug(`Outbox event saved: ${eventType} for ${aggregateType}:${aggregateId}`);

      return outboxEntry;
    } catch (error) {
      this.logger.error('Failed to save outbox event', error);
      throw error;
    }
  }

  /**
   * Payment-specific: Save payment completed event
   */
  async savePaymentCompletedEvent(
    payment: PaymentEntity,
    tx?: Prisma.TransactionClient,
  ): Promise<Outbox> {
    const event: PaymentCompletedEvent = {
      payment_id: payment.id,
      order_code: payment.orderCode,
      amount_cents: payment.amountCents,
      currency: payment.currency,
      provider: payment.provider,
      transaction_id: payment.providerTransactionId,
      completed_at: (payment.completedAt || new Date()).toISOString(),
    };

    return this.saveEvent(payment.id, 'Payment', EventType.PAYMENT_COMPLETED, event, tx);
  }

  /**
   * Payment-specific: Save payment failed event
   */
  async savePaymentFailedEvent(
    payment: PaymentEntity,
    errorCode?: string,
    tx?: Prisma.TransactionClient,
  ): Promise<Outbox> {
    const event: PaymentFailedEvent = {
      payment_id: payment.id,
      order_code: payment.orderCode,
      amount_cents: payment.amountCents,
      currency: payment.currency,
      provider: payment.provider,
      transaction_id: payment.providerTransactionId,
      failed_at: (payment.failedAt || new Date()).toISOString(),
    };

    return this.saveEvent(payment.id, 'Payment', EventType.PAYMENT_FAILED, event, tx);
  }

  /**
   * Get unpublished events for processing
   * Supports pagination and batch processing
   */
  async getUnpublishedEvents(limit: number = 100, maxRetries: number = 5): Promise<Outbox[]> {
    return this.prisma.outbox.findMany({
      where: {
        published: false,
        retryCount: {
          lt: maxRetries,
        },
      },
      orderBy: {
        createdAt: 'asc',
      },
      take: limit,
    });
  }

  /**
   * Mark event as published
   */
  async markAsPublished(id: string): Promise<void> {
    await this.prisma.outbox.update({
      where: { id },
      data: {
        published: true,
        publishedAt: new Date(),
      },
    });

    this.logger.debug(`Outbox event marked as published: ${id}`);
  }

  /**
   * Increment retry count on failure
   */
  async incrementRetryCount(id: string, error: string): Promise<void> {
    await this.prisma.outbox.update({
      where: { id },
      data: {
        retryCount: {
          increment: 1,
        },
        lastError: error.substring(0, 500), // Limit error message length
      },
    });

    this.logger.warn(`Outbox event retry count incremented: ${id}`);
  }

  /**
   * Delete old published events (cleanup)
   * Should be called periodically
   */
  async deleteOldPublishedEvents(olderThanDays: number = 7): Promise<number> {
    const cutoffDate = new Date();
    cutoffDate.setDate(cutoffDate.getDate() - olderThanDays);

    const result = await this.prisma.outbox.deleteMany({
      where: {
        published: true,
        publishedAt: {
          lt: cutoffDate,
        },
      },
    });

    this.logger.log(
      `Deleted ${result.count} old published events older than ${olderThanDays} days`,
    );

    return result.count;
  }

  /**
   * Get failed events that exceeded max retries
   */
  async getFailedEvents(maxRetries: number = 5): Promise<Outbox[]> {
    return this.prisma.outbox.findMany({
      where: {
        published: false,
        retryCount: {
          gte: maxRetries,
        },
      },
      orderBy: {
        createdAt: 'desc',
      },
    });
  }

  /**
   * Reset failed event for manual retry
   */
  async resetFailedEvent(id: string): Promise<void> {
    await this.prisma.outbox.update({
      where: { id },
      data: {
        retryCount: 0,
        lastError: null,
      },
    });

    this.logger.log(`Outbox event reset for retry: ${id}`);
  }
}
