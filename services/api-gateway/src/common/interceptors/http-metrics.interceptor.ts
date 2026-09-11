import { SERVICE_NAME, grpcDuration, grpcInFlight, grpcRequests } from '@/shared/metrics/metrics';
import { status as GrpcStatus } from '@grpc/grpc-js';
import {
  CallHandler,
  ExecutionContext,
  HttpException,
  Injectable,
  NestInterceptor,
} from '@nestjs/common';
import { Request } from 'express';
import { Observable } from 'rxjs';
import { finalize, tap } from 'rxjs/operators';

// The inverse of GRPC_TO_HTTP in filters/global-exception.filter.ts, for the
// gateway's own rejections -- a guard or a pipe never held a gRPC code.
// 409 is ambiguous (the filter sends both ALREADY_EXISTS and
// FAILED_PRECONDITION there) but the gateway itself raises no 409.
const HTTP_TO_GRPC: Record<number, GrpcStatus> = {
  400: GrpcStatus.INVALID_ARGUMENT,
  401: GrpcStatus.UNAUTHENTICATED,
  403: GrpcStatus.PERMISSION_DENIED,
  404: GrpcStatus.NOT_FOUND,
  409: GrpcStatus.FAILED_PRECONDITION,
  501: GrpcStatus.UNIMPLEMENTED,
  503: GrpcStatus.UNAVAILABLE,
  504: GrpcStatus.DEADLINE_EXCEEDED,
};

// downstream gRPC error -> its own code, the one the filter maps to a status
// gateway HttpException  -> the code that status came from
// anything else          -> INTERNAL, which is the 500 the filter sends
const codeOf = (err: unknown): string => {
  const downstream = (err as { code?: unknown })?.code;
  if (typeof downstream === 'number') {
    return GrpcStatus[downstream] ?? 'UNKNOWN';
  }
  if (err instanceof HttpException) {
    const mapped = HTTP_TO_GRPC[err.getStatus()];
    return mapped === undefined ? 'INTERNAL' : GrpcStatus[mapped];
  }
  return 'INTERNAL';
};

// The registered route, never req.url: a path carrying an order code or an
// event id is one series per buyer.
const methodOf = (context: ExecutionContext): string => {
  const req = context.switchToHttp().getRequest<Request>();
  return `${req.method} ${req.route?.path ?? 'unmatched'}`;
};

@Injectable()
export class HttpMetricsInterceptor implements NestInterceptor {
  intercept(context: ExecutionContext, next: CallHandler): Observable<unknown> {
    const method = methodOf(context);
    const start = process.hrtime.bigint();
    grpcInFlight.inc({ service: SERVICE_NAME });

    const observe = (code: string): void => {
      grpcRequests.inc({ service: SERVICE_NAME, method, code });
      grpcDuration.observe(
        { service: SERVICE_NAME, method },
        Number(process.hrtime.bigint() - start) / 1e9,
      );
    };

    return next.handle().pipe(
      tap({ complete: () => observe('OK'), error: (err) => observe(codeOf(err)) }),
      // finalize, not tap: a client that disconnects completes neither branch
      // above, and an in-flight gauge that only counts up is worse than none.
      finalize(() => grpcInFlight.dec({ service: SERVICE_NAME })),
    );
  }
}
