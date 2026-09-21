import { IPaginationOptions } from '@/shared/interfaces/pagination-input.interface';
import { GetPaginationResponse } from '@/shared/interfaces/pagination-resp.interface';
import { Injectable } from '@nestjs/common';
import { EventRoleType, EventStatus } from '@prisma/client';
import { CreateEventDto, FilterEventDto, UpdateConfigDto } from './dtos';
import { EventConfigEntity, EventEntity, EventRoleEntity } from './entities';
import { EventsRepository } from './repository/events.repository';
import { UpdateEventDto } from './dtos/update-event.dto';
import { RpcBusinessException } from '@/common/exceptions/rpc-business.exception';
import { ErrorCodeEnum } from '@/shared/constants/error-code.constant';
import { CreateConfigDto } from './dtos/create-config.dto';

// Editing an event and configuring it are the same privilege; approving is not.
const CAN_EDIT = [EventRoleType.ADMIN, EventRoleType.EDITOR];

@Injectable()
export class EventsService {
  constructor(private readonly repository: EventsRepository) {}

  // Every gated method asks the same question of the caller. Asking it in one
  // place is what makes a missing gate visible.
  private assertRole(
    roles: EventRoleEntity[] | undefined,
    userId: string,
    allowed: EventRoleType[],
  ): void {
    const held = roles?.some((role) => role.userId === userId && allowed.includes(role.role));
    if (!held) {
      throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
    }
  }

  async create(dto: CreateEventDto): Promise<EventEntity> {
    const event = await this.repository.create(dto);

    await this.repository.createRole({
      userId: dto.createdBy,
      eventId: event.id,
      role: EventRoleType.ADMIN,
    });

    return event;
  }

  async update(id: string, userId: string, dto: UpdateEventDto): Promise<EventEntity> {
    const roles = await this.repository.findEventRoles(id);
    this.assertRole(roles, userId, CAN_EDIT);

    return this.repository.update(id, dto);
  }

  async findById(id: string): Promise<EventEntity> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    return event;
  }

  list(dto: FilterEventDto): Promise<EventEntity[]> {
    return this.repository.list(dto);
  }

  findMany({
    filter,
    pagination,
  }: {
    filter?: FilterEventDto;
    pagination: IPaginationOptions;
  }): Promise<GetPaginationResponse<EventEntity>> {
    return this.repository.findMany({
      filter,
      pagination,
    });
  }

  delete(id: string) {
    return this.repository.delete(id);
  }

  // ********************* CONFIG ********************* //
  async createConfig(userId: string, dto: CreateConfigDto): Promise<EventConfigEntity> {
    const event = await this.repository.findById(dto.eventId);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    this.assertRole(event.roles, userId, CAN_EDIT);

    const config = await this.repository.createConfig(dto);
    await this.repository.update(dto.eventId, { status: EventStatus.CONFIGURED });

    return config;
  }

  async updateConfig(id: string, userId: string, dto: UpdateConfigDto): Promise<EventConfigEntity> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    this.assertRole(event.roles, userId, CAN_EDIT);

    const config = await this.repository.updateConfig(id, dto);
    return config;
  }

  async findConfigByEventId(eventId: string, userId?: string): Promise<EventConfigEntity> {
    const event = await this.repository.findById(eventId);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    if (userId) {
      this.assertRole(event.roles, userId, CAN_EDIT);
    }

    const config = await this.repository.findConfigByEventId(eventId);
    if (!config) {
      throw new RpcBusinessException(ErrorCodeEnum.EventConfigNotFound);
    }

    return config;
  }

  // ********************* EVENT-SPECIFIC OPERATIONS ********************* //
  async approveEvent(id: string, userId: string): Promise<void> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    // Approving is the gate before publish, so it is not an editor's to open.
    this.assertRole(event.roles, userId, [EventRoleType.ADMIN]);

    await this.repository.update(id, { status: EventStatus.APPROVED });
  }

  async publishEvent(id: string, userId: string): Promise<void> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    this.assertRole(event.roles, userId, CAN_EDIT);

    await this.repository.update(id, { status: EventStatus.PUBLISHED });

    // TODO: Send notification to users, admins and editors
    // TODO: trigger the ticket service
  }
}
