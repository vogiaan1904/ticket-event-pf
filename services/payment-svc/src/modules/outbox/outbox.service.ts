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
}
