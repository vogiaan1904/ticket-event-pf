import { EventBridgeEvent, Context } from 'aws-lambda';
import { logger } from '@/common/logger';
import { handleScheduledEvent } from './handlers/processor.handler';
import { getPrismaClient } from '@/common/database/prisma';
import { disconnectKafka } from '@/common/kafka/producer';

export const handler = async (
  event: EventBridgeEvent<string, any>,
  context: Context,
): Promise<{ statusCode: number; body: string }> => {
  // Set request ID from Lambda context
  logger.defaultMeta = {
    ...logger.defaultMeta,
    requestId: context.awsRequestId,
    functionName: context.functionName,
  };

  logger.info('Outbox processor Lambda invoked', {
    source: event.source,
    detailType: event['detail-type'],
  });

  try {
    // Process outbox events
    const result = await handleScheduledEvent(event);

    return result;
  } catch (error) {
    logger.error('Unhandled error in Lambda handler', {
      error: error instanceof Error ? error.message : String(error),
      stack: error instanceof Error ? error.stack : undefined,
    });

    // Return error response
    return {
      statusCode: 500,
      body: JSON.stringify({
        message: 'An unexpected error occurred',
        error: error instanceof Error ? error.message : String(error),
      }),
    };
  } finally {
    // Clean up connections if Lambda is shutting down
    if (context.getRemainingTimeInMillis() < 1000) {
      logger.warn('Lambda timeout approaching, disconnecting clients');

      try {
        await disconnectKafka();
      } catch (error) {
        logger.error('Failed to disconnect Kafka producer', {
          error: error instanceof Error ? error.message : String(error),
        });
      }

      try {
        await getPrismaClient().$disconnect();
      } catch (error) {
        logger.error('Failed to disconnect Prisma client', {
          error: error instanceof Error ? error.message : String(error),
        });
      }
    }
  }
};
