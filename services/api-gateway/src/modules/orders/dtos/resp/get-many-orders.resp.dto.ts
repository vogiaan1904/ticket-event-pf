import { OrderRespDto } from './order.resp.dto';

export class GetManyOrdersRespDto {
  data: OrderRespDto[];
  meta: {
    currentPage: number;
    perPage: number;
    total: number;
    lastPage: number;
    hasNext: boolean;
    hasPrevious: boolean;
  };
}
