import 'reflect-metadata';
import { plainToInstance } from 'class-transformer';
import { validate } from 'class-validator';
import { UpdateConfigDto } from './update-config.dto';
import { UpdateEventDto } from './update-event.dto';

const config = {
  isFree: false,
  maxAttendees: 500,
  isPublic: true,
  requiresApproval: false,
  allowWaitRoom: true,
  isNewTrending: false,
};

const failing = async (Dto: new () => object, body: object) =>
  (await validate(plainToInstance(Dto, body))).map((e) => e.property);

// An update takes the same instants a create does: a sale that opens at 19:00 stays at 19:00.
describe('an update keeps the time of day', () => {
  it('accepts an event start and end with their times', async () => {
    const body = { startDate: '2027-02-01T19:00:00.000Z', endDate: '2027-02-01T22:00:00.000Z' };
    expect(await failing(UpdateEventDto, body)).toEqual([]);
  });

  it('accepts a sale window with its times', async () => {
    const body = {
      ...config,
      ticketSaleStartDate: '2027-01-15T19:00:00.000Z',
      ticketSaleEndDate: '2027-01-31T23:59:00.000Z',
    };
    expect(await failing(UpdateConfigDto, body)).toEqual([]);
  });

  it('still refuses something that is not a date', async () => {
    expect(await failing(UpdateEventDto, { startDate: 'next friday' })).toContain('startDate');
  });
});
