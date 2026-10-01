import { OrderStatus as ProtoOrderStatus } from '@/protogen/order.pb';
import { OrderStatus } from '../enums';

const PAIRS: ReadonlyArray<[OrderStatus, ProtoOrderStatus]> = [
  [OrderStatus.UNSPECIFIED, ProtoOrderStatus.ORDER_STATUS_UNSPECIFIED],
  [OrderStatus.PENDING, ProtoOrderStatus.ORDER_STATUS_PENDING],
  [OrderStatus.COMPLETED, ProtoOrderStatus.ORDER_STATUS_COMPLETED],
  [OrderStatus.CANCELED, ProtoOrderStatus.ORDER_STATUS_CANCELED],
  [OrderStatus.FAILED, ProtoOrderStatus.ORDER_STATUS_FAILED],
  [OrderStatus.EXPIRED, ProtoOrderStatus.ORDER_STATUS_EXPIRED],
  [OrderStatus.REFUND_REQUIRED, ProtoOrderStatus.ORDER_STATUS_REFUND_REQUIRED],
  [OrderStatus.REFUNDED, ProtoOrderStatus.ORDER_STATUS_REFUNDED],
];

export class OrderStatusMapper {
  private static enumToProtoMap = new Map<OrderStatus, ProtoOrderStatus>(PAIRS);
  private static protoToEnumMap = new Map<ProtoOrderStatus, OrderStatus>(
    PAIRS.map(([e, p]) => [p, e]),
  );

  static toProto(status: OrderStatus): ProtoOrderStatus {
    const protoStatus = this.enumToProtoMap.get(status);
    if (protoStatus === undefined) {
      throw new Error(`Unknown OrderStatus: ${status}`);
    }
    return protoStatus;
  }

  /** A status newer than this gateway reads as UNSPECIFIED: a rollout, not a bug. */
  static toEnum(protoStatus: ProtoOrderStatus): OrderStatus {
    return this.protoToEnumMap.get(protoStatus) ?? OrderStatus.UNSPECIFIED;
  }
}
