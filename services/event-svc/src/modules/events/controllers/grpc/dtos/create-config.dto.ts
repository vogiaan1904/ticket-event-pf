import { IsISODateString } from '@/common/decorators/is-date-string.decorator';
import { CreateEventConfigRequest } from '@/protogen/event.pb';
import { IsBoolean, IsInt, IsNotEmpty, IsNumber, IsOptional, IsString, Max, Min } from 'class-validator';
import { CreateConfigDto as ServiceCreateConfigDto } from '../../../dtos';

export class CreateConfigDto implements CreateEventConfigRequest {
  toServiceDto(): ServiceCreateConfigDto {
    return {
      eventId: this.eventId,
      ticketSaleStartDate: new Date(this.ticketSaleStartDate),
      ticketSaleEndDate: new Date(this.ticketSaleEndDate),
      isFree: this.isFree,
      maxAttendees: this.maxAttendees,
      // 0 is proto3's unset.
      maxTicketsPerOrder: this.maxTicketsPerOrder || undefined,
      isPublic: this.isPublic,
      requiresApproval: this.requiresApproval,
      allowWaitRoom: this.allowWaitRoom,
      isNewTrending: this.isNewTrending,
    };
  }

  @IsNotEmpty()
  @IsString()
  userId: string;

  @IsNotEmpty()
  @IsString()
  eventId: string;

  @IsNotEmpty()
  @IsISODateString()
  ticketSaleStartDate: string;

  @IsNotEmpty()
  @IsISODateString()
  ticketSaleEndDate: string;

  @IsNotEmpty()
  @IsBoolean()
  isFree: boolean;

  @IsNotEmpty()
  @IsNumber()
  maxAttendees: number;

  @IsOptional()
  @IsInt()
  @Min(0)
  @Max(10)
  maxTicketsPerOrder: number;

  @IsNotEmpty()
  @IsBoolean()
  isPublic: boolean;

  @IsNotEmpty()
  @IsBoolean()
  requiresApproval: boolean;

  @IsNotEmpty()
  @IsBoolean()
  allowWaitRoom: boolean;

  @IsNotEmpty()
  @IsBoolean()
  isNewTrending: boolean;
}
