import { status } from '@grpc/grpc-js';
import { RpcException } from '@nestjs/microservices';

export class RpcValidationException extends RpcException {
  constructor(message: string, details?: string | object) {
    super({
      code: status.INVALID_ARGUMENT,
      message,
      details,
    });
  }
}
