import { status as grpcStatus } from '@grpc/grpc-js';
import { Test } from '@nestjs/testing';
import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { OutboxService } from '@/modules/outbox/outbox.service';
import { LoggerService } from '@/shared/services/logger.service';
import { PaymentGatewayFactory } from './gateways/gateway.factory';
import { PaymentService } from './payment.service';
import { PaymentRepository } from './repository/payment.repository';

// Every collaborator is mocked: these tests are about what PaymentService
// decides, not what Prisma does.
async function buildService() {
  const repo = {
    findByIdempotencyKey: jest.fn(),
    findByProviderTransactionId: jest.fn(),
    create: jest.fn(),
  };
  const outbox = {
    savePaymentCompletedEvent: jest.fn(),
    savePaymentFailedEvent: jest.fn(),
    saveEvent: jest.fn(),
  };
  const tx = {
    payment: { update: jest.fn(), updateMany: jest.fn(), findUnique: jest.fn() },
  };
  const prisma = { $transaction: jest.fn(async (fn: any) => fn(tx)) };
  const gateway = { createPaymentLink: jest.fn(), handleCallback: jest.fn() };
  const factory = { getGateway: jest.fn().mockReturnValue(gateway) };
  const logger = {
    setContext: jest.fn(),
    log: jest.fn(),
    info: jest.fn(),
    error: jest.fn(),
    warn: jest.fn(),
    debug: jest.fn(),
  };

  const moduleRef = await Test.createTestingModule({
    providers: [
      PaymentService,
      { provide: PaymentRepository, useValue: repo },
      { provide: OutboxService, useValue: outbox },
      { provide: PrismaService, useValue: prisma },
      { provide: PaymentGatewayFactory, useValue: factory },
      { provide: LoggerService, useValue: logger },
    ],
  }).compile();

  return { service: moduleRef.get(PaymentService), repo, outbox, prisma, tx, gateway };
}

describe('PaymentService', () => {
  describe('findByIdempotencyKey', () => {
    it('reports a missing payment as NOT_FOUND', async () => {
      const { service, repo } = await buildService();
      repo.findByIdempotencyKey.mockResolvedValue(null);

      const err: any = await service.findByIdempotencyKey('no-such-key').catch((e) => e);

      // PERMISSION_DENIED tells a caller to stop; NOT_FOUND tells it to refetch.
      expect(err.getError()).toMatchObject({ code: grpcStatus.NOT_FOUND });
    });
  });
});
