import { EventBridgeEvent, Context } from 'aws-lambda';
import { logger } from '@/common/logger';
import { handleScheduledEvent } from './handlers/cleanup.handler';
import { getPrismaClient } from '@/common/database/prisma';

export const handler = async (
  event: EventBridgeEvent<string, any>,
  context: Context,
): Promise<{ statusCode: number; body: string }> => {
  logger.defaultMeta = {
    ...logger.defaultMeta,
    requestId: context.awsRequestId,
    functionName: context.functionName,
  };

  logger.info('Outbox cleanup Lambda invoked', {
    source: event.source,
    detailType: event['detail-type'],
  });

  try {
    const result = await handleScheduledEvent(event);

    return result;
  } catch (error) {
    logger.error('Unhandled error in Lambda handler', {
      error: error instanceof Error ? error.message : String(error),
      stack: error instanceof Error ? error.stack : undefined,
    });

    return {
      statusCode: 500,
      body: JSON.stringify({
        message: 'An unexpected error occurred',
        error: error instanceof Error ? error.message : String(error),
      }),
    };
  } finally {
    if (context.getRemainingTimeInMillis() < 1000) {
      logger.warn('Lambda timeout approaching, disconnecting Prisma');

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
