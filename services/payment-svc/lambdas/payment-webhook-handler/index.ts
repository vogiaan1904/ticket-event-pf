import { APIGatewayProxyEvent, APIGatewayProxyResult, Context } from 'aws-lambda';
import { logger } from '@/common/logger';
import { handleWebhook } from './handlers/webhook.handler';
import { getPrismaClient } from '@/common/database/prisma';

export const handler = async (
  event: APIGatewayProxyEvent,
  context: Context,
): Promise<APIGatewayProxyResult> => {
  // Set request ID from Lambda context
  logger.defaultMeta = {
    ...logger.defaultMeta,
    requestId: context.awsRequestId,
    functionName: context.functionName,
  };

  logger.info('Payment webhook handler invoked', {
    path: event.path,
    httpMethod: event.httpMethod,
    sourceIp: event.requestContext.identity.sourceIp,
  });

  try {
    // Process webhook
    const result = await handleWebhook(event);

    return result;
  } catch (error) {
    logger.error('Unhandled error in Lambda handler', {
      error: error instanceof Error ? error.message : String(error),
      stack: error instanceof Error ? error.stack : undefined,
    });

    // Return generic error response
    return {
      statusCode: 500,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        error: 'InternalServerError',
        message: 'An unexpected error occurred',
      }),
    };
  } finally {
    // Clean up Prisma connection if Lambda is shutting down
    // Note: In most cases, we want to keep the connection open for reuse
    // Only disconnect if explicitly needed
    if (context.getRemainingTimeInMillis() < 1000) {
      logger.warn('Lambda timeout approaching, disconnecting Prisma');
      await getPrismaClient().$disconnect();
    }
  }
};
