import { EventRoleType } from '@prisma/client';

export class EventRoleEntity {
  id: string;
  userId: string;
  eventId: string;
  role: EventRoleType;
}
