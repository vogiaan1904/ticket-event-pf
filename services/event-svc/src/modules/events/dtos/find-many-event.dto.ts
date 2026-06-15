import { FilterEventDto } from './filter-event.dto';

export class FindManyEventDto {
  page: number;
  pageSize: number;
  filter: FilterEventDto;
}
