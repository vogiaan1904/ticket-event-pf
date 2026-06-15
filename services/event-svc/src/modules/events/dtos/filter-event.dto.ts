import { EventStatus } from '@prisma/client';

export class FilterEventDto {
  searchQuery?: string;
  categoryIds: string[] = [];
  status?: EventStatus;
  organizerId?: string;
  userId?: string;
  startDateFrom?: Date;
  startDateTo?: Date;
  city?: string;
  country?: string;
  isPublic?: boolean;
  isFree?: boolean;
}
