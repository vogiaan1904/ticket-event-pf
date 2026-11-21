import { EventRoleType, EventStatus } from '@prisma/client';

export interface CreateEventDto {
  createdBy: string;
  name: string;
  description: string;
  startDate: Date;
  endDate: Date;
  thumbnailUrl: string;
  venue: string;
  street: string;
  city: string;
  country: string;
  ward?: string;
  district?: string;
  categoryIds: string[];
  organizerName: string;
  organizerDescription: string;
  organizerLogoUrl: string;
}

export interface UpdateEventDto {
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

export interface FilterEventDto {
  searchQuery?: string;
  categoryIds: string[];
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

export interface FindManyEventDto {
  page: number;
  pageSize: number;
  filter: FilterEventDto;
}

export interface CreateCategoryDto {
  name: string;
}

export interface CreateEventRoleDto {
  userId: string;
  eventId: string;
  role: EventRoleType;
}

export interface CreateConfigDto {
  eventId: string;
  ticketSaleStartDate: Date;
  ticketSaleEndDate: Date;
  isFree: boolean;
  maxAttendees: number;
  // Unset: the column default on create, the stored value on update.
  maxTicketsPerOrder?: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;
}

export interface UpdateConfigDto {
  eventId: string;
  ticketSaleStartDate: Date;
  ticketSaleEndDate: Date;
  isFree: boolean;
  maxAttendees: number;
  // Unset: the column default on create, the stored value on update.
  maxTicketsPerOrder?: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;
}
