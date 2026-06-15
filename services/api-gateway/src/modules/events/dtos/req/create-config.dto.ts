import { IsBoolean, IsDateString, IsNotEmpty, IsNumber } from 'class-validator';

export class CreateConfigDto {
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
