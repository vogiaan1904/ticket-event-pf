import {
  Event,
  EVENT_SERVICE_NAME,
  EventConfig,
  EventServiceClient,
  FindManyEventResponse,
} from '@/protogen/event.pb';
import { RequestUser } from '@/shared/types/request-user.type';
import { Inject, Injectable } from '@nestjs/common';
import { ClientGrpc } from '@nestjs/microservices';
import { firstValueFrom } from 'rxjs';
import { CreateConfigDto, CreateEventDto, PaginationDto, UpdateConfigDto } from './dtos/req';
import { FilterEventDto } from './dtos/req/filter-event.dto';
import { StatusMapper } from './mappers';
import { UpdateEventDto } from './dtos/req/update-event.dto';

@Injectable()
export class EventsService {
  private eventService: EventServiceClient;
  constructor(@Inject(EVENT_SERVICE_NAME) private eventServiceClient: ClientGrpc) {}

  public onModuleInit(): void {
    this.eventService = this.eventServiceClient.getService<EventServiceClient>(EVENT_SERVICE_NAME);
  }

  async create(user: RequestUser, dto: CreateEventDto): Promise<Event> {
    const createResp = await firstValueFrom(
      this.eventService.create({
        ...dto,
        creatorUserId: user.id,
      }),
    );

    return createResp.event;
  }

  async update(user: RequestUser, id: string, dto: UpdateEventDto): Promise<Event> {
    const updateResp = await firstValueFrom(
      this.eventService.update({
        ...dto,
        categoryIds: dto.categoryIds ? dto.categoryIds : [],
        id,
        userId: user.id,
      }),
    );
    return updateResp.event;
  }

  async findById(id: string): Promise<Event> {
    const findByIdResp = await firstValueFrom(this.eventService.findOne({ id }));
    return findByIdResp.event;
  }

  async findMany(
    filter: FilterEventDto,
    pagination: PaginationDto,
  ): Promise<FindManyEventResponse> {
    const findManyResp = await firstValueFrom(
      this.eventService.findMany({
        page: pagination.page,
        pageSize: pagination.limit,
        filter: {
          ...filter,
          categoryIds: filter.categoryIds ? filter.categoryIds : [],
          status: filter.status ? StatusMapper.toProto(filter.status) : undefined,
        },
      }),
    );

    return findManyResp;
  }

  // ********************** Event Config ********************** //
  async createConfig(user: RequestUser, id: string, dto: CreateConfigDto): Promise<EventConfig> {
    const createConfigResp = await firstValueFrom(
      this.eventService.createConfig({
        ...dto,
        eventId: id,
        userId: user.id,
      }),
    );

    return createConfigResp.eventConfig;
  }

  async updateConfig(user: RequestUser, id: string, dto: UpdateConfigDto): Promise<EventConfig> {
    const updateConfigResp = await firstValueFrom(
      this.eventService.updateConfig({ ...dto, eventId: id, userId: user.id }),
    );

    return updateConfigResp.eventConfig;
  }

  async findConfigByEventId(user: RequestUser, eventId: string): Promise<EventConfig> {
    const getConfigResp = await firstValueFrom(
      this.eventService.getConfig({ eventId, userId: user.id }),
    );

    return getConfigResp.eventConfig;
  }
}
