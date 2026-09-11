import { SERVICE_NAME, grpcDuration, grpcInFlight, grpcRequests } from '@/shared/metrics/metrics';
import { status as grpcStatus } from '@grpc/grpc-js';
import { CallHandler, ExecutionContext, Injectable, NestInterceptor } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { Observable } from 'rxjs';
import { finalize, tap } from 'rxjs/operators';

// The code the CALLER sees, not the class of the exception thrown here:
//
// RpcBusinessException | RpcValidationException -> { code } from the ErrorCode
//                                                  tuple -> that code on the wire
// anything else -> GlobalGrpcExceptionFilter replaces it with a codeless
//                  RpcException, which grpc-js sends as UNKNOWN, not INTERNAL
const codeOf = (err: unknown): string => {
  const error = err instanceof RpcException ? err.getError() : undefined;
  const code =
    typeof error === 'object' && error !== null ? (error as { code?: unknown }).code : undefined;
  return typeof code === 'number' ? (grpcStatus[code] ?? 'UNKNOWN') : 'UNKNOWN';
};

// The gRPC transport hands the handler (data, metadata, call), so the third
// argument carries the wire path -- the same string the Go services label with.
const methodOf = (context: ExecutionContext): string => {
  const call = context.getArgByIndex(2) as { getPath?: () => string } | undefined;
  return call?.getPath?.() ?? `${context.getClass().name}.${context.getHandler().name}`;
};

@Injectable()
export class GrpcMetricsInterceptor implements NestInterceptor {
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
      // finalize, not tap: a cancelled call completes neither branch above, and
      // an in-flight gauge that only counts up is worse than none.
      finalize(() => grpcInFlight.dec({ service: SERVICE_NAME })),
    );
  }
}
