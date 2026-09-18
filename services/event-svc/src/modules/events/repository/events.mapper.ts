import {
  Event,
  EventCategory,
  EventConfig,
  EventLocation,
  EventRole,
  Organizer,
} from '@prisma/client';
import {
  EventConfigEntity,
  EventEntity,
  EventLocationEntity,
  EventOrganizerEntity,
  EventRoleEntity,
} from '../entities';

export type PrismaEventWithRelations = Event & {
  categories?: (EventCategory & {
    category: {
      id: string;
      name: string;
    };
  })[];
  location?: EventLocation | null;
  config?: EventConfig | null;
  organizer?: Organizer | null;
  roles?: EventRole[] | null;
};

export function mapToEventEntity(prismaEvent: PrismaEventWithRelations): EventEntity {
  const entity = new EventEntity();

  entity.id = prismaEvent.id;
  entity.name = prismaEvent.name;
  entity.description = prismaEvent.description;
  entity.startDate = prismaEvent.startDate;
  entity.endDate = prismaEvent.endDate;
  entity.thumbnailUrl = prismaEvent.thumbnailUrl;
  entity.status = prismaEvent.status;
  entity.createdAt = prismaEvent.createdAt;
  entity.updatedAt = prismaEvent.updatedAt;

  // Flattens the EventCategory junction rows to {id, name}.
  entity.categories =
    prismaEvent.categories?.map((ec) => ({
      id: ec.category.id,
      name: ec.category.name,
    })) || [];

  if (prismaEvent.location) {
    entity.location = mapToEventLocationEntity(prismaEvent.location);
  }

  if (prismaEvent.config) {
    entity.config = mapToEventConfigEntity(prismaEvent.config);
  }

  if (prismaEvent.organizer) {
    entity.organizer = mapToEventOrganizerEntity(prismaEvent.organizer);
  }

  if (prismaEvent.roles) {
    entity.roles = prismaEvent.roles.map((role) => mapToEventRoleEntity(role));
  }

  return entity;
}

function mapToEventLocationEntity(location: EventLocation): EventLocationEntity {
  const entity = new EventLocationEntity();
  entity.id = location.id;
  entity.venue = location.venue;
  entity.street = location.street;
  entity.city = location.city;
  entity.district = location.district;
  entity.ward = location.ward;
  entity.country = location.country;
  return entity;
}

function mapToEventRoleEntity(role: EventRole): EventRoleEntity {
  const entity = new EventRoleEntity();
  entity.id = role.id;
  entity.userId = role.userId;
  entity.eventId = role.eventId;
  entity.role = role.role;
  return entity;
}

function mapToEventConfigEntity(config: EventConfig): EventConfigEntity {
  const entity = new EventConfigEntity();
  entity.id = config.id;
  entity.ticketSaleStartDate = config.ticketSaleStartDate;
  entity.ticketSaleEndDate = config.ticketSaleEndDate;
  entity.isFree = config.isFree;
  entity.maxAttendees = config.maxAttendees;
  entity.isPublic = config.isPublic;
  entity.requiresApproval = config.requiresApproval;
  entity.allowWaitRoom = config.allowWaitRoom;
  entity.isNewTrending = config.isNewTrending;
  return entity;
}

function mapToEventOrganizerEntity(organizer: Organizer): EventOrganizerEntity {
  const entity = new EventOrganizerEntity();
  entity.id = organizer.id;
  entity.name = organizer.name;
  entity.description = organizer.description;
  entity.logoUrl = organizer.logoUrl;
  return entity;
}

export function mapToEventEntities(prismaEvents: PrismaEventWithRelations[]): EventEntity[] {
  return prismaEvents.map(mapToEventEntity);
}
