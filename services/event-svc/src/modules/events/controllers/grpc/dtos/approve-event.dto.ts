import { ApproveEventRequest } from '@/protogen/event.pb';
import { IsNotEmpty, IsString } from 'class-validator';

export class ApproveEventDto implements ApproveEventRequest {
  @IsNotEmpty()
  @IsString()
  eventId: string;

  @IsNotEmpty()
  @IsString()
  userId: string;
}
