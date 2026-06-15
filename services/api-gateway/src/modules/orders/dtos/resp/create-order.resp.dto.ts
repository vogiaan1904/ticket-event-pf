import { OrderRespDto } from './order.resp.dto';

export class CreateOrderRespDto {
  order: OrderRespDto;
  paymentUrl: string;
}
