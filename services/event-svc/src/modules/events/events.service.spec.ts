import { status as grpcStatus } from '@grpc/grpc-js';
import { RpcException } from '@nestjs/microservices';
import { Test } from '@nestjs/testing';
import { EventRoleType, EventStatus } from '@prisma/client';
import { UpdateConfigDto } from './dtos';
import { EventEntity } from './entities';
import { EventsService } from './events.service';
import { EventsRepository } from './repository/events.repository';

export async function buildService(repository: any): Promise<EventsService> {
  const moduleRef = await Test.createTestingModule({
    providers: [EventsService, { provide: EventsRepository, useValue: repository }],
  }).compile();
  return moduleRef.get(EventsService);
}

// RpcException.getError() is typed `string | object`; every exception this
// service means to raise carries the object form. Anything else escaped
// unclassified -- report that as a missing code, not a crash on the method.
export const errorOf = (e: unknown): { code?: number; message?: string } =>
  e instanceof RpcException ? (e.getError() as { code?: number; message?: string }) : {};

export const rejectionOf = (p: Promise<unknown>): Promise<unknown> =>
  p.then(
    () => null,
    (e) => e,
  );

// An event held by 'admin-1' as ADMIN and 'editor-1' as EDITOR. Status is a
// parameter because every lifecycle case turns on it.
export const eventWith = (status: EventStatus): EventEntity =>
  ({
    id: 'evt-1',
    name: 'On sale',
    status,
    roles: [
      { id: 'r-1', userId: 'admin-1', eventId: 'evt-1', role: EventRoleType.ADMIN },
      { id: 'r-2', userId: 'editor-1', eventId: 'evt-1', role: EventRoleType.EDITOR },
    ],
  }) as EventEntity;

describe('EventsService.findById', () => {
  it('raises EventNotFound when the event is missing', async () => {
    const service = await buildService({ findById: jest.fn().mockResolvedValue(null) });

    const error = await rejectionOf(service.findById('nope'));

    expect(errorOf(error).code).toBe(grpcStatus.NOT_FOUND);
  });

  it('returns the event when it exists', async () => {
    const event = eventWith(EventStatus.DRAFT);
    const service = await buildService({ findById: jest.fn().mockResolvedValue(event) });

    expect(await service.findById('evt-1')).toBe(event);
  });
});

describe('EventsService.approveEvent', () => {
  it('refuses a caller who holds no role on the event', async () => {
    const update = jest.fn();
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.CONFIGURED)),
      update,
    });

    const error = await rejectionOf(service.approveEvent('evt-1', 'stranger'));

    expect(errorOf(error).code).toBe(grpcStatus.PERMISSION_DENIED);
    expect(update).not.toHaveBeenCalled();
  });

  it('refuses an editor: approving is an admin action', async () => {
    const update = jest.fn();
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.CONFIGURED)),
      update,
    });

    const error = await rejectionOf(service.approveEvent('evt-1', 'editor-1'));

    expect(errorOf(error).code).toBe(grpcStatus.PERMISSION_DENIED);
    expect(update).not.toHaveBeenCalled();
  });

  it('lets an admin approve', async () => {
    const update = jest.fn().mockResolvedValue(undefined);
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.CONFIGURED)),
      update,
    });

    await service.approveEvent('evt-1', 'admin-1');

    expect(update).toHaveBeenCalledWith('evt-1', { status: EventStatus.APPROVED });
  });
});

describe('EventsService authorization, on the paths that already had it', () => {
  it('refuses an update from a caller with no role', async () => {
    const service = await buildService({
      findEventRoles: jest.fn().mockResolvedValue(eventWith(EventStatus.DRAFT).roles),
      update: jest.fn(),
    });

    const error = await rejectionOf(service.update('evt-1', 'stranger', { name: 'x' }));

    expect(errorOf(error).code).toBe(grpcStatus.PERMISSION_DENIED);
  });

  it('lets an editor update', async () => {
    const update = jest.fn().mockResolvedValue(eventWith(EventStatus.DRAFT));
    const service = await buildService({
      findEventRoles: jest.fn().mockResolvedValue(eventWith(EventStatus.DRAFT).roles),
      update,
    });

    await service.update('evt-1', 'editor-1', { name: 'x' });

    expect(update).toHaveBeenCalledWith('evt-1', { name: 'x' });
  });

  it('refuses a publish from a caller with no role', async () => {
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.APPROVED)),
      update: jest.fn(),
    });

    const error = await rejectionOf(service.publishEvent('evt-1', 'stranger'));

    expect(errorOf(error).code).toBe(grpcStatus.PERMISSION_DENIED);
  });
});

describe('EventsService.updateConfig', () => {
  const configPatch: UpdateConfigDto = {
    eventId: 'evt-1',
    ticketSaleStartDate: new Date('2026-01-01T00:00:00.000Z'),
    ticketSaleEndDate: new Date('2026-01-02T00:00:00.000Z'),
    isFree: false,
    maxAttendees: 100,
    isPublic: false,
    requiresApproval: false,
    allowWaitRoom: true,
    isNewTrending: false,
  };

  it('addresses the config by its event, not by the event id as a config id', async () => {
    const updateConfigByEventId = jest.fn().mockResolvedValue({ id: 'cfg-1' });
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.CONFIGURED)),
      updateConfigByEventId,
    });

    await service.updateConfig('evt-1', 'admin-1', configPatch);

    expect(updateConfigByEventId).toHaveBeenCalledWith('evt-1', configPatch);
  });

  it('refuses a caller with no role before touching the config', async () => {
    const updateConfigByEventId = jest.fn();
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.CONFIGURED)),
      updateConfigByEventId,
    });

    const error = await rejectionOf(service.updateConfig('evt-1', 'stranger', configPatch));

    expect(errorOf(error).code).toBe(grpcStatus.PERMISSION_DENIED);
    expect(updateConfigByEventId).not.toHaveBeenCalled();
  });
});
