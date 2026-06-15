import { EventConfig } from '@/protogen/event.pb';
import { EventConfigRespDto } from '../dtos/resp';

export class ConfigMapper {
  static toDto(config: EventConfig): EventConfigRespDto {
    return {
      id: config.id,
      ticketSaleStartDate: new Date(config.ticketSaleStartDate),
      ticketSaleEndDate: new Date(config.ticketSaleEndDate),
      isFree: config.isFree,
      maxAttendees: config.maxAttendees,
      isPublic: config.isPublic,
      requiresApproval: config.requiresApproval,
      allowWaitRoom: config.allowWaitRoom,
      isNewTrending: config.isNewTrending,
    };
  }
}
