import { APIGatewayProxyResult } from 'aws-lambda';
import { logger } from '../logger';

export interface ErrorResponse {
  error: string;
  message: string;
  details?: any;
  requestId?: string;
}

export const createErrorResponse = (
  statusCode: number,
  error: string,
  message: string,
  details?: any,
): APIGatewayProxyResult => {
  const response: ErrorResponse = {
    error,
    message,
    details,
  };

  return {
    statusCode,
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(response),
  };
};

export const handleError = (
  error: unknown,
  context?: Record<string, any>,
): APIGatewayProxyResult => {
  if (error instanceof Error) {
    logger.error('Lambda execution error', {
      name: error.name,
      message: error.message,
      stack: error.stack,
      ...context,
    });

    const statusCode = getStatusCodeForError(error);

    return createErrorResponse(
      statusCode,
      error.name,
      error.message,
      process.env.NODE_ENV === 'development' ? { stack: error.stack } : undefined,
    );
  }

  logger.error('Unknown error occurred', { error, ...context });

  return createErrorResponse(500, 'InternalServerError', 'An unexpected error occurred', undefined);
};

const getStatusCodeForError = (error: Error): number => {
  const errorName = error.name.toLowerCase();

  if (errorName.includes('validation')) return 400;
  if (errorName.includes('unauthorized') || errorName.includes('authentication')) return 401;
  if (errorName.includes('forbidden') || errorName.includes('permission')) return 403;
  if (errorName.includes('notfound') || errorName.includes('not found')) return 404;
  if (errorName.includes('conflict')) return 409;
  if (errorName.includes('timeout')) return 408;

  return 500;
};

export class ValidationError extends Error {
  constructor(
    message: string,
    public details?: any,
  ) {
    super(message);
    this.name = 'ValidationError';
  }
}

export class NotFoundError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'NotFoundError';
  }
}

export class UnauthorizedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'UnauthorizedError';
  }
}

export class PaymentProviderError extends Error {
  constructor(
    message: string,
    public provider: string,
    public details?: any,
  ) {
    super(message);
    this.name = 'PaymentProviderError';
  }
}
