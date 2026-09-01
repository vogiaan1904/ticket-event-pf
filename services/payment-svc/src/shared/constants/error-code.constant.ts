import { status as grpcStatus } from '@grpc/grpc-js';

export enum ErrorCodeEnum {
  PermissionDenied = 20403,

  PaymentNotFound = 20000,
}

// [message, httpStatus, grpcCode]. The gRPC code is required — it is what the
// caller maps to an HTTP status. See the error taxonomy in the root CLAUDE.md.
export const ErrorCode = Object.freeze<
  Record<ErrorCodeEnum, [string, number, grpcStatus]>
>({
  [ErrorCodeEnum.PermissionDenied]: ['Permission denied', 403, grpcStatus.PERMISSION_DENIED],
  [ErrorCodeEnum.PaymentNotFound]: ['Payment not found', 404, grpcStatus.NOT_FOUND],
});
