import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { Currency, PrismaClient } from '@prisma/client';
import { execSync } from 'child_process';
import { randomUUID } from 'crypto';
import { PaymentProvider } from '../enums/provider.enum';
import { PaymentRepository } from './payment.repository';

// Against a real Postgres: PAYMENT_TEST_DATABASE_URL, migrated here. Without it the
// suite skips locally and fails in CI, where a suite that skips itself asserts nothing.
const url = process.env.PAYMENT_TEST_DATABASE_URL;
if (!url && process.env.CI) {
  throw new Error(
    'PAYMENT_TEST_DATABASE_URL is not set: the repository suite would assert nothing',
  );
}
const suite = url ? describe : describe.skip;

suite('PaymentRepository against Postgres', () => {
  let prisma: PrismaClient;
  let repo: PaymentRepository;

  beforeAll(async () => {
    execSync('npx prisma migrate deploy', {
      env: { ...process.env, DATABASE_URL: url },
      stdio: 'ignore',
    });
    prisma = new PrismaClient({ datasources: { db: { url } } });
    repo = new PaymentRepository(prisma as PrismaService);
  }, 60_000);

  afterAll(async () => prisma?.$disconnect());

  it('reads back every field a payment intent is created with', async () => {
    const orderCode = `TB-RB-${randomUUID()}`;
    await repo.create({
      amountCents: 24690,
      currency: Currency.VND,
      orderCode,
      idempotencyKey: `key-${orderCode}`,
      provider: PaymentProvider.ZALOPAY,
      transactionId: `trans-${orderCode}`,
      redirectUrl: 'https://example.com/done',
      timeoutSeconds: 360,
      paymentUrl: 'https://pay.example.com/x',
    });

    const read = await repo.findByOrderCode(orderCode);
    expect(read).toMatchObject({
      orderCode,
      amountCents: 24690,
      currency: Currency.VND,
      idempotencyKey: `key-${orderCode}`,
      provider: PaymentProvider.ZALOPAY,
      providerTransactionId: `trans-${orderCode}`,
      redirectUrl: 'https://example.com/done',
      paymentUrl: 'https://pay.example.com/x',
      status: 'PENDING',
    });
    expect(await repo.findByIdempotencyKey(`key-${orderCode}`)).toMatchObject({ orderCode });
    expect(await repo.findByProviderTransactionId(`trans-${orderCode}`)).toMatchObject({
      orderCode,
    });
  });
});
