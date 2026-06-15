import { IsISODateString } from '@/common/decorators/is-date-string.decorator';
import { CreateEventRequest } from '@/protogen/event.pb';
import { IsArray, IsNotEmpty, IsOptional, IsString, IsUrl } from 'class-validator';
import { CreateEventDto as ServiceCreateEventDto } from '../../../dtos';

export class CreateEventDto implements CreateEventRequest {
  toServiceDto(): ServiceCreateEventDto {
    return {
      createdBy: this.creatorUserId,
      name: this.name,
      description: this.description,
      startDate: new Date(this.startDate),
      endDate: new Date(this.endDate),
      thumbnailUrl: this.thumbnailUrl,
      venue: this.venue,
      street: this.street,
      city: this.city,
      country: this.country,
      ward: this.ward,
      district: this.district,
      categoryIds: this.categoryIds,
      organizerName: this.organizerName,
      organizerDescription: this.organizerDescription,
      organizerLogoUrl: this.organizerLogoUrl,
    };
  }

  @IsNotEmpty()
  @IsString()
  creatorUserId: string;

  @IsNotEmpty()
  @IsString()
  name: string;

  @IsNotEmpty()
  @IsString()
  description: string;

  @IsNotEmpty()
  @IsISODateString()
  startDate: string;

  @IsNotEmpty()
  @IsISODateString()
  endDate: string;

  @IsNotEmpty()
  @IsUrl()
  thumbnailUrl: string;

  @IsNotEmpty()
  @IsString()
  venue: string;

  @IsNotEmpty()
  @IsString()
  street: string;

  @IsNotEmpty()
  @IsString()
  city: string;

  @IsNotEmpty()
  @IsString()
  country: string;

  @IsOptional()
  @IsString()
  ward?: string;

  @IsOptional()
  @IsString()
  district?: string;

  @IsNotEmpty()
  @IsArray()
  @IsString({ each: true })
  categoryIds: string[];

  @IsNotEmpty()
  @IsString()
  organizerName: string;

  @IsNotEmpty()
  @IsString()
  organizerDescription: string;

  @IsNotEmpty()
  @IsUrl()
  organizerLogoUrl: string;
}

export class CreateEventConfigDto {
  startSellDate: Date;
  endSellDate: Date;
  isFree: boolean;
  customerTicketLimit: number;
  categories: string[];
}
