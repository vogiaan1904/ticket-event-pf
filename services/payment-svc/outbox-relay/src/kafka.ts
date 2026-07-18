export { getKafkaProducer, disconnectKafka, publishWithRetry } from '../../lambdas/common/kafka/producer';

import { publishWithRetry } from '../../lambdas/common/kafka/producer';
import { KAFKA_TOPICS } from '../../lambdas/common/constants/kafka-topics';
import type { OutboxRow } from '../../lambdas/common/db/outbox.repo';

export const topicFor = (eventType: string): string => {
  switch (eventType) {
    case 'PaymentCompleted':
      return KAFKA_TOPICS.PAYMENT_COMPLETED;
    case 'PaymentFailed':
      return KAFKA_TOPICS.PAYMENT_FAILED;
    case 'PaymentCancelled':
      return KAFKA_TOPICS.PAYMENT_CANCELLED;
    default:
      return KAFKA_TOPICS.PAYMENT_FAILED;
  }
};

export const publishRow = async (topic: string, row: OutboxRow): Promise<void> => {
  await publishWithRetry(topic, row.payload, row.aggregateId, {
    eventType: row.eventType,
    aggregateType: row.aggregateType,
    eventId: row.id,
  });
};
