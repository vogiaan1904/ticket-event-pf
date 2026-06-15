import { IsYYYYMMDD } from '@/common/decorators/isYYYYMMDD.decorator';
import { IsBoolean, IsNotEmpty, IsNumber } from 'class-validator';

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
