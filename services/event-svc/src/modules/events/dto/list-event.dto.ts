import { ListEventRequest } from '@/protogen/event.pb';
import { FilterEventDto } from './filter-event.dto';
import { Type } from 'class-transformer';
import { IsOptional, ValidateNested } from 'class-validator';

export class ListEventDto implements ListEventRequest {
  @IsOptional()
  @ValidateNested()
  @Type(() => FilterEventDto)
  filter?: FilterEventDto;
}
