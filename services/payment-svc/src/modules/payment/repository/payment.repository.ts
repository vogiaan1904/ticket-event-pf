import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { Injectable } from '@nestjs/common';
import { toPaymentEntity } from './payment.mapper';
import { PaymentEntity } from '../entities/payment.entity';
import { CreatePaymentIntentDto } from '../dto';
import { PaymentStatus } from '@prisma/client';

@Injectable()
export class PaymentRepository {
  constructor(private readonly prisma: PrismaService) {}

  async create(dto: CreatePaymentIntentDto): Promise<PaymentEntity> {
    await this.prisma.payment.create({
      data: {
        amountCents: dto.amountCents,
        currency: dto.currency,
        idempotencyKey: dto.idempotencyKey,
        orderCode: dto.orderCode,
        redirectUrl: dto.redirectUrl,
        status: PaymentStatus.PENDING,
        provider: dto.provider,
        providerTransactionId: dto.transactionId,
        paymentUrl: dto.paymentUrl,
      },
    });
    return {} as PaymentEntity;
  }

  async findByIdempotencyKey(idempotencyKey: string): Promise<PaymentEntity | null> {
    const payment = await this.prisma.payment.findUnique({
      where: { idempotencyKey },
    });

    if (!payment) return null;

    return toPaymentEntity(payment);
  }

  async findByOrderCode(orderCode: string): Promise<PaymentEntity | null> {
    const payment = await this.prisma.payment.findUnique({
      where: { orderCode },
    });

    if (!payment) return null;

    return toPaymentEntity(payment);
  }

  async findByProviderTransactionId(providerTransactionId: string): Promise<PaymentEntity | null> {
    const payment = await this.prisma.payment.findUnique({
      where: { providerTransactionId },
    });

    if (!payment) return null;

    return toPaymentEntity(payment);
  }

  async updateStatus(id: string, status: PaymentStatus): Promise<PaymentEntity> {
    const payment = await this.prisma.payment.update({
      where: { id },
      data: { status },
    });

    return toPaymentEntity(payment);
  }
}
