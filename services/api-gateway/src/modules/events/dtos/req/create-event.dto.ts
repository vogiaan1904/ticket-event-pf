import { IsArray, IsDateString, IsNotEmpty, IsOptional, IsString, IsUrl } from 'class-validator';

export class CreateEventDto {
  @IsNotEmpty()
  @IsString()
  name: string;

  @IsNotEmpty()
  @IsString()
  description: string;

  @IsNotEmpty()
  @IsDateString()
  startDate: string;

  @IsNotEmpty()
  @IsDateString()
  endDate: string;

  @IsNotEmpty()
  @IsUrl()
  thumbnailUrl: string;

  @IsNotEmpty()
  @IsString()
  venue: string;

  @IsNotEmpty()
  @IsString()
  street: string;

  @IsNotEmpty()
  @IsString()
  city: string;

  @IsNotEmpty()
  @IsString()
  country: string;

  @IsOptional()
  @IsString()
  ward?: string;

  @IsOptional()
  @IsString()
  district?: string;

  @IsOptional()
  @IsArray()
  @IsString({ each: true })
  categoryIds: string[];

  @IsNotEmpty()
  @IsString()
  organizerName: string;

  @IsNotEmpty()
  @IsString()
  organizerDescription: string;

  @IsNotEmpty()
  @IsUrl()
  organizerLogoUrl: string;
}
