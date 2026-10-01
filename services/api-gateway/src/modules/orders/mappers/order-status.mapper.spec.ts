import { OrderStatus as ProtoOrderStatus } from '@/protogen/order.pb';
import { OrderStatus } from '../enums';
import { OrderStatusMapper } from './order-status.mapper';

describe('OrderStatusMapper', () => {
  it('gives every wire status its own value, so a buyer owed money can tell', () => {
    expect(OrderStatusMapper.toEnum(ProtoOrderStatus.ORDER_STATUS_EXPIRED)).toBe(
      OrderStatus.EXPIRED,
    );
    expect(OrderStatusMapper.toEnum(ProtoOrderStatus.ORDER_STATUS_REFUND_REQUIRED)).toBe(
      OrderStatus.REFUND_REQUIRED,
    );
    expect(OrderStatusMapper.toEnum(ProtoOrderStatus.ORDER_STATUS_REFUNDED)).toBe(
      OrderStatus.REFUNDED,
    );
  });

  it('round-trips every status a buyer can filter by', () => {
    for (const status of Object.values(OrderStatus).filter((s) => s !== OrderStatus.UNSPECIFIED)) {
      expect(OrderStatusMapper.toEnum(OrderStatusMapper.toProto(status))).toBe(status);
    }
  });

  // A status added to the contract after this gateway was built is not our bug.
  it('reads a status it does not know as unspecified, not as a server error', () => {
    expect(OrderStatusMapper.toEnum(ProtoOrderStatus.UNRECOGNIZED)).toBe(OrderStatus.UNSPECIFIED);
    expect(OrderStatusMapper.toEnum(99 as ProtoOrderStatus)).toBe(OrderStatus.UNSPECIFIED);
  });
});
