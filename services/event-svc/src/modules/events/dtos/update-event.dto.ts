import { EventStatus } from '@prisma/client';

export class UpdateEventDto {
  name?: string;
  description?: string;
  startDate?: Date;
  endDate?: Date;
  status?: EventStatus;
  thumbnailUrl?: string;
  venue?: string;
  street?: string;
  city?: string;
  country?: string;
  ward?: string;
  district?: string;
  categoryIds?: string[];
  organizerName?: string;
  organizerDescription?: string;
  organizerLogoUrl?: string;
}
