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

  @Get('code/:code')
  @UseGuards(AccessGuard)
  @ResponseDto(OrderRespDto)
  async findByCode(
    @Req() req: RequestWithUser,
    @Param('code') code: string,
  ): Promise<OrderRespDto> {
    const protoOrder = await this.ordersService.findByCode(req.user, code);
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
    return OrderMapper.toListDto(resp);
  }

  @Delete('code/:code')
  @UseGuards(AccessGuard)
  async cancel(@Req() req: RequestWithUser, @Param('code') code: string): Promise<void> {
    await this.ordersService.cancel(req.user, code);
  }
}
