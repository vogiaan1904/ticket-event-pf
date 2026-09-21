import { LoggerService } from '@/shared/services/logger.service';
import { status as grpcStatus } from '@grpc/grpc-js';
import { ArgumentsHost, Catch, RpcExceptionFilter } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { Observable, throwError } from 'rxjs';

@Catch()
export class GlobalGrpcExceptionFilter implements RpcExceptionFilter<any> {
  constructor(private readonly logger: LoggerService) {
    this.logger.setContext(GlobalGrpcExceptionFilter.name);
  }

  catch(exception: any, host: ArgumentsHost): Observable<any> {
    if (exception instanceof RpcException) {
      return throwError(() => exception.getError());
    }

    this.logger.error(`Unhandled exception: ${exception?.message}`, exception?.stack);

    // A payload, not an RpcException instance: grpc-js reads `code` off the
    // error it is given, and an instance without one goes out as UNKNOWN.
    return throwError(() => ({
      code: grpcStatus.INTERNAL,
      message: 'Internal server error',
    }));
  }
}
