import { ErrorResponse } from '@/shared/interfaces/response-body.interface';
import { ArgumentsHost, Catch, ExceptionFilter, HttpException, HttpStatus } from '@nestjs/common';
import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';
import { status as GrpcStatus } from '@grpc/grpc-js';
import { Response } from 'express';
import { BusinessException } from '../exceptions/business.exception';
import { GRPC_TO_HTTP, TB_CODE, grpcCodeOf } from '@/shared/metrics/code';

interface GrpcError {
  code: GrpcStatus;
  details?: string;
  message: string;
}

const asGrpcError = (e: unknown): GrpcError | null =>
  typeof e === 'object' && e !== null && typeof (e as GrpcError).code === 'number'
    ? (e as GrpcError)
    : null;

@Catch()
export class GlobalExceptionFilter implements ExceptionFilter {
  constructor(
    private readonly config: AppConfigService,
    private readonly logger: LoggerService,
  ) {
    this.logger.setContext(GlobalExceptionFilter.name);
  }
  catch(exception: unknown, host: ArgumentsHost) {
    const ctx = host.switchToHttp();
    const response = ctx.getResponse<Response>();

    const responseBody = this.handleException(exception, ctx.getRequest());
    const statusCode = this.resolveStatus(exception);

    // Left for MetricsMiddleware, which records after the response finishes:
    // a guard rejection never reaches an interceptor, so this is the only
    // place the gateway learns what code a failed request ended with.
    ctx.getRequest()[TB_CODE] = grpcCodeOf(exception);

    this.logger.error(
      responseBody.message,
      exception instanceof Error && exception.stack ? exception.stack : undefined,
    );

    response.status(statusCode).json(responseBody);
  }

  private resolveStatus(exception: unknown): HttpStatus {
    if (exception instanceof HttpException) {
      return exception.getStatus();
    }

    const grpcError = asGrpcError(exception);
    if (grpcError) {
      return GRPC_TO_HTTP[grpcError.code] ?? HttpStatus.INTERNAL_SERVER_ERROR;
    }

    return HttpStatus.INTERNAL_SERVER_ERROR;
  }

  private handleException(exception: unknown, request: Request): ErrorResponse {
    if (exception instanceof BusinessException) {
      const response = exception.getResponse() as ErrorResponse;
      return {
        ...response,
      };
    }

    if (exception instanceof HttpException) {
      return {
        success: false,
        message: exception.message,
        details: exception.getResponse(),
      };
    }

    // A mapped gRPC error is the downstream service's own answer, so its message
    // is safe to pass through in any environment.
    const grpcError = asGrpcError(exception);
    if (grpcError && GRPC_TO_HTTP[grpcError.code]) {
      return {
        success: false,
        message: grpcError.details ?? grpcError.message,
      };
    }

    return {
      success: false,
      message: this.config.isDev
        ? (exception as Error)?.message || 'Unknown error'
        : 'Internal server error',
    };
  }
}
