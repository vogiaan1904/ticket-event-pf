import { Injectable } from '@nestjs/common';
import { CreatePaymentIntentDto } from './dto';
import { PaymentProvider } from './enums/provider.enum';
import { PaymentGatewayFactory } from './gateways/gateway.factory';
import { PaymentRepository } from './repository/payment.repository';
import { PaymentEntity } from './entities/payment.entity';
import { RpcBusinessException } from '@/common/exceptions/rpc-business.exception';
import { ErrorCodeEnum } from '@/shared/constants/error-code.constant';
import { LoggerService } from '@/shared/services/logger.service';
import { PaymentStatus } from '@prisma/client';
import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { OutboxService } from '@/modules/outbox/outbox.service';

@Injectable()
export class PaymentService {
  constructor(
    private readonly paymentGatewayFactory: PaymentGatewayFactory,
    private readonly repo: PaymentRepository,
    private readonly outboxService: OutboxService,
    private readonly prisma: PrismaService,
    private readonly logger: LoggerService,
  ) {
    logger.setContext(PaymentService.name);
  }

  private async handleSuccessPayment(providerTransactionId: string): Promise<void> {
    const now = new Date();

    // Find payment by providerTransactionId first
    const existingPayment = await this.repo.findByProviderTransactionId(providerTransactionId);
    if (!existingPayment) {
      this.logger.error(`Payment not found for providerTransactionId: ${providerTransactionId}`);
      throw new Error(`Payment not found for providerTransactionId: ${providerTransactionId}`);
    }

    await this.prisma.$transaction(async (tx) => {
      const payment = await tx.payment.update({
        where: { orderCode: existingPayment.orderCode },
        data: { status: PaymentStatus.COMPLETED, completedAt: now },
      });

      await this.outboxService.savePaymentCompletedEvent(payment, tx);
    });

    this.logger.info(
      `Payment completed: orderCode=${existingPayment.orderCode}, providerTransactionId=${providerTransactionId}`,
    );
  }

  private async handleFailedPayment(
    providerTransactionId: string,
    reason?: string,
    errorCode?: string,
  ): Promise<void> {
    try {
      const now = new Date();

      const existingPayment = await this.repo.findByProviderTransactionId(providerTransactionId);
      if (!existingPayment) {
        this.logger.error(`Payment not found for providerTransactionId: ${providerTransactionId}`);
        throw new Error(`Payment not found for providerTransactionId: ${providerTransactionId}`);
      }

      await this.prisma.$transaction(async (tx) => {
        const payment = await tx.payment.update({
          where: { orderCode: existingPayment.orderCode },
          data: {
            status: PaymentStatus.FAILED,
            failedAt: now,
          },
        });

        await this.outboxService.savePaymentFailedEvent(payment, errorCode, tx);
      });

      this.logger.log(
        `Payment failed: orderCode=${existingPayment.orderCode}, providerTransactionId=${providerTransactionId}`,
      );
    } catch (error) {
      this.logger.error(
        `Failed to handle failed payment for providerTransactionId: ${providerTransactionId}`,
        error,
      );
      throw error;
    }
  }

  async cancelPayment(orderCode: string): Promise<void> {
    try {
      const now = new Date();
      await this.prisma.$transaction(async (tx) => {
        const payment = await tx.payment.update({
          where: { orderCode },
          data: {
            status: PaymentStatus.CANCELLED,
            cancelledAt: now,
          },
        });

        await this.outboxService.saveEvent(
          payment.id,
          'Payment',
          'PaymentCancelled',
          {
            paymentId: payment.id,
            orderCode: payment.orderCode,
            cancelledAt: now.toISOString(),
          },
          tx,
        );

        return payment;
      });

      this.logger.log(`Payment cancelled for order: ${orderCode}`);
    } catch (error) {
      this.logger.error(`Failed to cancel payment for order: ${orderCode}`, error);
      throw error;
    }
  }

  async createPaymentIntent(dto: CreatePaymentIntentDto): Promise<string> {
    const existing = await this.repo.findByIdempotencyKey(dto.idempotencyKey);
    if (existing) {
      this.logger.log(`Returning cached payment for idempotency key: ${dto.idempotencyKey}`);
      return existing.paymentUrl;
    }

    const gateway = this.paymentGatewayFactory.getGateway(dto.provider);

    const { url, transactionId } = await gateway.createPaymentLink({
      amount: dto.amountCents,
      orderCode: dto.orderCode,
      currency: dto.currency,
      idempotencyKey: dto.idempotencyKey,
      redirectUrl: dto.redirectUrl,
      timeoutSeconds: dto.timeoutSeconds,
    });
    dto.transactionId = transactionId;
    dto.paymentUrl = url;

    await this.repo.create(dto);

    return url;
  }

  async findByIdempotencyKey(idempotencyKey: string): Promise<PaymentEntity> {
    const payment = await this.repo.findByIdempotencyKey(idempotencyKey);
    if (!payment) throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);

    return payment;
  }

  async handleCallback(provider: PaymentProvider, callbackBody: any) {
    const gateway = this.paymentGatewayFactory.getGateway(provider);

    const output = await gateway.handleCallback(callbackBody);

    if (!output.providerTransactionId) {
      this.logger.error('Callback handling failed - missing providerTransactionId');
    } else if (output.success) {
      await this.handleSuccessPayment(output.providerTransactionId);
    } else {
      await this.handleFailedPayment(output.providerTransactionId, 'Callback indicated failure');
    }

    return output.response;
  }
}
