import { AccessGuard } from '@/common/guards/access.guard';
import { ResponseDto } from '@/common/interceptors/transfrom.interceptor';
import { RequestWithUser } from '@/shared/types/request-user.type';
import { Body, Controller, Delete, Get, Param, Post, Query, Req, UseGuards } from '@nestjs/common';
import { CreateOrderDto, FilterOrderDto, PaginationDto } from './dtos/req';
import { CreateOrderRespDto, GetManyOrdersRespDto, OrderRespDto } from './dtos/resp';
import { CreateOrderMapper, OrderMapper } from './mappers';
import { OrdersService } from './orders.service';

@Controller('orders')
export class OrdersController {
  constructor(private readonly ordersService: OrdersService) {}

  @Post()
  @UseGuards(AccessGuard)
  @ResponseDto(CreateOrderRespDto)
  async create(
    @Req() req: RequestWithUser,
    @Body() dto: CreateOrderDto,
  ): Promise<CreateOrderRespDto> {
    const protoResponse = await this.ordersService.create(req.user, dto);
    return CreateOrderMapper.toDto(protoResponse);
  }

  @Get(':id')
  @UseGuards(AccessGuard)
  @ResponseDto(OrderRespDto)
  async findById(@Param('id') id: string): Promise<OrderRespDto> {
    const protoOrder = await this.ordersService.findById(id);
    return OrderMapper.toDto(protoOrder);
  }

  @Get('code/:code')
  @UseGuards(AccessGuard)
  @ResponseDto(OrderRespDto)
  async findByCode(@Param('code') code: string): Promise<OrderRespDto> {
    const protoOrder = await this.ordersService.findByCode(code);
    return OrderMapper.toDto(protoOrder);
  }

  @Get()
  @UseGuards(AccessGuard)
  @ResponseDto(GetManyOrdersRespDto)
  async findMany(
    @Req() req: RequestWithUser,
    @Query() pagination: PaginationDto,
    @Query() filter: FilterOrderDto,
  ): Promise<GetManyOrdersRespDto> {
    const resp = await this.ordersService.findMany(req.user, pagination, filter);
    return {
      data: resp.orders?.map(OrderMapper.toDto) || [],
      meta: {
        currentPage: resp.pagination.page,
        perPage: Number(resp.pagination.pageSize),
        total: Number(resp.pagination.total),
        lastPage: resp.pagination.lastPage,
        hasNext: resp.pagination.hasNext,
        hasPrevious: resp.pagination.hasPrevious,
      },
    };
  }

  @Delete(':id')
  @UseGuards(AccessGuard)
  async cancel(@Req() req: RequestWithUser, @Param('id') id: string): Promise<void> {
    await this.ordersService.cancel(req.user, id);
  }
}
