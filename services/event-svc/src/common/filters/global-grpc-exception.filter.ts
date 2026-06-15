import { LoggerService } from '@/shared/services/logger.service';
import { ArgumentsHost, Catch, RpcExceptionFilter } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { Observable, throwError } from 'rxjs';

@Catch()
export class GlobalGrpcExceptionFilter implements RpcExceptionFilter<any> {
  constructor(private readonly logger: LoggerService) {
    this.logger.setContext(GlobalGrpcExceptionFilter.name);
  }
  catch(exception: any, host: ArgumentsHost): Observable<any> {
    console.log(exception);
    if (exception instanceof RpcException) {
      const error: any = exception.getError();
      console.log(error);
      return throwError(() => error);
    } else {
      console.log(exception);
      this.logger.error(`Unhandled exception: ${exception?.message}`, exception?.stack);
      return throwError(() => new RpcException('Internal server error'));
    }
  }
}
