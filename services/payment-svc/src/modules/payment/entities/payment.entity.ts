import { Currency, Payment, PaymentStatus } from '@prisma/client';

export class PaymentEntity implements Payment {
  id: string;
  orderCode: string;
  amountCents: number;
  currency: Currency;
  provider: string;
  providerTransactionId: string; // ZaloPay's app_trans_id, Stripe's payment_intent_id
  idempotencyKey: string;
  status: PaymentStatus;
  redirectUrl: string;
  paymentUrl: string;
  metadata: any;
  createdAt: Date;
  updatedAt: Date;
  completedAt: Date | null;
  failedAt: Date | null;
  cancelledAt: Date | null;
}
