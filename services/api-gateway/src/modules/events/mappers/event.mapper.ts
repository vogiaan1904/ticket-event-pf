import { Event } from '@/protogen/event.pb';
import { EventRespDto } from '../dtos/resp';
import { StatusMapper } from './status.mapper';

export class EventMapper {
  static toDto(event: Event): EventRespDto {
    return {
      id: event.id,
      name: event.name,
      description: event.description,
      thumbnailUrl: event.thumbnailUrl,
      status: StatusMapper.toEnum(event.status),
      startDate: new Date(event.startDate),
      endDate: new Date(event.endDate),
      categories: event.categories.map((category) => ({
        id: category.id,
        name: category.name,
      })),
      location: event.location
        ? {
            id: event.location.id,
            venue: event.location.venue,
            address: event.location.address,
          }
        : undefined,
      config: event.config
        ? {
            id: event.config.id,
            ticketSaleStartDate: new Date(event.config.ticketSaleStartDate),
            ticketSaleEndDate: new Date(event.config.ticketSaleEndDate),
            isFree: event.config.isFree,
            maxAttendees: event.config.maxAttendees,
            isPublic: event.config.isPublic,
            requiresApproval: event.config.requiresApproval,
            allowWaitRoom: event.config.allowWaitRoom,
            isNewTrending: event.config.isNewTrending,
          }
        : undefined,
      organizer: event.organizer
        ? {
            id: event.organizer.id,
            name: event.organizer.name,
            description: event.organizer.description,
            logoUrl: event.organizer.logoUrl,
          }
        : undefined,
      createdAt: new Date(event.createdAt),
      updatedAt: new Date(event.updatedAt),
    };
  }
}
