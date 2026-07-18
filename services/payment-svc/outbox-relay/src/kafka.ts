export { getKafkaProducer, disconnectKafka, publishWithRetry } from '../../lambdas/common/kafka/producer';

import { publishWithRetry } from '../../lambdas/common/kafka/producer';
import { KAFKA_TOPICS } from '../../lambdas/common/constants/kafka-topics';
import { EventType } from '../../lambdas/common/types/event.types';
import type { OutboxRow } from './db';

export const topicFor = (eventType: string): string => {
  switch (eventType) {
    case EventType.PAYMENT_COMPLETED:
      return KAFKA_TOPICS.PAYMENT_COMPLETED;
    case EventType.PAYMENT_FAILED:
      return KAFKA_TOPICS.PAYMENT_FAILED;
    case EventType.PAYMENT_CANCELLED:
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
