import { GetManyOrdersResponse, OrderStatus as ProtoOrderStatus } from '@/protogen/order.pb';
import { OrderMapper } from './order.mapper';

describe('OrderMapper.toListDto', () => {
  it('hands the client the cursor for the next page', () => {
    const resp: GetManyOrdersResponse = {
      orders: [],
      pagination: { nextCursor: 'abc', pageSize: 20, count: 0, hasNext: true },
    };
    expect(OrderMapper.toListDto(resp).meta).toEqual({
      perPage: 20,
      count: 0,
      nextCursor: 'abc',
      hasNext: true,
    });
  });

  it('carries no id: an order is known by its code', () => {
    const dto = OrderMapper.toDto({
      code: 'TB-1',
      eventId: 'e1',
      userId: 'u1',
      userFullname: '',
      userEmail: '',
      userPhone: '',
      totalAmountCents: 1000,
      currency: 'VND',
      status: ProtoOrderStatus.ORDER_STATUS_EXPIRED,
      paymentMethod: 'zalopay',
      items: [],
      createdAt: '2026-10-01T00:00:00Z',
      updatedAt: '2026-10-01T00:00:00Z',
    });
    expect(dto).not.toHaveProperty('id');
    expect(dto.status).toBe('EXPIRED');
  });
});
