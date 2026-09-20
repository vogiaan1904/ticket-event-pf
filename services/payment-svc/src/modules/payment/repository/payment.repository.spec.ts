import { Test } from '@nestjs/testing';
import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { PaymentProvider } from '../enums/provider.enum';
import { PaymentRepository } from './payment.repository';

// One row, shaped like Prisma's Payment, reused by every case here.
const row = {
  id: 'pay-1',
  orderCode: 'ORD-1',
  amountCents: 1000,
  currency: 'VND',
  provider: 'zalopay',
  providerTransactionId: 'tx-1',
  idempotencyKey: 'idem-1',
  redirectUrl: 'https://shop/return',
  paymentUrl: 'https://pay/1',
  status: 'PENDING',
  metadata: null,
  completedAt: null,
  failedAt: null,
  cancelledAt: null,
  createdAt: new Date(),
  updatedAt: new Date(),
};

async function buildRepo(prisma: any) {
  const moduleRef = await Test.createTestingModule({
    providers: [PaymentRepository, { provide: PrismaService, useValue: prisma }],
  }).compile();
  return moduleRef.get(PaymentRepository);
}

describe('PaymentRepository', () => {
  it('keeps the payment url a lookup is asked for', async () => {
    const repo = await buildRepo({ payment: { findUnique: jest.fn().mockResolvedValue(row) } });

    const found = await repo.findByIdempotencyKey('idem-1');

    // createPaymentIntent returns this field directly on an idempotency hit.
    expect(found?.paymentUrl).toBe('https://pay/1');
    expect(found?.redirectUrl).toBe('https://shop/return');
  });

  it('returns the created payment, not an empty object', async () => {
    const repo = await buildRepo({ payment: { create: jest.fn().mockResolvedValue(row) } });

    const created = await repo.create({
      idempotencyKey: 'idem-1',
      orderCode: 'ORD-1',
      amountCents: 1000,
      currency: 'VND',
      provider: PaymentProvider.ZALOPAY,
      redirectUrl: 'https://shop/return',
      timeoutSeconds: 60,
      transactionId: 'tx-1',
      paymentUrl: 'https://pay/1',
    });

    expect(created.orderCode).toBe('ORD-1');
  });
});
