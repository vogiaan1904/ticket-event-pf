export class UpdateConfigDto {
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
