import { Type } from 'class-transformer';
import {
  IsArray,
  IsEmail,
  IsNotEmpty,
  IsOptional,
  IsString,
  ValidateNested,
} from 'class-validator';
import { CreateOrderItemDto } from './create-order-item.dto';

export class CreateOrderDto {
  @IsNotEmpty()
  @IsString()
  eventId: string;

  @IsNotEmpty()
  @IsString()
  userFullname: string;

  @IsNotEmpty()
  @IsEmail()
  userEmail: string;

  @IsNotEmpty()
  @IsString()
  userPhone: string;

  @IsNotEmpty()
  @IsString()
  paymentMethod: string;

  @IsNotEmpty()
  @IsArray()
  @ValidateNested({ each: true })
  @Type(() => CreateOrderItemDto)
  items: CreateOrderItemDto[];

  @IsOptional()
  @IsString()
  currency?: string;

  @IsNotEmpty()
  @IsString()
  checkoutToken: string;

  @IsNotEmpty()
  @IsString()
  redirectUrl: string;
}
