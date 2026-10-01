import { Type } from 'class-transformer';
import { IsNumber, IsOptional, IsString, Max, Min } from 'class-validator';

/** Orders page by cursor: omit it for the first page, then pass back `nextCursor`. */
export class PaginationDto {
  @IsString()
  @IsOptional()
  cursor?: string;

  @IsNumber()
  @IsOptional()
  @Min(1)
  @Max(100)
  @Type(() => Number)
  limit: number = 20;
}
