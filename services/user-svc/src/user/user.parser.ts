import { User } from '@prisma/client';
import { User as UserPb } from '@/protogen/user.pb';

export const parseUserToPb = (user: User | null): UserPb => {
  if (!user) {
    return null;
  }

  return {
    ...user,
    createdAt: user.createdAt.toISOString(),
    updatedAt: user.updatedAt.toISOString(),
  };
};
