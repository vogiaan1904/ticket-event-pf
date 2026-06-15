export class EventLocationEntity {
  id: string;
  eventId: string;
  venue: string;
  street: string;
  city: string;
  district: string | null;
  ward: string | null;
  country: string;
}
