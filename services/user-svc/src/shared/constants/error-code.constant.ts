import { status as grpcStatus } from '@grpc/grpc-js';

export enum ErrorCodeEnum {
  PermissionDenied = 'USR403',

  UserNotFound = 'USR000',
  UserAlreadyExists = 'USR001',
}

// [message, httpStatus, grpcCode]. The gRPC code is required: it is what
// the caller maps to an HTTP status, and a missing one used to make every
// rejection an INVALID_ARGUMENT. See the error taxonomy in the root CLAUDE.md.
export const ErrorCode = Object.freeze<
  Record<ErrorCodeEnum, [string, number, grpcStatus]>
>({
  [ErrorCodeEnum.PermissionDenied]: ['Permission denied', 403, grpcStatus.PERMISSION_DENIED],
  [ErrorCodeEnum.UserNotFound]: ['User not found', 404, grpcStatus.NOT_FOUND],
  [ErrorCodeEnum.UserAlreadyExists]: ['User already exists', 409, grpcStatus.ALREADY_EXISTS],
});
