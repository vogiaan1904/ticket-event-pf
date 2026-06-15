import {
  EventConfigEntity,
  EventEntity,
  EventLocationEntity,
  EventOrganizerEntity,
  EventRoleEntity,
} from '@/modules/events/entities';
import {
  Event as ProtoEvent,
  EventConfig as ProtoEventConfig,
  EventLocation as ProtoEventLocation,
  EventOrganizer as ProtoEventOrganizer,
  EventRole as ProtoEventRole,
} from '@/protogen/event.pb';
import { EventRoleTypeMapper } from './event-role.mapper';
import { EventStatusMapper } from './event-status.mapper';

export class EventResponseMapper {
  static toProtoEvent(entity: EventEntity): ProtoEvent {
    return {
      id: entity.id,
      name: entity.name,
      description: entity.description,
      startDate: entity.startDate.toISOString(),
      endDate: entity.endDate.toISOString(),
      thumbnailUrl: entity.thumbnailUrl,
      status: EventStatusMapper.toProto(entity.status),
      location: entity.location ? this.toProtoEventLocation(entity.location) : undefined,
      config: undefined,
      organizer: entity.organizer ? this.toProtoEventOrganizer(entity.organizer) : undefined,
      categories: entity.categories.map((category) => ({
        id: category.id,
        name: category.name,
      })),
      roles: entity.roles ? entity.roles.map((role) => this.toProtoEventRole(role)) : [],
      createdAt: entity.createdAt.toISOString(),
      updatedAt: entity.updatedAt.toISOString(),
    };
  }

  static toProtoEventLocation(entity: EventLocationEntity): ProtoEventLocation {
    const address = [entity.street, entity.ward, entity.district, entity.city, entity.country]
      .filter(Boolean)
      .join(', ');

    return {
      id: entity.id,
      venue: entity.venue,
      address: address,
    };
  }

  static toProtoEventConfig(entity: EventConfigEntity): ProtoEventConfig {
    return {
      id: entity.id,
      ticketSaleStartDate: entity.ticketSaleStartDate.toISOString(),
      ticketSaleEndDate: entity.ticketSaleEndDate.toISOString(),
      isFree: entity.isFree,
      maxAttendees: entity.maxAttendees,
      isPublic: entity.isPublic,
      requiresApproval: entity.requiresApproval,
      allowWaitRoom: entity.allowWaitRoom,
      isNewTrending: entity.isNewTrending,
    };
  }

  static toProtoEventOrganizer(entity: EventOrganizerEntity): ProtoEventOrganizer {
    return {
      id: entity.id,
      name: entity.name,
      description: entity.description,
      logoUrl: entity.logoUrl || '',
    };
  }

  static toProtoEventRole(entity: EventRoleEntity): ProtoEventRole {
    return {
      id: entity.id,
      userId: entity.userId,
      eventId: entity.eventId,
      role: EventRoleTypeMapper.toProto(entity.role),
    };
  }
}
