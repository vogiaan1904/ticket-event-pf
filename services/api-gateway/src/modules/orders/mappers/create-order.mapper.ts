import { CreateOrderResponse } from '@/protogen/order.pb';
import { CreateOrderRespDto } from '../dtos/resp';
import { OrderMapper } from './order.mapper';

export class CreateOrderMapper {
  static toDto(proto: CreateOrderResponse): CreateOrderRespDto {
    return {
      order: OrderMapper.toDto(proto.order),
      paymentUrl: proto.paymentUrl,
    };
  }
}
