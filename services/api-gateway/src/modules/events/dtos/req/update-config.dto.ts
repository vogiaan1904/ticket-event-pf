import { IsYYYYMMDD } from '@/common/decorators/isYYYYMMDD.decorator';
import { IsBoolean, IsInt, IsNotEmpty, IsNumber, IsOptional, Max, Min } from 'class-validator';

export class UpdateConfigDto {
  @IsNotEmpty()
  @IsYYYYMMDD()
  ticketSaleStartDate: string;

  @IsNotEmpty()
  @IsYYYYMMDD()
  ticketSaleEndDate: string;

  @IsNotEmpty()
  @IsBoolean()
  isFree: boolean;

  @IsNotEmpty()
  @IsNumber()
  maxAttendees: number;

  // Left out: the event's default on create, unchanged on update.
  @IsOptional()
  @IsInt()
  @Min(1)
  @Max(10)
  maxTicketsPerOrder?: number;

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
