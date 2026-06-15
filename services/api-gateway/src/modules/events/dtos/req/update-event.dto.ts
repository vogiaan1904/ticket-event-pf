import { IsYYYYMMDD } from '@/common/decorators/isYYYYMMDD.decorator';
import { Transform, Type } from 'class-transformer';
import { IsArray, IsOptional, IsString, IsUrl, IsUUID } from 'class-validator';

export class UpdateEventDto {
  @IsOptional()
  @IsString()
  name?: string;

  @IsOptional()
  @IsString()
  description?: string;

  @IsOptional()
  @IsYYYYMMDD()
  startDate?: string;

  @IsOptional()
  @IsYYYYMMDD()
  endDate?: string;

  @IsOptional()
  @IsUrl()
  thumbnailUrl?: string;

  @IsOptional()
  @IsString()
  venue?: string;

  @IsOptional()
  @IsString()
  street?: string;

  @IsOptional()
  @IsString()
  city?: string;

  @IsOptional()
  @IsString()
  country?: string;

  @IsOptional()
  @IsString()
  ward?: string;

  @IsOptional()
  @IsString()
  district?: string;

  @IsOptional()
  @IsArray()
  @IsUUID('4', { each: true })
  @Type(() => String)
  @Transform(({ value }) => value || [])
  categoryIds?: string[];

  @IsOptional()
  @IsString()
  organizerName?: string;

  @IsOptional()
  @IsString()
  organizerDescription?: string;

  @IsOptional()
  @IsUrl()
  organizerLogoUrl?: string;
}
