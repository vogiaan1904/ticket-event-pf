import { IsOptional, IsString, IsUrl } from 'class-validator';

export class UpdateUserDto {
  @IsOptional()
  @IsString()
  @IsUrl()
  avatar: string;

  @IsOptional()
  @IsString()
  firstName: string;

  @IsOptional()
  @IsString()
  lastName: string;
}
