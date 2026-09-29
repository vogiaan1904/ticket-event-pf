import { plainToInstance } from 'class-transformer';
import { validate } from 'class-validator';
import { ConfigMapper } from '../../mappers/config.mapper';
import { CreateConfigDto } from './create-config.dto';
import { UpdateConfigDto } from './update-config.dto';

const base = {
  ticketSaleStartDate: '2026-10-01',
  ticketSaleEndDate: '2026-10-02',
  isFree: false,
  maxAttendees: 500,
  isPublic: true,
  requiresApproval: false,
  allowWaitRoom: true,
  isNewTrending: false,
};

const invalid = async (Dto: typeof CreateConfigDto | typeof UpdateConfigDto, value: unknown) =>
  (await validate(plainToInstance(Dto, { ...base, maxTicketsPerOrder: value }))).map((e) => e.property);

describe.each([
  ['CreateConfigDto', CreateConfigDto],
  ['UpdateConfigDto', UpdateConfigDto],
] as const)('%s tickets per order', (_name, Dto) => {
  it('accepts a request that leaves the limit out', async () => {
    expect(await invalid(Dto, undefined)).not.toContain('maxTicketsPerOrder');
  });

  it('accepts a limit from 1 to 10', async () => {
    expect(await invalid(Dto, 1)).not.toContain('maxTicketsPerOrder');
    expect(await invalid(Dto, 10)).not.toContain('maxTicketsPerOrder');
  });

  it('refuses 0 and 11', async () => {
    expect(await invalid(Dto, 0)).toContain('maxTicketsPerOrder');
    expect(await invalid(Dto, 11)).toContain('maxTicketsPerOrder');
  });
});

describe('ConfigMapper.toDto', () => {
  it('returns the limit to the organizer', () => {
    const dto = ConfigMapper.toDto({
      id: 'c1',
      ticketSaleStartDate: '2026-10-01T00:00:00.000Z',
      ticketSaleEndDate: '2026-10-02T00:00:00.000Z',
      isFree: false,
      maxAttendees: 500,
      maxTicketsPerOrder: 4,
      isPublic: true,
      requiresApproval: false,
      allowWaitRoom: true,
      isNewTrending: false,
    });
    expect(dto.maxTicketsPerOrder).toBe(4);
  });
});
