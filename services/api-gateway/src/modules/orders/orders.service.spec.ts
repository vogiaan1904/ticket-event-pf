import { RequestUser } from '@/shared/types/request-user.type';
import { of } from 'rxjs';
import { OrdersService } from './orders.service';

const buyer = { id: 'u1' } as RequestUser;

function buildService() {
  const sent: Record<string, unknown> = {};
  const client = {
    getOrder: (req: unknown) => ((sent.getOrder = req), of({ order: { code: 'TB-1' } })),
    cancelOrder: (req: unknown) => ((sent.cancelOrder = req), of({})),
    getManyOrders: (req: unknown) => (
      (sent.getManyOrders = req),
      of({ orders: [], pagination: { nextCursor: '', pageSize: 20, count: 0, hasNext: false } })
    ),
  };
  const service = new OrdersService({ getService: () => client } as never);
  service.onModuleInit();
  return { service, sent };
}

describe('OrdersService', () => {
  it('reads an order as the caller, so order-svc can refuse a stranger', async () => {
    const { service, sent } = buildService();
    await service.findByCode(buyer, 'TB-1');
    expect(sent.getOrder).toEqual({ code: 'TB-1', userId: 'u1' });
  });

  it('cancels an order by its code, as the caller', async () => {
    const { service, sent } = buildService();
    await service.cancel(buyer, 'TB-1');
    expect(sent.cancelOrder).toEqual({ code: 'TB-1', userId: 'u1' });
  });

  it("lists the caller's orders from the first page without a cursor", async () => {
    const { service, sent } = buildService();
    await service.findMany(buyer, { limit: 20 }, {});
    expect(sent.getManyOrders).toEqual({
      cursor: '',
      pageSize: 20,
      filter: { userId: 'u1', eventId: undefined, status: undefined },
    });
  });
});
