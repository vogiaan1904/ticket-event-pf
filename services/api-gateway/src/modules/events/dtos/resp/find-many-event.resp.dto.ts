import { PaginationResponse } from '@/shared/interfaces/response-body.interface';
import { EventRespDto } from './event.resp.dto';

export class FindManyEventRespDto {
  data: EventRespDto[];
  meta: PaginationResponse;
}
