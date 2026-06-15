import { Order } from '@/protogen/order.pb';
import { OrderRespDto } from '../dtos/resp';
import { OrderStatusMapper } from './order-status.mapper';

export class OrderMapper {
  static toDto(proto: Order): OrderRespDto {
    return {
      id: proto.id,
      code: proto.code,
      eventId: proto.eventId,
      userId: proto.userId,
      userFullname: proto.userFullname,
      userEmail: proto.userEmail,
      userPhone: proto.userPhone,
      totalAmountCents: Number(proto.totalAmountCents),
      currency: proto.currency,
      status: OrderStatusMapper.toEnum(proto.status),
      paymentMethod: proto.paymentMethod,
      items: proto.items
        ? proto.items.map((item) => ({
            ticketClassId: item.ticketClassId,
            quantity: item.quantity,
            priceCents: Number(item.priceCents),
          }))
        : [],
      createdAt: new Date(proto.createdAt),
      updatedAt: new Date(proto.updatedAt),
    };
  }
}
