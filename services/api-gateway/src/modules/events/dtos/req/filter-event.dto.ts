import { IsYYYYMMDD } from '@/common/decorators/isYYYYMMDD.decorator';
import { Transform, Type } from 'class-transformer';
import { IsArray, IsBoolean, IsEnum, IsOptional, IsString, IsUUID } from 'class-validator';
import { EventStatus } from '../../enums';

export class FilterEventDto {
  @IsOptional()
  @IsString()
  searchQuery?: string;

  @IsOptional()
  @IsArray()
  @IsUUID('4', { each: true })
  @Type(() => String)
  @Transform(({ value }) => value || [])
  categoryIds?: string[] = [];

  @IsOptional()
  @IsEnum(EventStatus)
  status?: EventStatus;

  @IsOptional()
  @IsUUID()
  organizerId?: string;

  @IsOptional()
  @IsUUID()
  userId?: string;

  @IsOptional()
  @IsYYYYMMDD()
  startDateFrom?: string;

  @IsOptional()
  @IsYYYYMMDD()
  startDateTo?: string;

  @IsOptional()
  @IsString()
  city?: string;

  @IsOptional()
  @IsString()
  country?: string;

  @IsOptional()
  @IsBoolean()
  @Transform(({ value }) => value === 'true')
  isPublic?: boolean;

  @IsOptional()
  @IsBoolean()
  @Transform(({ value }) => value === 'true')
  isFree?: boolean;
}
