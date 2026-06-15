export class UpdateConfigDto {
  eventId: string;
  ticketSaleStartDate: Date;
  ticketSaleEndDate: Date;
  isFree: boolean;
  maxAttendees: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;
}
