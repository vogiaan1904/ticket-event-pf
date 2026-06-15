import { createParamDecorator, ExecutionContext } from '@nestjs/common';
import { IUserContext } from '@interfaces/user-ctx.interface';

export const UserContext = createParamDecorator(
  (data: unknown, ctx: ExecutionContext): IUserContext | undefined => {
    const request = ctx.switchToHttp().getRequest();
    return request.userCtx as IUserContext | undefined;
  },
);
