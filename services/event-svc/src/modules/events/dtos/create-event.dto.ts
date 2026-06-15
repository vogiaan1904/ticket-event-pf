export class CreateEventDto {
  createdBy: string;
  name: string;
  description: string;
  startDate: Date;
  endDate: Date;
  thumbnailUrl: string;
  venue: string;
  street: string;
  city: string;
  country: string;
  ward?: string;
  district?: string;
  categoryIds: string[];
  organizerName: string;
  organizerDescription: string;
  organizerLogoUrl: string;
}
