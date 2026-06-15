import { OrderStatus as ProtoOrderStatus } from '@/protogen/order.pb';
import { OrderStatus } from '../enums';

export class OrderStatusMapper {
  private static enumToProtoMap = new Map<OrderStatus, ProtoOrderStatus>([
    [OrderStatus.UNSPECIFIED, ProtoOrderStatus.ORDER_STATUS_UNSPECIFIED],
    [OrderStatus.PENDING, ProtoOrderStatus.ORDER_STATUS_PENDING],
    [OrderStatus.COMPLETED, ProtoOrderStatus.ORDER_STATUS_COMPLETED],
    [OrderStatus.CANCELED, ProtoOrderStatus.ORDER_STATUS_CANCELED],
    [OrderStatus.FAILED, ProtoOrderStatus.ORDER_STATUS_FAILED],
  ]);

  private static protoToEnumMap = new Map<ProtoOrderStatus, OrderStatus>([
    [ProtoOrderStatus.ORDER_STATUS_UNSPECIFIED, OrderStatus.UNSPECIFIED],
    [ProtoOrderStatus.ORDER_STATUS_PENDING, OrderStatus.PENDING],
    [ProtoOrderStatus.ORDER_STATUS_COMPLETED, OrderStatus.COMPLETED],
    [ProtoOrderStatus.ORDER_STATUS_CANCELED, OrderStatus.CANCELED],
    [ProtoOrderStatus.ORDER_STATUS_FAILED, OrderStatus.FAILED],
  ]);

  static toProto(status: OrderStatus): ProtoOrderStatus {
    const protoStatus = this.enumToProtoMap.get(status);
    if (!protoStatus) {
      throw new Error(`Unknown OrderStatus: ${status}`);
    }
    return protoStatus;
  }

  static toEnum(protoStatus: ProtoOrderStatus): OrderStatus {
    const status = this.protoToEnumMap.get(protoStatus);
    if (!status) {
      throw new Error(`Unknown ProtoOrderStatus: ${protoStatus}`);
    }
    return status;
  }
}
