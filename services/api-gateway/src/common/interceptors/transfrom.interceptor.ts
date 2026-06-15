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
import { plainToInstance } from 'class-transformer';
import { SENSITIVE_FIELDS } from '@/shared/constants/system.constant';

export const RESPONSE_DTO_KEY = 'responseDto';
export const ResponseDto = (dto: Type) => SetMetadata(RESPONSE_DTO_KEY, dto);
const transformOptions = {
  excludePrefixes: SENSITIVE_FIELDS.EXCLUDE_PREFIXES,
  excludeExtraneousValues: false,
  enableImplicitConversion: true,
};

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
          return plainToInstance(responseDto, data, transformOptions);
        }

        // Handle paginated responses
        if (data.data && Array.isArray(data.data)) {
          return {
            ...data,
            data: plainToInstance(responseDto, data.data, transformOptions),
          };
        }

        return plainToInstance(responseDto, data, transformOptions);
      }),
    );
  }
}
