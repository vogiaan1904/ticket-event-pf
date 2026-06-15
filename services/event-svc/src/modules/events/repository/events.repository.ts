import { PrismaService } from '@/infra/database/prisma/prisma.service';
import { IPaginationOptions } from '@/shared/interfaces/pagination-input.interface';
import { GetPaginationResponse } from '@/shared/interfaces/pagination-resp.interface';
import { createPaginator } from '@/shared/utils/pagination.util';
import { Injectable } from '@nestjs/common';
import { Prisma } from '@prisma/client';
import { CreateCategoryDto, CreateEventDto, FilterEventDto, UpdateConfigDto } from '../dtos';
import { CreateConfigDto } from '../dtos/create-config.dto';
import { CreateEventRoleDto } from '../dtos/create-role.dto';
import { UpdateEventDto } from '../dtos/update-event.dto';
import { EventCategoryEntity, EventConfigEntity, EventEntity, EventRoleEntity } from '../entities';
import { mapToEventEntities, mapToEventEntity, PrismaEventWithRelations } from './events.mapper';

@Injectable()
export class EventsRepository {
  constructor(private readonly prisma: PrismaService) {}

  private readonly paginator = createPaginator(1, 20);

  private readonly baseInclude = {
    organizer: true,
    location: true,
    config: true,
    roles: true,
    categories: {
      include: {
        category: {
          select: {
            id: true,
            name: true,
          },
        },
      },
    },
  };

  private buildWhereFilter(dto: FilterEventDto): Prisma.EventWhereInput {
    const where: Prisma.EventWhereInput = {};
    const conditions: Prisma.EventWhereInput[] = [];
    const configFilters: Prisma.EventConfigWhereInput = {
      isPublic: dto.isPublic,
      isFree: dto.isFree,
    };

    conditions.push({
      config: configFilters,
    });

    if (dto.searchQuery?.trim()) {
      conditions.push({
        OR: [
          {
            name: {
              contains: dto.searchQuery.trim(),
              mode: 'insensitive',
            },
          },
          {
            description: {
              contains: dto.searchQuery.trim(),
              mode: 'insensitive',
            },
          },
          {
            organizer: {
              name: {
                contains: dto.searchQuery.trim(),
                mode: 'insensitive',
              },
            },
          },
        ],
      });
    }

    if (dto.startDateFrom || dto.startDateTo) {
      const dateFilter: Prisma.DateTimeFilter = {};

      if (dto.startDateFrom) {
        dateFilter.gte = dto.startDateFrom;
      }

      if (dto.startDateTo) {
        const endOfDay = new Date(dto.startDateTo);
        endOfDay.setHours(23, 59, 59, 999);
        dateFilter.lte = endOfDay;
      }

      conditions.push({
        startDate: dateFilter,
      });
    }

    if (dto.city?.trim() || dto.country?.trim()) {
      const locationFilter: Prisma.EventLocationWhereInput = {};

      if (dto.city?.trim()) {
        locationFilter.city = {
          contains: dto.city.trim(),
          mode: 'insensitive',
        };
      }

      if (dto.country?.trim()) {
        locationFilter.country = {
          contains: dto.country.trim(),
          mode: 'insensitive',
        };
      }

      conditions.push({
        location: locationFilter,
      });
    }

    if (dto.organizerId?.trim()) {
      conditions.push({
        organizer: {
          id: dto.organizerId.trim(),
        },
      });
    }

    if (dto.userId?.trim()) {
      conditions.push({
        roles: {
          some: {
            userId: dto.userId.trim(),
            // Optionally filter by specific role types
            // role: EventRoleType.ADMIN,
          },
        },
      });
    }

    if (dto.categoryIds?.length > 0) {
      conditions.push({
        categories: {
          some: {
            categoryId: { in: dto.categoryIds },
          },
        },
      });
    }

    if (dto.status) {
      conditions.push({
        status: dto.status,
      });
    }

    where.AND = conditions;

    return where;
  }

  // ********************* EVENTS ********************* //
  async create(dto: CreateEventDto): Promise<EventEntity> {
    const event = await this.prisma.event.create({
      data: {
        name: dto.name,
        description: dto.description,
        startDate: dto.startDate,
        endDate: dto.endDate,
        thumbnailUrl: dto.thumbnailUrl,
        location: {
          create: {
            venue: dto.venue,
            street: dto.street,
            city: dto.city,
            country: dto.country,
            ward: dto.ward,
            district: dto.district,
          },
        },
        organizer: {
          create: {
            name: dto.organizerName,
            description: dto.organizerDescription,
            logoUrl: dto.organizerLogoUrl,
          },
        },
        categories: {
          createMany: {
            data: dto.categoryIds.map((id) => ({
              categoryId: id,
            })),
          },
        },
      },
      include: this.baseInclude,
    });

    // await this.prisma.eventCategory.createMany({
    //   data: dto.categoryIds.map((id) => ({
    //     eventId: event.id,
    //     categoryId: id,
    //   })),
    // });

    return mapToEventEntity(event);
  }

  async update(id: string, dto: UpdateEventDto): Promise<EventEntity> {
    const event = await this.prisma.event.update({
      where: { id },
      data: {
        name: dto.name,
        description: dto.description,
        startDate: dto.startDate,
        endDate: dto.endDate,
        thumbnailUrl: dto.thumbnailUrl,
        status: dto.status,
        location: {
          update: {
            venue: dto.venue,
            street: dto.street,
            city: dto.city,
            country: dto.country,
            ward: dto.ward ?? null,
            district: dto.district ?? null,
          },
        },
        organizer: {
          update: {
            name: dto.organizerName,
            description: dto.organizerDescription,
            logoUrl: dto.organizerLogoUrl,
          },
        },
      },
      include: this.baseInclude,
    });
    return mapToEventEntity(event);
  }

  async findById(id: string): Promise<EventEntity | null> {
    const event = await this.prisma.event.findUnique({ where: { id }, include: this.baseInclude });
    if (!event) {
      return null;
    }

    return mapToEventEntity(event);
  }

  async list(dto: FilterEventDto): Promise<EventEntity[]> {
    const events = await this.prisma.event.findMany({
      where: this.buildWhereFilter(dto),
      include: this.baseInclude,
    });
    return mapToEventEntities(events);
  }

  async findMany({
    filter,
    pagination,
  }: {
    filter?: FilterEventDto;
    pagination: IPaginationOptions;
  }): Promise<GetPaginationResponse<EventEntity>> {
    const out = await this.paginator(
      this.prisma.event,
      {
        where: this.buildWhereFilter(filter),
        include: this.baseInclude,
      },
      {
        page: pagination.page,
        perPage: pagination.limit,
      },
    );
    return {
      data: mapToEventEntities(out.data as PrismaEventWithRelations[]),
      meta: out.meta,
    };
  }

  async delete(id: string): Promise<void> {
    await this.prisma.event.delete({ where: { id } });
  }

  // ********************* EVENT CONFIGURATIONS ********************* //

  async createConfig(dto: CreateConfigDto): Promise<EventConfigEntity> {
    return this.prisma.eventConfig.create({
      data: {
        ...dto,
      },
    });
  }

  async updateConfig(id: string, dto: UpdateConfigDto): Promise<EventConfigEntity> {
    return this.prisma.eventConfig.update({
      where: { id },
      data: dto,
    });
  }

  async findConfigById(id: string): Promise<EventConfigEntity | null> {
    return this.prisma.eventConfig.findUnique({ where: { id } });
  }

  async findConfigByEventId(eventId: string): Promise<EventConfigEntity | null> {
    return this.prisma.eventConfig.findUnique({ where: { eventId } });
  }

  // ********************* CATEGORIES ********************* //
  async createCategory(dto: CreateCategoryDto): Promise<EventCategoryEntity> {
    return this.prisma.category.create({
      data: {
        name: dto.name,
      },
    });
  }

  // ********************* ROLES ********************* //
  async createRole(dto: CreateEventRoleDto): Promise<EventRoleEntity> {
    return this.prisma.eventRole.create({
      data: dto,
    });
  }

  async updateCategory(dto: any): Promise<EventCategoryEntity> {
    return this.prisma.category.update({
      where: { id: dto.id },
      data: { name: dto.name },
    });
  }

  async findEventRoles(eventId: string): Promise<EventRoleEntity[]> {
    return this.prisma.eventRole.findMany({
      where: { eventId },
    });
  }
}
