import { IsNotEmpty, IsNumber, IsString, Min } from 'class-validator';

export class CreateOrderItemDto {
  @IsNotEmpty()
  @IsString()
  ticketClassId: string;

  @IsNotEmpty()
  @IsNumber()
  @Min(1)
  quantity: number;
}
