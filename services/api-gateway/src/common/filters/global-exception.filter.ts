import { ErrorResponse } from '@/shared/interfaces/response-body.interface';
import { ArgumentsHost, Catch, ExceptionFilter, HttpException, HttpStatus } from '@nestjs/common';
import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';
import { status as GrpcStatus } from '@grpc/grpc-js';
import { Response } from 'express';
import { BusinessException } from '../exceptions/business.exception';

// Every downstream service speaks gRPC, and a gRPC error is not an
// HttpException — so without this table it falls through to the 500 branch
// below. A sold-out ticket class, a missing event and a failed validation all
// arrive as INVALID_ARGUMENT or NOT_FOUND and would be reported as server
// faults. Codes absent from the map are genuinely ours to answer for.
const GRPC_TO_HTTP: Partial<Record<GrpcStatus, HttpStatus>> = {
  [GrpcStatus.INVALID_ARGUMENT]: HttpStatus.BAD_REQUEST,
  [GrpcStatus.NOT_FOUND]: HttpStatus.NOT_FOUND,
  [GrpcStatus.ALREADY_EXISTS]: HttpStatus.CONFLICT,
  [GrpcStatus.PERMISSION_DENIED]: HttpStatus.FORBIDDEN,
  [GrpcStatus.UNAUTHENTICATED]: HttpStatus.UNAUTHORIZED,
  [GrpcStatus.FAILED_PRECONDITION]: HttpStatus.CONFLICT,
  [GrpcStatus.OUT_OF_RANGE]: HttpStatus.BAD_REQUEST,
  [GrpcStatus.ABORTED]: HttpStatus.CONFLICT,
  [GrpcStatus.UNIMPLEMENTED]: HttpStatus.NOT_IMPLEMENTED,
  [GrpcStatus.UNAVAILABLE]: HttpStatus.SERVICE_UNAVAILABLE,
  [GrpcStatus.DEADLINE_EXCEEDED]: HttpStatus.GATEWAY_TIMEOUT,
};

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

    // A mapped gRPC error is the downstream service's considered answer, so its
    // message is the useful one and is safe to pass through in any environment.
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
