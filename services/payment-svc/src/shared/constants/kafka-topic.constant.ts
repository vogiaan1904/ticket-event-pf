export const KAFKA_TOPICS = {
  PAYMENT_COMPLETED: 'payment.completed',
  PAYMENT_FAILED: 'payment.failed',
  PAYMENT_CANCELLED: 'payment.cancelled',

  NOTIFICATION_SEND: 'notification.send',

  AUDIT_LOG: 'audit.log',
} as const;

export type KafkaTopicType = (typeof KAFKA_TOPICS)[keyof typeof KAFKA_TOPICS];
