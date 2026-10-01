import { OrderRespDto } from './order.resp.dto';

export class GetManyOrdersRespDto {
  data: OrderRespDto[];
  meta: {
    perPage: number;
    count: number;
    /** Pass back as `cursor` for the next page; empty on the last one. */
    nextCursor: string;
    hasNext: boolean;
  };
}
