/** Wire shape for payment.completed. snake_case to match the Go consumer. */
export interface PaymentCompletedEvent {
  order_code: string;
  payment_id: string;
  amount_cents: number;
  currency: string;
  provider: string;
  transaction_id: string;
  completed_at: string; // ISO 8601 string (e.g., "2025-10-16T12:34:56.789Z")
}

/** Wire shape for payment.failed. snake_case to match the Go consumer. */
export interface PaymentFailedEvent {
  order_code: string;
  payment_id: string;
  amount_cents: number;
  currency: string;
  provider: string;
  transaction_id: string;
  failed_at: string; // ISO 8601 string (e.g., "2025-10-16T12:34:56.789Z")
}
