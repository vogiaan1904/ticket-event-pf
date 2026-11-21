import {
  IsBoolean,
  IsDateString,
  IsInt,
  IsNotEmpty,
  IsNumber,
  IsOptional,
  Max,
  Min,
} from 'class-validator';

export class UpdateConfigDto {
  @IsNotEmpty()
  @IsDateString()
  ticketSaleStartDate: string;

  @IsNotEmpty()
  @IsDateString()
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
