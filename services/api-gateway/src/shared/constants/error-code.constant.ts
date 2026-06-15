export enum ErrorCodeEnum {
  PermissionDenied = 'AUTH0403',
  TokenExpired = 'AUTH0401',
  InvalidToken = 'AUTH0402',
  UserAlreadyExists = 'AUTH0001',
  InvalidCredentials = 'AUTH0002',

  UserNotFound = 'USER0000',

  SessionNotFound = 'WAITROOM0001',
  SessionAlreadyExists = 'WAITROOM0002',
  WaitroomNotAvailable = 'WAITROOM0003',
}

export const ErrorCode = Object.freeze<Record<ErrorCodeEnum, [string, number]>>({
  [ErrorCodeEnum.PermissionDenied]: ['Permission denied', 403],
  [ErrorCodeEnum.TokenExpired]: ['Token expired', 401],
  [ErrorCodeEnum.InvalidToken]: ['Invalid token', 401],
  [ErrorCodeEnum.UserAlreadyExists]: ['User already exists', 400],
  [ErrorCodeEnum.InvalidCredentials]: ['Invalid credentials', 400],

  [ErrorCodeEnum.UserNotFound]: ['User not found', 400],

  [ErrorCodeEnum.SessionNotFound]: ['Session not found', 404],
  [ErrorCodeEnum.SessionAlreadyExists]: ['Session already exists', 400],
  [ErrorCodeEnum.WaitroomNotAvailable]: ['Waitroom not available', 503],
});
