import { GetEventConfigRequest } from '@/protogen/event.pb';
import { IsNotEmpty, IsOptional, IsString } from 'class-validator';

export class FindConfigDto implements GetEventConfigRequest {
  @IsNotEmpty()
  @IsString()
  eventId: string;

  @IsOptional()
  @IsString()
  userId: string;
}
