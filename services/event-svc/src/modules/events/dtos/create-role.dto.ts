import { EventRoleType } from '@prisma/client';

export class CreateEventRoleDto {
  userId: string;
  eventId: string;
  role: EventRoleType;
}
