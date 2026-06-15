import { IsArray, IsEmail, IsOptional, IsString } from 'class-validator';

export class QueryUserDto {
  @IsOptional()
  @IsArray()
  @IsString({ each: true })
  ids: string[];

  @IsOptional()
  @IsArray()
  @IsEmail({}, { each: true })
  emails: string[];

  @IsOptional()
  @IsString()
  name: string;
}
