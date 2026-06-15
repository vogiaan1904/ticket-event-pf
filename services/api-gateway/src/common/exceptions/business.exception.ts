import { ErrorResponse } from '@/shared/interfaces/response-body.interface';
import { ErrorCode, ErrorCodeEnum } from '@constants/error-code.constant';
import { HttpException } from '@nestjs/common';

export class BusinessException extends HttpException {
  constructor(code: ErrorCodeEnum) {
    const [message, status] = ErrorCode[code];
    const body: ErrorResponse = {
      success: false,
      message,
      code,
    };
    super(body, status);
  }
}
