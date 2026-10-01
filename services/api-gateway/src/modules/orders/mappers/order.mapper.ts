import { GetManyOrdersResponse, Order } from '@/protogen/order.pb';
import { GetManyOrdersRespDto, OrderRespDto } from '../dtos/resp';
import { OrderStatusMapper } from './order-status.mapper';

export class OrderMapper {
  static toDto(proto: Order): OrderRespDto {
    return {
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

  static toListDto(proto: GetManyOrdersResponse): GetManyOrdersRespDto {
    return {
      data: proto.orders?.map((o) => OrderMapper.toDto(o)) ?? [],
      meta: {
        perPage: Number(proto.pagination?.pageSize),
        count: Number(proto.pagination?.count),
        nextCursor: proto.pagination?.nextCursor ?? '',
        hasNext: proto.pagination?.hasNext ?? false,
      },
    };
  }
}
