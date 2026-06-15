import { Event, EventStatus } from '@prisma/client';
import { EventCategoryEntity } from './event-category.entity';
import { EventConfigEntity } from './event-config.entity';
import { EventLocationEntity } from './event-location.entity';
import { EventOrganizerEntity } from './event-org.entity';
import { EventRoleEntity } from './event-role.entity';

export class EventEntity implements Event {
  id: string;
  name: string;
  description: string;
  startDate: Date;
  endDate: Date;
  thumbnailUrl: string;
  categories: EventCategoryEntity[];
  status: EventStatus;

  location?: EventLocationEntity;
  config?: EventConfigEntity;
  organizer?: EventOrganizerEntity;
  roles?: EventRoleEntity[];

  createdAt: Date;
  updatedAt: Date;
}
