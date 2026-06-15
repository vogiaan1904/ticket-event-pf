import { isArrayLike } from 'lodash';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';

import * as SYSTEM from '@constants/system.constant';
import { CallHandler, ExecutionContext, Injectable, NestInterceptor } from '@nestjs/common';
import { Reflector } from '@nestjs/core';
import { SuccessResponse } from '@/shared/interfaces/response-body.interface';
import { SUCCESS_MESSAGE_KEY } from '../decorators/success-response.decorator';

@Injectable()
export class ResponseInterceptor<T> implements NestInterceptor<T, SuccessResponse<T>> {
  constructor(private readonly reflector: Reflector) {}
  intercept(context: ExecutionContext, next: CallHandler): Observable<SuccessResponse<T>> {
    if (!context.switchToHttp().getRequest()) {
      return next.handle();
    }
    const handler = context.getHandler();
    const controllerClass = context.getClass();

    const bypass = this.reflector.get<boolean>(SYSTEM.RESPONSE_PASSTHROUGH_METADATA, handler);
    if (bypass) {
      return next.handle();
    }

    const message = this.reflector.getAllAndOverride<string>(SUCCESS_MESSAGE_KEY, [
      handler,
      controllerClass,
    ]);

    const defaultMessage = 'OK';

    return next.handle().pipe(
      map((data) => {
        if (typeof data === 'undefined') {
          context.switchToHttp().getResponse().status(200);
          return {
            success: true,
            message: message || defaultMessage,
            data: null as any,
          };
        }

        return {
          success: true,
          message: message || defaultMessage,
          data: isArrayLike(data) ? data : data,
        };
      }),
    );
  }
}
