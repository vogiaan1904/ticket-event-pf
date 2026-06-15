import { PaginationQuery } from '@/shared/interfaces/pagination-input.interface';
import { Type } from 'class-transformer';
import { IsNumber, IsOptional, Max, Min } from 'class-validator';

export class PaginationDto implements PaginationQuery {
  @IsNumber()
  @IsOptional()
  @Min(1)
  @Type(() => Number)
  page: number = 1;

  @IsNumber()
  @IsOptional()
  @Min(1)
  @Max(100)
  @Type(() => Number)
  limit: number = 20;
}
