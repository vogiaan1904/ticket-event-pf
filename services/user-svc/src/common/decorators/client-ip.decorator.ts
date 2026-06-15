import { createParamDecorator, ExecutionContext } from '@nestjs/common';
import { ServerUnaryCall } from '@grpc/grpc-js';

export const ClientIp = createParamDecorator((data: unknown, ctx: ExecutionContext): string => {
  const call = ctx.switchToRpc().getContext<ServerUnaryCall<any, any>>();
  const peer = call.getPeer();
  return peer.replace(/^(ipv4|ipv6):/, '').split(':')[0];
});
