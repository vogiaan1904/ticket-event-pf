import { ErrorResponse } from '@/shared/interfaces/response-body.interface';
import { ArgumentsHost, Catch, ExceptionFilter, HttpException, HttpStatus } from '@nestjs/common';
import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';
import { Response } from 'express';
import { BusinessException } from '../exceptions/business.exception';

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
    const statusCode =
      exception instanceof HttpException ? exception.getStatus() : HttpStatus.INTERNAL_SERVER_ERROR;

    this.logger.error(
      responseBody.message,
      exception instanceof Error && exception.stack ? exception.stack : undefined,
    );

    response.status(statusCode).json(responseBody);
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

    return {
      success: false,
      message: this.config.isDev
        ? (exception as Error)?.message || 'Unknown error'
        : 'Internal server error',
    };
  }
}
