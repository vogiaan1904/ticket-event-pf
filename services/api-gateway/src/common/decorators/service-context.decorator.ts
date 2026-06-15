import { IServiceContext } from '@interfaces/service-ctx.interface';
import { createParamDecorator, ExecutionContext } from '@nestjs/common';

export const UserContext = createParamDecorator((data: unknown, ctx: ExecutionContext): IServiceContext | undefined => {
  const request = ctx.switchToHttp().getRequest();
  return request.serviceCtx as IServiceContext | undefined;
});
