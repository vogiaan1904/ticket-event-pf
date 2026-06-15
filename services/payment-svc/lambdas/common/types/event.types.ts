export enum EventType {
  PAYMENT_COMPLETED = 'PAYMENT_COMPLETED',
  PAYMENT_FAILED = 'PAYMENT_FAILED',
  PAYMENT_CANCELLED = 'PAYMENT_CANCELLED',
}

export interface PaymentCompletedEvent {
  order_code: string;
  payment_id: string;
  amount_cents: number;
  currency: string;
  provider: string;
  transaction_id: string;
  completed_at: string; // ISO 8601 string
}

export interface PaymentFailedEvent {
  order_code: string;
  payment_id: string;
  amount_cents: number;
  currency: string;
  provider: string;
  transaction_id: string;
  failed_at: string; // ISO 8601 string
}

export type EventPayload = PaymentCompletedEvent | PaymentFailedEvent;
