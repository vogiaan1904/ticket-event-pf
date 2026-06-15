import { UpdateEventRequest } from '@/protogen/event.pb';
import { UpdateEventDto as ServiceUpdateEventDto } from '../../../dtos';
import { IsArray, IsNotEmpty, IsOptional, IsString, IsUrl } from 'class-validator';
import { IsISODateString } from '@/common/decorators/is-date-string.decorator';

export class UpdateEventDto implements UpdateEventRequest {
  toServiceDto(): ServiceUpdateEventDto {
    return {
      name: this.name,
      description: this.description,
      startDate: this.startDate ? new Date(this.startDate) : undefined,
      endDate: this.endDate ? new Date(this.endDate) : undefined,
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
  id: string;

  @IsNotEmpty()
  @IsString()
  userId: string;

  @IsOptional()
  @IsString()
  name?: string;

  @IsOptional()
  @IsString()
  description?: string;

  @IsOptional()
  @IsISODateString()
  startDate?: string;

  @IsOptional()
  @IsISODateString()
  endDate?: string;

  @IsOptional()
  @IsUrl()
  thumbnailUrl?: string;

  @IsOptional()
  @IsString()
  venue?: string;

  @IsOptional()
  @IsString()
  street?: string;

  @IsOptional()
  @IsString()
  city?: string;

  @IsOptional()
  @IsString()
  country?: string;

  @IsOptional()
  @IsString()
  ward?: string;

  @IsOptional()
  @IsString()
  district?: string;

  @IsOptional()
  @IsArray()
  @IsString({ each: true })
  categoryIds: string[];

  @IsOptional()
  @IsString()
  organizerName?: string;

  @IsOptional()
  @IsString()
  organizerDescription?: string;

  @IsOptional()
  @IsUrl()
  organizerLogoUrl?: string;
}
