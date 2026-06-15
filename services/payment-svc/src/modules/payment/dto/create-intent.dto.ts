import { Currency } from '@prisma/client';
import { PaymentProvider } from '../enums/provider.enum';

export class CreatePaymentIntentDto {
  amountCents: number;
  currency: Currency;
  orderCode: string;
  idempotencyKey: string;
  provider: PaymentProvider;
  transactionId: string = ''; // provider's transaction id
  redirectUrl: string;
  timeoutSeconds: number;
  paymentUrl: string = '';
}
