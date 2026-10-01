import { plainToInstance } from 'class-transformer';
import { validate } from 'class-validator';
import { EventResponseMapper } from '../mappers/event.mapper';
import { EventConfigEntity } from '../../../entities';
import { CreateConfigDto } from './create-config.dto';
import { UpdateConfigDto } from './update-config.dto';

const base = {
  userId: 'u1',
  eventId: 'e1',
  ticketSaleStartDate: '2026-10-01T00:00:00.000Z',
  ticketSaleEndDate: '2026-10-02T00:00:00.000Z',
  isFree: false,
  maxAttendees: 500,
  isPublic: true,
  requiresApproval: false,
  allowWaitRoom: true,
  isNewTrending: false,
};

describe.each([
  ['CreateConfigDto', CreateConfigDto],
  ['UpdateConfigDto', UpdateConfigDto],
] as const)('%s tickets per order', (_name, Dto) => {
  it('passes a set limit to the service', () => {
    const dto = plainToInstance(Dto, { ...base, maxTicketsPerOrder: 6 });
    expect(dto.toServiceDto().maxTicketsPerOrder).toBe(6);
  });

  // proto3 sends an unset int32 as 0; the service must see "not given", so a
  // create takes the column default and an update keeps the stored limit.
  it('passes an unset limit as undefined', () => {
    const dto = plainToInstance(Dto, { ...base, maxTicketsPerOrder: 0 });
    expect(dto.toServiceDto().maxTicketsPerOrder).toBeUndefined();
  });

  it('refuses a limit above 10', async () => {
    const errors = await validate(plainToInstance(Dto, { ...base, maxTicketsPerOrder: 11 }));
    expect(errors.map((e) => e.property)).toContain('maxTicketsPerOrder');
  });
});

describe('EventResponseMapper.toProtoEventConfig', () => {
  it('puts the limit on the wire', () => {
    const entity = Object.assign(new EventConfigEntity(), {
      id: 'c1',
      ticketSaleStartDate: new Date('2026-10-01T00:00:00.000Z'),
      ticketSaleEndDate: new Date('2026-10-02T00:00:00.000Z'),
      maxTicketsPerOrder: 4,
    });
    expect(EventResponseMapper.toProtoEventConfig(entity).maxTicketsPerOrder).toBe(4);
  });
});
