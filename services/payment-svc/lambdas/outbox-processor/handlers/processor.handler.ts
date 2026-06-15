import { getConfig } from '@/common/config';
import { KAFKA_TOPICS } from '@/common/constants/kafka-topics';
import { getPrismaClient } from '@/common/database/prisma';
import { getKafkaProducer, publishWithRetry } from '@/common/kafka/producer';
import { logger } from '@/common/logger';
import { EventType } from '@/common/types/event.types';
import { OutboxEntity } from '@/common/types/outbox.types';
import { EventBridgeEvent } from 'aws-lambda';

const getTopicForEventType = (eventType: EventType): string => {
  switch (eventType) {
    case EventType.PAYMENT_COMPLETED:
      return KAFKA_TOPICS.PAYMENT_COMPLETED;
    case EventType.PAYMENT_FAILED:
      return KAFKA_TOPICS.PAYMENT_FAILED;
    case EventType.PAYMENT_CANCELLED:
      return KAFKA_TOPICS.PAYMENT_CANCELLED;
    default:
      logger.warn('Unknown event type, using default topic', { eventType });
      return KAFKA_TOPICS.PAYMENT_FAILED;
  }
};

const processOutboxEvent = async (event: OutboxEntity): Promise<boolean> => {
  try {
    const topic = getTopicForEventType(event.eventType as EventType);

    const metadata = await publishWithRetry(
      topic,
      event.payload,
      event.aggregateId, // Use aggregateId as partition key
      {
        eventType: event.eventType,
        aggregateType: event.aggregateType,
        eventId: event.id,
      },
    );

    logger.debug('Event published to Kafka', {
      id: event.id,
      topic,
      partition: metadata[0].partition,
    });

    return true;
  } catch (error) {
    logger.error('Failed to process outbox event', {
      id: event.id,
      eventType: event.eventType,
      error: error instanceof Error ? error.message : String(error),
    });

    return false;
  }
};

const updateOutboxEvent = async (
  eventId: string,
  success: boolean,
  error?: string,
): Promise<void> => {
  const prisma = getPrismaClient();

  if (success) {
    await prisma.outbox.update({
      where: { id: eventId },
      data: {
        publishedAt: new Date(),
      },
    });
  } else {
    await prisma.outbox.update({
      where: { id: eventId },
      data: {
        retryCount: { increment: 1 },
        lastError: error || 'Unknown error',
      },
    });
  }
};

export const processOutbox = async (): Promise<{
  processed: number;
  succeeded: number;
  failed: number;
}> => {
  const config = getConfig();
  const prisma = getPrismaClient();

  const batchSize = config.outbox.batchSize;
  const maxRetries = config.outbox.maxRetries;

  try {
    await getKafkaProducer();

    const pendingEvents = await prisma.outbox.findMany({
      where: {
        publishedAt: null,
        retryCount: { lt: maxRetries },
      },
      orderBy: {
        createdAt: 'asc',
      },
      take: batchSize,
    });

    if (pendingEvents.length === 0) {
      return {
        processed: 0,
        succeeded: 0,
        failed: 0,
      };
    }

    let succeeded = 0;
    let failed = 0;

    for (const event of pendingEvents) {
      const success = await processOutboxEvent(event);

      try {
        await updateOutboxEvent(
          event.id,
          success,
          success ? undefined : 'Failed to publish to Kafka',
        );

        if (success) {
          succeeded++;
        } else {
          failed++;
        }
      } catch (error) {
        logger.error('Failed to update outbox event status', {
          eventId: event.id,
          error: error instanceof Error ? error.message : String(error),
        });
        failed++;
      }
    }

    logger.info('Outbox processing completed', {
      processed: pendingEvents.length,
      succeeded,
      failed,
    });

    return {
      processed: pendingEvents.length,
      succeeded,
      failed,
    };
  } catch (error) {
    logger.error('Outbox processing failed', {
      error: error instanceof Error ? error.message : String(error),
    });

    throw error;
  }
};

export const handleScheduledEvent = async (
  event: EventBridgeEvent<string, any>,
): Promise<{ statusCode: number; body: string }> => {
  logger.info('Outbox processor triggered by EventBridge', {
    source: event.source,
    detailType: event['detail-type'],
    time: event.time,
  });

  try {
    const result = await processOutbox();

    return {
      statusCode: 200,
      body: JSON.stringify({
        message: 'Outbox processing completed',
        result,
      }),
    };
  } catch (error) {
    logger.error('Outbox processor handler error', {
      error: error instanceof Error ? error.message : String(error),
    });

    return {
      statusCode: 500,
      body: JSON.stringify({
        message: 'Outbox processing failed',
        error: error instanceof Error ? error.message : String(error),
      }),
    };
  }
};
