// src/common/interceptors/transform.interceptor.ts
import {
  Injectable,
  NestInterceptor,
  ExecutionContext,
  CallHandler,
  Type,
  SetMetadata,
} from '@nestjs/common';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';
import { Reflector } from '@nestjs/core';

export const RESPONSE_DTO_KEY = 'responseDto';
export const ResponseDto = (dto: Type) => SetMetadata(RESPONSE_DTO_KEY, dto);

@Injectable()
export class TransformInterceptor implements NestInterceptor {
  constructor(private reflector: Reflector) {}

  intercept(context: ExecutionContext, next: CallHandler): Observable<any> {
    const responseDto = this.reflector.getAllAndOverride(RESPONSE_DTO_KEY, [
      context.getHandler(),
      context.getClass(),
    ]);

    return next.handle().pipe(
      map((data) => {
        if (!responseDto || !data) {
          return data;
        }

        if (Array.isArray(data)) {
          return data.map((item) => new responseDto(item));
        }

        // Handle paginated responses
        if (data.data && Array.isArray(data.data)) {
          return {
            ...data,
            data: data.data.map((item) => new responseDto(item)),
          };
        }

        return new responseDto(data);
      }),
    );
  }
}
