import { FindManyEventRequest } from '@/protogen/event.pb';
import { ApiPropertyOptional } from '@nestjs/swagger';
import { Transform, Type } from 'class-transformer';
import { IsInt, IsOptional, IsString, Max, Min, ValidateNested } from 'class-validator';
import { FilterEventDto } from './filter-event.dto';

export enum EventSortField {
  NAME = 'name',
  START_DATE = 'startDate',
  END_DATE = 'endDate',
  CREATED_AT = 'createdAt',
  UPDATED_AT = 'updatedAt',
  CAPACITY = 'capacity',
  STATUS = 'status',
  // Relationship fields
  TICKETS_COUNT = 'ticketsCount',
  ORDERS_COUNT = 'ordersCount',
  VENUE = 'venue',
}

export class SortEventDto {
  @ApiPropertyOptional()
  @IsString()
  @IsOptional()
  orderBy?: EventSortField;

  @ApiPropertyOptional()
  @IsString()
  @IsOptional()
  order?: 'asc' | 'desc';
}

export class FindManyEventDto implements FindManyEventRequest {
  @IsInt()
  @Min(1)
  @Transform(({ value }) => parseInt(value))
  page: number = 1;

  @IsInt()
  @Min(1)
  @Max(100)
  @Transform(({ value }) => parseInt(value))
  pageSize: number = 20;

  @IsOptional()
  @ValidateNested()
  @Type(() => FilterEventDto)
  filter: FilterEventDto;
}
