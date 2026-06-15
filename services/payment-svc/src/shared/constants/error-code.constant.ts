export enum ErrorCodeEnum {
  PermissionDenied = 20403,

  PaymentNotFound = 20000,
}

export const ErrorCode = Object.freeze<Record<ErrorCodeEnum, [string, number]>>(
  {
    [ErrorCodeEnum.PermissionDenied]: ['Permission denied', 403],

    [ErrorCodeEnum.PaymentNotFound]: ['Payment not found', 400],
  },
);
