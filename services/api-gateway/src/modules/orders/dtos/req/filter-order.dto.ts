import { IsEnum, IsOptional, IsString } from 'class-validator';
import { OrderStatus } from '../../enums';

export class FilterOrderDto {
  @IsOptional()
  @IsString()
  eventId?: string;

  @IsOptional()
  @IsEnum(OrderStatus)
  status?: OrderStatus;
}
