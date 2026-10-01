import { Inject, Injectable } from '@nestjs/common';
import { ClientGrpc } from '@nestjs/microservices';
import { firstValueFrom } from 'rxjs';
import {
  CreateOrderResponse,
  GetManyOrdersResponse,
  Order,
  ORDER_SERVICE_NAME,
  OrderServiceClient,
} from '@/protogen/order.pb';
import { RequestUser } from '@/shared/types/request-user.type';
import { CreateOrderDto, FilterOrderDto, PaginationDto } from './dtos/req';
import { OrderStatusMapper } from './mappers';

@Injectable()
export class OrdersService {
  private orderService: OrderServiceClient;

  constructor(@Inject(ORDER_SERVICE_NAME) private orderServiceClient: ClientGrpc) {}

  public onModuleInit(): void {
    this.orderService = this.orderServiceClient.getService<OrderServiceClient>(ORDER_SERVICE_NAME);
  }

  async create(user: RequestUser, dto: CreateOrderDto): Promise<CreateOrderResponse> {
    const createResp = await firstValueFrom(
      this.orderService.createOrder({
        ...dto,
        userId: user.id,
        currency: dto.currency || 'VND',
      }),
    );

    return createResp;
  }

  /** order-svc answers 404 when the order is not `user`'s. */
  async findByCode(user: RequestUser, code: string): Promise<Order> {
    const getOrderResp = await firstValueFrom(
      this.orderService.getOrder({ code, userId: user.id }),
    );

    return getOrderResp.order;
  }

  async findMany(
    user: RequestUser,
    pagination: PaginationDto,
    filter: FilterOrderDto,
  ): Promise<GetManyOrdersResponse> {
    const getManyResp = await firstValueFrom(
      this.orderService.getManyOrders({
        cursor: pagination.cursor ?? '',
        pageSize: pagination.limit,
        filter: {
          userId: user.id,
          eventId: filter.eventId,
          status: filter.status ? OrderStatusMapper.toProto(filter.status) : undefined,
        },
      }),
    );

    return getManyResp;
  }

  /** order-svc answers 404 when the order is not `user`'s, 409 once it is paid. */
  async cancel(user: RequestUser, code: string): Promise<void> {
    await firstValueFrom(this.orderService.cancelOrder({ code, userId: user.id }));
  }
}
