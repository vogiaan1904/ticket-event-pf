import 'reflect-metadata';
import { GRPC_LOADER_OPTIONS } from '@/shared/constants/grpc.constant';
import { loadSync, ServiceDefinition } from '@grpc/proto-loader';
import { join } from 'path';
import { of } from 'rxjs';
import { CreateEventDto, UpdateEventDto } from './dtos/req';
import { EventsService } from './events.service';

const def = loadSync(join(__dirname, '../../protos/event.proto'), GRPC_LOADER_OPTIONS);
const rpc = (name: string) => (def['event.EventService'] as ServiceDefinition)[name];

// What the gateway sends, after the runtime contract has encoded and decoded it.
async function overTheWire(call: 'create' | 'update', dto: object) {
  let sent: Record<string, unknown> = {};
  const client = { [call]: (req: Record<string, unknown>) => ((sent = req), of({ event: {} })) };
  const service = new EventsService({ getService: () => client } as never);
  service.onModuleInit();
  await (call === 'create'
    ? service.create({ id: 'u1', email: 'a@b.c' }, dto as CreateEventDto)
    : service.update({ id: 'u1', email: 'a@b.c' }, 'e1', dto as UpdateEventDto));
  const m = rpc(call === 'create' ? 'Create' : 'Update');
  return {
    sent,
    arrived: m.requestDeserialize(m.requestSerialize(sent)) as Record<string, unknown>,
  };
}

const event = {
  name: 'Show',
  description: 'd',
  startDate: '2027-01-01T10:00:00.000Z',
  endDate: '2027-01-02T10:00:00.000Z',
  thumbnailUrl: 'https://example.com/t.png',
  venue: 'Hall',
  street: '1 St',
  ward: 'W',
  district: 'D',
  city: 'HCMC',
  country: 'VN',
  categoryIds: ['c1'],
  organizerName: 'Org',
  organizerDescription: 'od',
  organizerLogoUrl: 'https://example.com/o.png',
};

describe.each(['create', 'update'] as const)('an event %s', (call) => {
  it('reaches event-svc with every field the gateway sends', async () => {
    const { sent, arrived } = await overTheWire(call, event);
    for (const [key, value] of Object.entries(sent)) {
      expect({ key, value: arrived[key] }).toEqual({ key, value });
    }
  });
});
