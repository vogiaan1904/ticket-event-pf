import { IPaginationOptions } from '@/shared/interfaces/pagination-input.interface';
import { GetPaginationResponse } from '@/shared/interfaces/pagination-resp.interface';
import { Injectable } from '@nestjs/common';
import { EventRoleType, EventStatus } from '@prisma/client';
import { CreateEventDto, FilterEventDto, UpdateConfigDto } from './dtos';
import { EventConfigEntity, EventEntity } from './entities';
import { EventsRepository } from './repository/events.repository';
import { UpdateEventDto } from './dtos/update-event.dto';
import { RpcBusinessException } from '@/common/exceptions/rpc-business.exception';
import { ErrorCodeEnum } from '@/shared/constants/error-code.constant';
import { CreateConfigDto } from './dtos/create-config.dto';

@Injectable()
export class EventsService {
  constructor(private readonly repository: EventsRepository) {}

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
    const canUpdate = roles.some(
      (role) =>
        role.userId === userId &&
        (role.role === EventRoleType.ADMIN || role.role === EventRoleType.EDITOR),
    );
    if (!canUpdate) {
      throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
    }

    return this.repository.update(id, dto);
  }

  findById(id: string): Promise<EventEntity> {
    const event = this.repository.findById(id);
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

    const isAdminOrEditor = event.roles.some(
      (role) =>
        role.userId === userId &&
        (role.role === EventRoleType.ADMIN || role.role === EventRoleType.EDITOR),
    );
    if (!isAdminOrEditor) {
      throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
    }

    const config = await this.repository.createConfig(dto);
    await this.repository.update(dto.eventId, { status: EventStatus.CONFIGURED });

    return config;
  }

  async updateConfig(id: string, userId: string, dto: UpdateConfigDto): Promise<EventConfigEntity> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    const isAdminOrEditor = event.roles.some(
      (role) =>
        role.userId === userId &&
        (role.role === EventRoleType.ADMIN || role.role === EventRoleType.EDITOR),
    );
    if (!isAdminOrEditor) {
      throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
    }

    const config = await this.repository.updateConfig(id, dto);
    return config;
  }

  async findConfigByEventId(eventId: string, userId?: string): Promise<EventConfigEntity> {
    const event = await this.repository.findById(eventId);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    if (userId) {
      const isAdminOrEditor = event.roles.some(
        (role) =>
          role.userId === userId &&
          (role.role === EventRoleType.ADMIN || role.role === EventRoleType.EDITOR),
      );
      if (!isAdminOrEditor) {
        throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
      }
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

    await this.repository.update(id, { status: EventStatus.APPROVED });
  }

  async publishEvent(id: string, userId: string): Promise<void> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    const isAdminOrEditor = event.roles.some(
      (role) =>
        role.userId === userId &&
        (role.role === EventRoleType.ADMIN || role.role === EventRoleType.EDITOR),
    );
    if (!isAdminOrEditor) {
      throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
    }

    await this.repository.update(id, { status: EventStatus.PUBLISHED });

    // TODO: Send notification to users, admins and editors
    // TODO: trigger the ticket service
  }
}
