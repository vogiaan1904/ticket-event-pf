import { IsNotEmpty, IsString } from 'class-validator';

export class JoinQueueDto {
  @IsNotEmpty()
  @IsString()
  eventId: string;
}
