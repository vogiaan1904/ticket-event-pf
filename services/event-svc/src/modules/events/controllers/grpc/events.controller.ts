import {
  CreateEventConfigResponse,
  CreateEventResponse,
  EVENT_SERVICE_NAME,
  FindManyEventResponse,
  FindOneEventResponse,
  GetEventConfigResponse,
  ListEventResponse,
  UpdateEventConfigResponse,
  UpdateEventResponse,
} from '@/protogen/event.pb';
import { LoggerService } from '@/shared/services/logger.service';
import { Controller } from '@nestjs/common';
import { GrpcMethod } from '@nestjs/microservices';
import { EventsService } from '../../events.service';
import {
  ApproveEventDto,
  CreateEventDto,
  FindConfigDto,
  FindManyEventDto,
  FindOneEventDto,
  ListEventDto,
  PublishEventDto,
  UpdateConfigDto,
  UpdateEventDto,
} from './dtos';
import { CreateConfigDto } from './dtos/create-config.dto';
import { EventResponseMapper } from './mappers';

@Controller()
export class GrpcEventsController {
  constructor(
    private readonly eventsService: EventsService,
    private readonly logger: LoggerService,
  ) {
    logger.setContext(GrpcEventsController.name);
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'create')
  async create(dto: CreateEventDto): Promise<CreateEventResponse> {
    this.logger.info(`EventsController.create called.`);
    const event = await this.eventsService.create(dto.toServiceDto());
    this.logger.info(`EventsController.create completed.`);

    return { event: EventResponseMapper.toProtoEvent(event) };
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'update')
  async update(dto: UpdateEventDto): Promise<UpdateEventResponse> {
    this.logger.info(`EventsController.update called.`);
    const event = await this.eventsService.update(dto.id, dto.userId, dto.toServiceDto());
    this.logger.info(`EventsController.update completed.`);

    return { event: EventResponseMapper.toProtoEvent(event) };
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'findOne')
  async findOne(dto: FindOneEventDto): Promise<FindOneEventResponse> {
    this.logger.info(`EventsController.findOne called.`);
    const event = await this.eventsService.findById(dto.id);
    this.logger.info(`EventsController.findOne completed.`);

    return { event: EventResponseMapper.toProtoEvent(event) };
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'findMany')
  async findMany(dto: FindManyEventDto): Promise<FindManyEventResponse> {
    this.logger.info(`EventsController.findMany called.`);
    const out = await this.eventsService.findMany({
      filter: dto.filter.toServiceDto(),
      pagination: { page: dto.page, limit: dto.pageSize },
    });
    this.logger.info(`EventsController.findMany completed.`);

    return {
      events: out.data.map((item) => EventResponseMapper.toProtoEvent(item)),
      pagination: {
        page: out.meta.currentPage,
        pageSize: out.meta.perPage,
        count: out.meta.total,
        lastPage: out.meta.lastPage,
        total: out.meta.total,
        hasNext: out.meta.next === 1,
        hasPrevious: out.meta.prev === 1,
      },
    };
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'list')
  async list(dto: ListEventDto): Promise<ListEventResponse> {
    this.logger.info(`EventsController.list called.`);
    const out = await this.eventsService.list(dto.filter.toServiceDto());
    this.logger.info(`EventsController.list completed.`);

    return { events: out.map((item) => EventResponseMapper.toProtoEvent(item)) };
  }

  // ********************* CONFIG ********************* //
  @GrpcMethod(EVENT_SERVICE_NAME, 'createConfig')
  async createConfig(dto: CreateConfigDto): Promise<CreateEventConfigResponse> {
    const config = await this.eventsService.createConfig(dto.userId, dto.toServiceDto());
    return { eventConfig: EventResponseMapper.toProtoEventConfig(config) };
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'updateConfig')
  async updateConfig(dto: UpdateConfigDto): Promise<UpdateEventConfigResponse> {
    const config = await this.eventsService.updateConfig(
      dto.eventId,
      dto.userId,
      dto.toServiceDto(),
    );
    return { eventConfig: EventResponseMapper.toProtoEventConfig(config) };
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'getConfig')
  async getConfig(dto: FindConfigDto): Promise<GetEventConfigResponse> {
    const config = await this.eventsService.findConfigByEventId(dto.eventId, dto.userId);
    return { eventConfig: EventResponseMapper.toProtoEventConfig(config) };
  }

  // ********************* EVENT-SPECIFIC OPERATIONS ********************* //
  @GrpcMethod(EVENT_SERVICE_NAME, 'publishEvent')
  publishEvent(dto: PublishEventDto) {
    this.eventsService.publishEvent(dto.eventId, dto.userId);
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'approveEvent')
  approveEvent(dto: ApproveEventDto) {
    this.eventsService.approveEvent(dto.eventId, dto.userId);
  }
}
