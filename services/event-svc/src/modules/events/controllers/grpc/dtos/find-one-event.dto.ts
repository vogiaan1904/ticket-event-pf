import { FindOneEventRequest } from '@/protogen/event.pb';
import { IsNotEmpty, IsString } from 'class-validator';

export class FindOneEventDto implements FindOneEventRequest {
  @IsNotEmpty()
  @IsString()
  id: string;
}
