import { AccessGuard } from '@/common/guards/access.guard';
import { ResponseDto } from '@/common/interceptors/transfrom.interceptor';
import { RequestWithUser } from '@/shared/types/request-user.type';
import { Body, Controller, Get, Param, Post, Put, Query, Req, UseGuards } from '@nestjs/common';
import { CreateConfigDto, CreateEventDto, PaginationDto, UpdateConfigDto } from './dtos/req';
import { FilterEventDto } from './dtos/req/filter-event.dto';
import { UpdateEventDto } from './dtos/req/update-event.dto';
import { FindManyEventRespDto } from './dtos/resp/find-many-event.resp.dto';
import { EventsService } from './events.service';
import { ConfigMapper, EventMapper } from './mappers';
import { EventConfigRespDto, EventRespDto } from './dtos/resp';

@Controller('events')
export class EventsController {
  constructor(private readonly eventsService: EventsService) {}

  @Post()
  @UseGuards(AccessGuard)
  @ResponseDto(EventRespDto)
  async create(@Req() req: RequestWithUser, @Body() dto: CreateEventDto): Promise<EventRespDto> {
    const protoEvent = await this.eventsService.create(req.user, dto);
    return EventMapper.toDto(protoEvent);
  }

  @Put(':id')
  @UseGuards(AccessGuard)
  @ResponseDto(EventRespDto)
  async update(
    @Req() req: RequestWithUser,
    @Param('id') id: string,
    @Body() dto: UpdateEventDto,
  ): Promise<EventRespDto> {
    const protoEvent = await this.eventsService.update(req.user, id, dto);
    return EventMapper.toDto(protoEvent);
  }

  @Get(':id')
  @UseGuards(AccessGuard)
  @ResponseDto(EventRespDto)
  async findById(@Param('id') id: string): Promise<EventRespDto> {
    const protoEvent = await this.eventsService.findById(id);
    return EventMapper.toDto(protoEvent);
  }

  @Get()
  @UseGuards(AccessGuard)
  @ResponseDto(FindManyEventRespDto)
  async findMany(
    @Query() pagination: PaginationDto,
    @Query() filter: FilterEventDto,
  ): Promise<FindManyEventRespDto> {
    const resp = await this.eventsService.findMany(filter, pagination);
    return {
      data: resp.events?.map(EventMapper.toDto) || [],
      meta: {
        currentPage: resp.pagination.page,
        perPage: Number(resp.pagination.pageSize),
        total: Number(resp.pagination.count),
        lastPage: resp.pagination.lastPage,
        hasNext: resp.pagination.hasNext,
        hasPrevious: resp.pagination.hasPrevious,
      },
    };
  }

  // ********************** Event Config ********************** //

  @Post(':id/config')
  @UseGuards(AccessGuard)
  @ResponseDto(EventConfigRespDto)
  async createConfig(
    @Req() req: RequestWithUser,
    @Body() dto: CreateConfigDto,
    @Param('id') id: string,
  ): Promise<EventConfigRespDto> {
    const protoConfig = await this.eventsService.createConfig(req.user, id, dto);
    return ConfigMapper.toDto(protoConfig);
  }

  @Put(':id/config')
  @UseGuards(AccessGuard)
  @ResponseDto(EventConfigRespDto)
  async updateConfig(
    @Req() req: RequestWithUser,
    @Body() dto: UpdateConfigDto,
    @Param('id') id: string,
  ): Promise<EventConfigRespDto> {
    const protoConfig = await this.eventsService.updateConfig(req.user, id, dto);
    return ConfigMapper.toDto(protoConfig);
  }

  @Get(':id/config')
  @UseGuards(AccessGuard)
  @ResponseDto(EventConfigRespDto)
  async findConfigByEventId(
    @Req() req: RequestWithUser,
    @Param('id') id: string,
  ): Promise<EventConfigRespDto> {
    const protoConfig = await this.eventsService.findConfigByEventId(req.user, id);
    return ConfigMapper.toDto(protoConfig);
  }
}
