import { User } from '@/protogen/user.pb';
import { Exclude, Expose } from 'class-transformer';

@Expose()
export class UserDto implements User {
  id: string;
  firstName: string;
  lastName: string;
  email: string;
  avatar: string;
  emailVerified: boolean;
  createdAt: string;
  updatedAt: string;

  @Exclude()
  password: string;
}
