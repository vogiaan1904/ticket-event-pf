import { Test } from '@nestjs/testing';
import { LoggerService } from '@/shared/services/logger.service';
import { EventsService } from '../../events.service';
import { GrpcEventsController } from './events.controller';
import { ApproveEventDto, PublishEventDto } from './dtos';

const loggerStub = {
  setContext: jest.fn(),
  info: jest.fn(),
  error: jest.fn(),
} as unknown as LoggerService;

async function buildController(eventsService: any): Promise<GrpcEventsController> {
  const moduleRef = await Test.createTestingModule({
    controllers: [GrpcEventsController],
    providers: [
      { provide: EventsService, useValue: eventsService },
      { provide: LoggerService, useValue: loggerStub },
    ],
  }).compile();
  return moduleRef.get(GrpcEventsController);
}

const publishDto = { eventId: 'evt-1', userId: 'admin-1' } as PublishEventDto;
const approveDto = { eventId: 'evt-1', userId: 'admin-1' } as ApproveEventDto;

describe('GrpcEventsController lifecycle methods', () => {
  it('surfaces a refused publish instead of answering OK', async () => {
    const controller = await buildController({
      publishEvent: jest.fn().mockRejectedValue(new Error('permission denied')),
    });

    await expect(controller.publishEvent(publishDto)).rejects.toThrow('permission denied');
  });

  it('surfaces a refused approval instead of answering OK', async () => {
    const controller = await buildController({
      approveEvent: jest.fn().mockRejectedValue(new Error('permission denied')),
    });

    await expect(controller.approveEvent(approveDto)).rejects.toThrow('permission denied');
  });

  it('does not answer until the publish has actually happened', async () => {
    let written = false;
    // A real write outlasts a microtask. Awaiting a dropped promise's `undefined`
    // yields one tick, which is enough to hide the bug behind Promise.resolve().
    const publishEvent = jest.fn(async () => {
      await new Promise((resolve) => setImmediate(resolve));
      written = true;
    });
    const controller = await buildController({ publishEvent });

    await controller.publishEvent(publishDto);

    expect(written).toBe(true);
  });

  it('passes the event and caller through unchanged', async () => {
    const approveEvent = jest.fn().mockResolvedValue(undefined);
    const controller = await buildController({ approveEvent });

    await controller.approveEvent(approveDto);

    expect(approveEvent).toHaveBeenCalledWith('evt-1', 'admin-1');
  });
});
