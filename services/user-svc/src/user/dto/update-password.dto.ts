import { IsNotEmpty, IsString } from 'class-validator';

export class UpdatePasswordDto {
  @IsNotEmpty()
  @IsString()
  id: string;

  @IsNotEmpty()
  @IsString()
  oldPassword: string;

  @IsNotEmpty()
  @IsString()
  newPassword: string;
}
