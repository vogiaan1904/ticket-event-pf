import { EventStatus } from '../../enums';
import { EventConfigRespDto } from './config.resp.dto';

export class EventLocationRespDto {
  id: string;
  venue: string;
  address: string;
}

export class EventCategoryRespDto {
  id: string;
  name: string;
}

export class EventOrganizerRespDto {
  id: string;
  name: string;
  description: string;
  logoUrl: string;
}

export class EventRespDto {
  id: string;
  name: string;
  description: string;
  startDate: Date;
  endDate: Date;
  thumbnailUrl: string;
  categories: EventCategoryRespDto[];
  status: EventStatus;

  location?: EventLocationRespDto;
  config?: EventConfigRespDto;
  organizer?: EventOrganizerRespDto;

  createdAt: Date;
  updatedAt: Date;
}
