import { EventFilter, EventStatus } from '@/protogen/event.pb';
import { FilterEventDto as ServiceFilterEventDto } from '../../../dtos';
import { EventStatusMapper } from '../mappers';
import { IsArray, IsBoolean, IsEnum, IsOptional, IsString, IsUUID } from 'class-validator';
import { Transform } from 'class-transformer';

export class FilterEventDto implements EventFilter {
  toServiceDto(): ServiceFilterEventDto {
    return {
      searchQuery: this.searchQuery,
      categoryIds: this.categoryIds?.length ? this.categoryIds : undefined,
      status: this.status !== undefined ? EventStatusMapper.toPrisma(this.status) : undefined,
      organizerId: this.organizerId,
      userId: this.userId,
      startDateFrom: this.startDateFrom ? new Date(this.startDateFrom) : undefined,
      startDateTo: this.startDateTo ? new Date(this.startDateTo) : undefined,
      city: this.city,
      country: this.country,
      isPublic: this.isPublic,
      isFree: this.isFree,
    };
  }

  @IsOptional()
  @IsString()
  searchQuery?: string;

  @IsArray()
  @IsString({ each: true })
  @Transform(({ value }) => (Array.isArray(value) ? value : []))
  categoryIds: string[] = [];

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
  startDateFrom?: string;

  @IsOptional()
  startDateTo?: string;

  @IsOptional()
  @IsString()
  city?: string;

  @IsOptional()
  @IsString()
  country?: string;

  @IsOptional()
  @IsBoolean()
  isPublic?: boolean;

  @IsOptional()
  @IsBoolean()
  isFree?: boolean;
}
