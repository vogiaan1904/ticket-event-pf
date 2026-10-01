export enum OrderStatus {
  UNSPECIFIED = 'UNSPECIFIED',
  PENDING = 'PENDING',
  COMPLETED = 'COMPLETED',
  CANCELED = 'CANCELED',
  FAILED = 'FAILED',
  /** The hold expired before anyone paid. */
  EXPIRED = 'EXPIRED',
  /** Paid, but the tickets could not be issued: the buyer is owed the money. */
  REFUND_REQUIRED = 'REFUND_REQUIRED',
  REFUNDED = 'REFUNDED',
}
