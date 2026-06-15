export enum ErrorCodeEnum {
  PermissionDenied = 'USR403',

  UserNotFound = 'USR000',
  UserAlreadyExists = 'USR001',
}

export const ErrorCode = Object.freeze<Record<ErrorCodeEnum, [string, number]>>({
  [ErrorCodeEnum.PermissionDenied]: ['Permission denied', 403],

  [ErrorCodeEnum.UserNotFound]: ['User not found', 400],
  [ErrorCodeEnum.UserAlreadyExists]: ['User already exists', 400],
});
