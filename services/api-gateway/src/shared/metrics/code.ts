import { status as GrpcStatus } from '@grpc/grpc-js';
import { HttpException } from '@nestjs/common';

// Where GlobalExceptionFilter leaves the resolved code for the metrics
// middleware. A symbol so it cannot collide with an express property.
export const TB_CODE = Symbol('tb.grpcCode');

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
export const grpcCodeOf = (err: unknown): string => {
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
