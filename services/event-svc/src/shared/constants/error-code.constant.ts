import { status as grpcStatus } from '@grpc/grpc-js';

export enum ErrorCodeEnum {
  PermissionDenied = 20403,

  EventNotFound = 20000,
  EventConfigNotFound = 20001,

  OrganizerNotFound = 21000,
}

// [message, httpStatus, grpcCode]. The gRPC code is required — it is what the
// caller maps to an HTTP status. See the error taxonomy in the root CLAUDE.md.
export const ErrorCode = Object.freeze<
  Record<ErrorCodeEnum, [string, number, grpcStatus]>
>({
  [ErrorCodeEnum.PermissionDenied]: ['Permission denied', 403, grpcStatus.PERMISSION_DENIED],
  [ErrorCodeEnum.EventNotFound]: ['Event not found', 404, grpcStatus.NOT_FOUND],
  [ErrorCodeEnum.OrganizerNotFound]: ['Organizer not found', 404, grpcStatus.NOT_FOUND],
  [ErrorCodeEnum.EventConfigNotFound]: ['Event config not found', 404, grpcStatus.NOT_FOUND],
});
