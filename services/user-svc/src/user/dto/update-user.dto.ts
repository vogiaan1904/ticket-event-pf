import { UpdateUserRequest } from '@/protogen/user.pb';
import { IsNotEmpty, IsOptional, IsString } from 'class-validator';
export class UpdateUserDto implements UpdateUserRequest {
  @IsNotEmpty()
  id: string;

  @IsOptional()
  @IsString()
  firstName: string;

  @IsOptional()
  @IsString()
  lastName: string;

  @IsOptional()
  @IsString()
  avatar: string;

  @IsOptional()
  @IsString()
  password: string;
}
