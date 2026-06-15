import { PublishEventRequest } from '@/protogen/event.pb';
import { IsNotEmpty, IsString } from 'class-validator';

export class PublishEventDto implements PublishEventRequest {
  @IsNotEmpty()
  @IsString()
  eventId: string;

  @IsNotEmpty()
  @IsString()
  userId: string;
}
