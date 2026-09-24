import { status as GrpcStatus } from '@grpc/grpc-js';
import { HttpException, HttpStatus } from '@nestjs/common';

// Where GlobalExceptionFilter leaves the resolved code for the metrics
// middleware. A symbol so it cannot collide with an express property.
export const TB_CODE = Symbol('tb.grpcCode');

// The gateway's half of the error taxonomy: which downstream codes become a
// status of their own. A code absent here is answered 500 -- genuinely ours.
export const GRPC_TO_HTTP: Partial<Record<GrpcStatus, HttpStatus>> = {
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

// The inverse of GRPC_TO_HTTP above, for the gateway's own rejections -- a
// guard or a pipe never held a gRPC code.
// 409 is ambiguous (the filter sends both ALREADY_EXISTS and
// FAILED_PRECONDITION there) but the gateway itself raises no 409.
const HTTP_TO_GRPC: Record<number, GrpcStatus> = {
  400: GrpcStatus.INVALID_ARGUMENT,
  401: GrpcStatus.UNAUTHENTICATED,
  403: GrpcStatus.PERMISSION_DENIED,
  404: GrpcStatus.NOT_FOUND,
  409: GrpcStatus.FAILED_PRECONDITION,
  429: GrpcStatus.RESOURCE_EXHAUSTED,
  501: GrpcStatus.UNIMPLEMENTED,
  503: GrpcStatus.UNAVAILABLE,
  504: GrpcStatus.DEADLINE_EXCEEDED,
};

// mapped downstream code   -> its own code, the one the filter sends a status for
// unmapped downstream code -> INTERNAL, because the filter answered 500
// gateway HttpException    -> the code that status came from
// anything else            -> INTERNAL, which is the 500 the filter sends
export const grpcCodeOf = (err: unknown): string => {
  const downstream = (err as { code?: unknown })?.code;
  if (typeof downstream === 'number') {
    // A code the filter does not map was answered 500, so INTERNAL is the
    // truthful label -- and the only one the alert watches.
    return downstream in GRPC_TO_HTTP ? GrpcStatus[downstream] : 'INTERNAL';
  }
  if (err instanceof HttpException) {
    const mapped = HTTP_TO_GRPC[err.getStatus()];
    return mapped === undefined ? 'INTERNAL' : GrpcStatus[mapped];
  }
  return 'INTERNAL';
};
