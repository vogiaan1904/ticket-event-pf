export enum ErrorCodeEnum {
  PermissionDenied = 20403,

  EventNotFound = 20000,
  EventConfigNotFound = 20001,

  OrganizerNotFound = 21000,
}

export const ErrorCode = Object.freeze<Record<ErrorCodeEnum, [string, number]>>({
  [ErrorCodeEnum.PermissionDenied]: ['Permission denied', 403],

  [ErrorCodeEnum.EventNotFound]: ['Event not found', 400],

  [ErrorCodeEnum.OrganizerNotFound]: ['Organizer not found', 400],

  [ErrorCodeEnum.EventConfigNotFound]: ['Event config not found', 400],
});
