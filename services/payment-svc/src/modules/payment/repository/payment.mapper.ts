import { Payment } from '@prisma/client';
import { PaymentEntity } from '../entities/payment.entity';

export function toPaymentEntity(prismaPayment: Payment): PaymentEntity {
  const entity = new PaymentEntity();
  entity.id = prismaPayment.id;
  entity.orderCode = prismaPayment.orderCode;
  entity.amountCents = prismaPayment.amountCents;
  entity.currency = prismaPayment.currency;
  entity.provider = prismaPayment.provider;
  entity.providerTransactionId = prismaPayment.providerTransactionId;
  entity.idempotencyKey = prismaPayment.idempotencyKey;
  entity.status = prismaPayment.status;
  entity.metadata = prismaPayment.metadata;
  entity.createdAt = prismaPayment.createdAt;
  entity.updatedAt = prismaPayment.updatedAt;
  entity.completedAt = prismaPayment.completedAt;
  entity.failedAt = prismaPayment.failedAt;

  return entity;
}
