import { IsNotEmpty, IsString } from 'class-validator';

export class LeaveQueueDto {
  @IsNotEmpty()
  @IsString()
  sessionId: string;
}
