import { ColumnType, Generated, JSONColumnType } from 'kysely';

export type PaymentStatus = 'PENDING' | 'COMPLETED' | 'CANCELLED' | 'FAILED';

export interface OutboxTable {
  id: Generated<string>;
  aggregateId: string;
  aggregateType: string;
  eventType: string;
  payload: JSONColumnType<Record<string, unknown>>;
  publishedAt: ColumnType<Date | null, Date | null, Date | null>;
  createdAt: Generated<Date>;
  retryCount: Generated<number>;
  lastError: string | null;
}

export interface PaymentsTable {
  id: Generated<string>;
  orderCode: string;
  amountCents: number;
  currency: string;
  provider: string;
  providerTransactionId: string;
  idempotencyKey: string;
  redirectUrl: string;
  paymentUrl: string;
  status: PaymentStatus;
  metadata: JSONColumnType<Record<string, unknown>> | null;
  createdAt: Generated<Date>;
  updatedAt: ColumnType<Date, Date | undefined, Date>;
  completedAt: ColumnType<Date | null, Date | null, Date | null>;
  failedAt: ColumnType<Date | null, Date | null, Date | null>;
  cancelledAt: ColumnType<Date | null, Date | null, Date | null>;
}

export interface Database {
  outbox: OutboxTable;
  payments: PaymentsTable;
}
