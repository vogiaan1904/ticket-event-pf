import { Request } from 'express';

export type RequestWithUser = Request & {
  user: RequestUser;
};

export type RequestUser = {
  id: string;
  email: string;
};
