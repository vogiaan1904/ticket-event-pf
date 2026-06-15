import {
  Event,
  EventConfig,
  EventLocation,
  EventOrganizer,
  EventRole,
  EventRoleType,
  EventStatus,
} from '@/protogen/event.pb';

import { Type } from 'class-transformer';

class LocationDto implements EventLocation {
  id: string;
  venue: string;
  address: string;

  createdAt: string;
  updatedAt: string;
}

class ConfigDto implements EventConfig {
  id: string;
  ticketSaleStartDate: string;
  ticketSaleEndDate: string;
  isFree: boolean;
  maxAttendees: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;

  updatedAt: string;
  createdAt: string;
}

class OrganizerDto implements EventOrganizer {
  id: string;
  name: string;
  description: string;
  logoUrl: string;

  createdAt: string;
  updatedAt: string;
}

class EventRoleDto implements EventRole {
  id: string;
  userId: string;
  eventId: string;
  role: EventRoleType;
  createdAt: string;
  updatedAt: string;
}

class EventCategoryDto {
  id: string;
  name: string;
}

export class EventDto implements Event {
  id: string;
  name: string;
  description: string;
  startDate: string;
  endDate: string;
  thumbnailUrl: string;
  status: EventStatus;

  @Type(() => EventCategoryDto)
  categories: EventCategoryDto[];

  @Type(() => LocationDto)
  location: LocationDto;

  @Type(() => ConfigDto)
  config: ConfigDto;

  @Type(() => OrganizerDto)
  organizer: OrganizerDto;

  @Type(() => EventRoleDto)
  roles: EventRoleDto[];

  createdAt: string;
  updatedAt: string;
}
