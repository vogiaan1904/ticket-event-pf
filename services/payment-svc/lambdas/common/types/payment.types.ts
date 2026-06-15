export type { Payment as PaymentEntity } from '@prisma/client';
export { PaymentStatus, Currency } from '@prisma/client';

export enum PaymentProvider {
  ZALOPAY = 'ZALOPAY',
  PAYOS = 'PAYOS',
  VNPAY = 'VNPAY',
}
