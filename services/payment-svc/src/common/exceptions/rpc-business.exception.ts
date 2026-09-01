import { ErrorCode, ErrorCodeEnum } from '@/shared/constants/error-code.constant';
import { RpcException } from '@nestjs/microservices';
import { status as grpcStatus } from '@grpc/grpc-js';

export class RpcBusinessException extends RpcException {
  constructor(code: ErrorCodeEnum) {
    const [message, , grpcCode] = ErrorCode[code];
    super({
      code: grpcCode,
      message: `${code} - ${message}`,
    });
  }
}
