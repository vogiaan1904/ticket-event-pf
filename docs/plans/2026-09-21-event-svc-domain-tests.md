# event-svc Domain Test Coverage Implementation Plan

**Goal:** Put the event lifecycle under test, and fix the seven defects the tests are written to catch — including an approval step with no authorization, a config update that can never succeed, and two gRPC methods that report success before doing the work.

**Architecture:** Same shape as the three completed plans: jest with `moduleNameMapper` aliases (already present), `Test.createTestingModule` with the repository supplied as a plain mock, no database and no network. Each task writes a failing test first, then the smallest fix that turns it green, then commits.

**Tech Stack:** TypeScript 5.7, NestJS 11, jest 29 + ts-jest 29, `@nestjs/testing` 11, Prisma 6.15.

**Spec:** none. Derived from a read of `services/event-svc/src/modules/events/` on 2026-09-21. The documented lifecycle in `services/event-svc/CLAUDE.md` — `DRAFT → CONFIGURED → APPROVED → PUBLISHED` — is the contract Task 4 enforces.

**Line numbers throughout are as of that read.** Each task shifts the ones below it. Match on the quoted block text, not on the line number.

## Global Constraints

- **ts-jest type-checks every spec.** A type error in one spec fails the whole suite, not one test.
- **`tsc --noEmit` lies here.** `incremental: true` plus a stale `dist/tsconfig.tsbuildinfo` will report a clean build over real errors. Use `npx tsc --noEmit --incremental false` whenever a type check is the evidence for a claim.
- **Never write `as any`.** `as unknown as <Type>` is allowed for framework objects with a dozen methods you do not use; nothing else.
- **Prettier is the only formatting gate that runs.** `npm run lint` is broken repo-wide (ESLint 9 against an `.eslintrc.js` it cannot read). Run `npx prettier --check "src/**/*.ts"` before every commit. Files already dirty before this plan are out of scope — check only what you touched.
- **Error codes here are numeric**, in per-entity blocks from `...000`: `EventNotFound = 20000`, `EventConfigNotFound = 20001`, `OrganizerNotFound = 21000`, and the auth one-off `PermissionDenied = 20403`. A new Event-entity code continues at `20002`.
- **The proto and Prisma status enums are not interchangeable** — proto is `DRAFT=1, PUBLISHED=2, CANCELLED=3, CONFIGURED=4, APPROVED=5`. Inside the service and repository, only the Prisma enum exists. Never compare the two directly.
- **The approval rule is a decision, already made:** approval requires the **ADMIN** role on that event. Not editor.
- **Commit messages describe the platform.** No mention of plans, tasks, or test-writing sessions.

---

## What is wrong today

Seven defects, in the order the tasks fix them.

**1. `findById` is not `async` and its guard is dead.** `events.service.ts:43-50` calls `this.repository.findById(id)` without `await`, so `event` is a *Promise* — always truthy. `if (!event)` can never run. A missing event resolves to `null` and reaches `EventResponseMapper.toProtoEvent(null)` in the controller instead of raising `EventNotFound`. **Live.**

**2. `approveEvent` has no authorization at all.** `events.service.ts:140-147` takes `userId` and never reads it. Every other lifecycle method checks the caller's role; this one approves for anybody who can reach the RPC. Approval is the gate immediately before publish. **Live, and it is an access-control hole.**

**3. Both fire-and-forget methods on the controller report success before doing the work.** `events.controller.ts:122-130`: `publishEvent` and `approveEvent` are not `async`, do not `await`, and do not return the promise. Three consequences, all live:
   - the gRPC call answers **OK immediately**, before the write happens;
   - a `PermissionDenied` or `EventNotFound` from the service is **reported to the caller as success**;
   - the rejection is attached to nothing, so it becomes an unhandled promise rejection. Node's default since v15 is `--unhandled-rejections=throw`, which raises an uncaught exception; nothing in this service installs a handler for one.

**4. `updateConfig` looks up a config by the event's id.** The controller passes `dto.eventId` (`events.controller.ts:108`); the service passes it straight to `repository.updateConfig(id, dto)` (`events.service.ts:110`), which does `where: { id }` on `eventConfig` (`events.repository.ts:290-295`). `EventConfig.id` is its own uuid and `eventId` is a separate `@unique` column, so the two never match. Every call raises Prisma `P2025`. **Live: this RPC cannot succeed.**

**5. There is no lifecycle state machine.** `CLAUDE.md` documents `DRAFT → CONFIGURED → APPROVED → PUBLISHED`, and nothing enforces it. `publishEvent` will publish a `DRAFT` event that was never configured or approved; `approveEvent` will approve a `DRAFT`. Per the error taxonomy a valid request against the wrong world-state is `FAILED_PRECONDITION`, and no such code exists in this service yet. **Live.**

**6. `createConfig` resets the status unconditionally.** `events.service.ts:90` writes `status: CONFIGURED` whatever the event's current status is, so configuring a `PUBLISHED` event silently drags it back to `CONFIGURED` — an un-publish through a side door. **Live.**

**7. `create` is two writes with no transaction.** `events.service.ts:18-24` creates the event, then creates the creator's ADMIN role in a second statement. If the second fails, the event exists with **no roles at all** — and every method that could fix it is gated on holding a role. The event is permanently unadministrable. **Live, and unrecoverable through the API.**

## File structure

| File | Change | Task |
|---|---|---|
| `src/modules/events/events.service.spec.ts` | create | 1, 3, 4, 5 |
| `src/modules/events/events.service.ts` | role helper, `findById`, approve gate, config id, state machine, atomic create | 1, 3, 4, 5 |
| `src/modules/events/controllers/grpc/events.controller.spec.ts` | create | 2 |
| `src/modules/events/controllers/grpc/events.controller.ts` | await and return | 2 |
| `src/modules/events/repository/events.repository.ts` | config addressed by event; nested role create | 3, 5 |
| `src/shared/constants/error-code.constant.ts` | `EventStateInvalid` | 4 |

Jest aliases are already configured in `services/event-svc/package.json`, and the service already has two passing suites (interceptor, filter) — 6 tests. Every count below includes them.

---

### Task 1: One place answers "may this caller?", and a missing gate is closed

The same role test is written out five times and omitted a sixth. Give it one name, then use it to close the gap on `approveEvent`.

**Files:**
- Modify: `services/event-svc/src/modules/events/events.service.ts`
- Test: `services/event-svc/src/modules/events/events.service.spec.ts` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `buildService(repository: any): Promise<EventsService>`, `errorOf(e: unknown)`, `rejectionOf(p)` and the `adminEvent`/`editorEvent` fixtures, all module-local in the new spec and reused by Tasks 3, 4 and 5.

- [ ] **Step 1: Write the failing tests**

Create `services/event-svc/src/modules/events/events.service.spec.ts`:

```ts
import { status as grpcStatus } from '@grpc/grpc-js';
import { RpcException } from '@nestjs/microservices';
import { Test } from '@nestjs/testing';
import { EventRoleType, EventStatus } from '@prisma/client';
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
```

- [ ] **Step 2: Run them and watch three fail**

Run: `cd services/event-svc && npm test -- events.service`

Expected: **three fail** —
- `raises EventNotFound when the event is missing`: resolves to `null` instead of rejecting, so `errorOf(null).code` is `undefined`, not `5`;
- `refuses a caller who holds no role on the event` and `refuses an editor`: both *resolve*, and `update` was called. `approveEvent` checks nothing.

The other four pass, and they are the regression guard for the refactor in Step 3 — they must still pass afterwards.

- [ ] **Step 3: Give the role test one name, and use it where it was missing**

In `services/event-svc/src/modules/events/events.service.ts`:

Add `EventRoleEntity` to the entities import on line 6:

```ts
import { EventConfigEntity, EventEntity, EventRoleEntity } from './entities';
```

Add this above the `@Injectable()` decorator:

```ts
// Editing an event and configuring it are the same privilege; approving is not.
const CAN_EDIT = [EventRoleType.ADMIN, EventRoleType.EDITOR];
```

Add this as the first member of the class, immediately after the constructor:

```ts
  // Every gated method asks the same question of the caller. Asking it in one
  // place is what makes a missing gate visible.
  private assertRole(
    roles: EventRoleEntity[] | undefined,
    userId: string,
    allowed: EventRoleType[],
  ): void {
    const held = roles?.some((role) => role.userId === userId && allowed.includes(role.role));
    if (!held) {
      throw new RpcBusinessException(ErrorCodeEnum.PermissionDenied);
    }
  }
```

Now replace each hand-written check. In `update`, replace the `canUpdate` block (lines 31-38) with:

```ts
    this.assertRole(roles, userId, CAN_EDIT);
```

In `createConfig`, `updateConfig` and `publishEvent`, replace each `isAdminOrEditor` block with:

```ts
    this.assertRole(event.roles, userId, CAN_EDIT);
```

In `findConfigByEventId`, replace the block inside `if (userId) { ... }` so the whole conditional reads:

```ts
    if (userId) {
      this.assertRole(event.roles, userId, CAN_EDIT);
    }
```

Replace `findById` (lines 43-50) entirely:

```ts
  async findById(id: string): Promise<EventEntity> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    return event;
  }
```

And `approveEvent` (lines 140-147):

```ts
  async approveEvent(id: string, userId: string): Promise<void> {
    const event = await this.repository.findById(id);
    if (!event) {
      throw new RpcBusinessException(ErrorCodeEnum.EventNotFound);
    }

    // Approving is the gate before publish, so it is not an editor's to open.
    this.assertRole(event.roles, userId, [EventRoleType.ADMIN]);

    await this.repository.update(id, { status: EventStatus.APPROVED });
  }
```

- [ ] **Step 4: Run the suite**

Run: `cd services/event-svc && npm test && npx prettier --check "src/**/*.ts"`

Expected: **13 passed** (6 existing + 7 new).

Then mutate to check the tests bite: change `allowed.includes(role.role)` to `true` and confirm the three `PERMISSION_DENIED` cases fail. Put it back.

- [ ] **Step 5: Commit**

```bash
git add services/event-svc/src/modules/events/events.service.ts services/event-svc/src/modules/events/events.service.spec.ts
git commit -m "$(cat <<'EOF'
fix(event): approving an event is an admin's to do, and nobody's by default

The approve path took a userId and never read it, so any caller could open
the gate before publish. The role test now has one name, which is what made
the missing one visible.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: The controller waits for the work it reports

**Files:**
- Modify: `services/event-svc/src/modules/events/controllers/grpc/events.controller.ts:122-130`
- Test: `services/event-svc/src/modules/events/controllers/grpc/events.controller.spec.ts` (create)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: nothing later tasks import.

- [ ] **Step 1: Write the failing test**

Create `services/event-svc/src/modules/events/controllers/grpc/events.controller.spec.ts`:

```ts
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
    const publishEvent = jest.fn(async () => {
      await Promise.resolve();
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
```

- [ ] **Step 2: Run it and watch three fail**

Run: `cd services/event-svc && npm test -- events.controller`

Expected: the two `rejects.toThrow` cases fail — the methods return `undefined`, so there is nothing to reject and jest reports `received value must be a promise`. The `written` case fails with `Expected: true / Received: false`, because the answer came back before the write. The fourth passes: the arguments are forwarded correctly today, it is only the waiting that is missing.

Both failing rejection cases will also print an `UnhandledPromiseRejection` warning during this run. That warning *is* defect 3 — note it, it disappears with the fix.

- [ ] **Step 3: Return the promise**

In `services/event-svc/src/modules/events/controllers/grpc/events.controller.ts`, replace both methods (lines 122-130):

```ts
  @GrpcMethod(EVENT_SERVICE_NAME, 'publishEvent')
  async publishEvent(dto: PublishEventDto): Promise<void> {
    await this.eventsService.publishEvent(dto.eventId, dto.userId);
  }

  @GrpcMethod(EVENT_SERVICE_NAME, 'approveEvent')
  async approveEvent(dto: ApproveEventDto): Promise<void> {
    await this.eventsService.approveEvent(dto.eventId, dto.userId);
  }
```

- [ ] **Step 4: Run the suite**

Run: `cd services/event-svc && npm test && npx prettier --check "src/**/*.ts"`

Expected: **17 passed**, and no unhandled-rejection warning in the output.

- [ ] **Step 5: Commit**

```bash
git add services/event-svc/src/modules/events/controllers/grpc/events.controller.ts services/event-svc/src/modules/events/controllers/grpc/events.controller.spec.ts
git commit -m "$(cat <<'EOF'
fix(event): publish and approve answer after the write, not before

Both handlers dropped the promise, so the caller was told OK while the work
was still pending -- and a refusal reached nobody but the unhandled-rejection
handler.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: A config belongs to an event, and is addressed that way

**Files:**
- Modify: `services/event-svc/src/modules/events/repository/events.repository.ts:290-295`
- Modify: `services/event-svc/src/modules/events/events.service.ts` (`updateConfig`)
- Test: `services/event-svc/src/modules/events/events.service.spec.ts` (append)

**Interfaces:**
- Consumes: `buildService`, `errorOf`, `rejectionOf`, `eventWith` from Task 1's spec, same file.
- Produces: `EventsRepository.updateConfigByEventId(eventId: string, dto: UpdateConfigDto): Promise<EventConfigEntity>` — the old `updateConfig(id, dto)` renamed and re-targeted.

- [ ] **Step 1: Write the failing test**

Append to `services/event-svc/src/modules/events/events.service.spec.ts`:

Add `UpdateConfigDto` to the spec's imports:

```ts
import { UpdateConfigDto } from './dtos';
```

Then append:

```ts
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
```

- [ ] **Step 2: Run it and watch the first fail**

Run: `cd services/event-svc && npm test -- events.service`

Expected: the first case fails — the service calls `repository.updateConfig`, which the mock does not define, so it throws `this.repository.updateConfig is not a function`. That is the honest red: the method it should be calling does not exist yet.

- [ ] **Step 3: Re-target the repository method**

In `services/event-svc/src/modules/events/repository/events.repository.ts`, replace `updateConfig`:

```ts
  // eventId, not id: EventConfig.id is its own uuid and every caller here holds
  // the event's.
  async updateConfigByEventId(eventId: string, dto: UpdateConfigDto): Promise<EventConfigEntity> {
    return this.prisma.eventConfig.update({
      where: { eventId },
      data: dto,
    });
  }
```

In `services/event-svc/src/modules/events/events.service.ts`, change the call in `updateConfig`:

```ts
    const config = await this.repository.updateConfigByEventId(id, dto);
```

Rename the service method's first parameter from `id` to `eventId` so the two agree, and update its two uses in that method (`findById(eventId)` and the call above).

- [ ] **Step 4: Run the suite**

Run: `cd services/event-svc && npm test && npx prettier --check "src/**/*.ts"`

Expected: **19 passed**.

Then grep for any remaining caller of the old name — `grep -rn "updateConfig(" services/event-svc/src --include='*.ts'` — and confirm only the service method and the controller's own `updateConfig` handler remain. The repository method must have no caller under its old name.

- [ ] **Step 5: Commit**

```bash
git add services/event-svc/src/modules/events/repository/events.repository.ts services/event-svc/src/modules/events/events.service.ts services/event-svc/src/modules/events/events.service.spec.ts
git commit -m "$(cat <<'EOF'
fix(event): a config update finds the config

EventConfig.id is its own uuid, and every caller holds the event's, so the
lookup matched nothing and the RPC could not succeed. The method now says
which id it wants.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: The lifecycle is a machine, not a suggestion

`DRAFT → CONFIGURED → APPROVED → PUBLISHED`. A transition out of turn is a valid request against the wrong world-state, which the taxonomy calls `FAILED_PRECONDITION` — never `INTERNAL`, and it must never page.

**Files:**
- Modify: `services/event-svc/src/shared/constants/error-code.constant.ts`
- Modify: `services/event-svc/src/modules/events/events.service.ts` (`approveEvent`, `publishEvent`, `createConfig`)
- Test: `services/event-svc/src/modules/events/events.service.spec.ts` (append)

**Interfaces:**
- Consumes: `buildService`, `errorOf`, `rejectionOf`, `eventWith` from Task 1's spec.
- Produces: `ErrorCodeEnum.EventStateInvalid = 20002`.

- [ ] **Step 1: Add the code the taxonomy requires**

In `services/event-svc/src/shared/constants/error-code.constant.ts`, add to the enum after `EventConfigNotFound`:

```ts
  EventStateInvalid = 20002,
```

and to the frozen table:

```ts
  [ErrorCodeEnum.EventStateInvalid]: [
    'Event is not in a state that allows this',
    409,
    grpcStatus.FAILED_PRECONDITION,
  ],
```

`FAILED_PRECONDITION` is deliberate and load-bearing: an organizer publishing too early is not a server fault, and no alert rule may reference this code.

- [ ] **Step 2: Write the failing tests**

Append to `services/event-svc/src/modules/events/events.service.spec.ts`:

Add `CreateConfigDto` to the spec imports:

```ts
import { CreateConfigDto } from './dtos/create-config.dto';
```

Then append:

```ts
describe('EventsService lifecycle transitions', () => {
  const newConfig: CreateConfigDto = {
    eventId: 'evt-1',
    ticketSaleStartDate: new Date('2026-01-01T00:00:00.000Z'),
    ticketSaleEndDate: new Date('2026-01-02T00:00:00.000Z'),
    isFree: false,
    maxAttendees: 100,
    isPublic: true,
    requiresApproval: false,
    allowWaitRoom: true,
    isNewTrending: false,
  };

  it('refuses to approve an event that was never configured', async () => {
    const update = jest.fn();
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.DRAFT)),
      update,
    });

    const error = await rejectionOf(service.approveEvent('evt-1', 'admin-1'));

    expect(errorOf(error).code).toBe(grpcStatus.FAILED_PRECONDITION);
    expect(update).not.toHaveBeenCalled();
  });

  it('refuses to publish an event that was never approved', async () => {
    const update = jest.fn();
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.CONFIGURED)),
      update,
    });

    const error = await rejectionOf(service.publishEvent('evt-1', 'admin-1'));

    expect(errorOf(error).code).toBe(grpcStatus.FAILED_PRECONDITION);
    expect(update).not.toHaveBeenCalled();
  });

  it('publishes an approved event', async () => {
    const update = jest.fn().mockResolvedValue(undefined);
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.APPROVED)),
      update,
    });

    await service.publishEvent('evt-1', 'admin-1');

    expect(update).toHaveBeenCalledWith('evt-1', { status: EventStatus.PUBLISHED });
  });

  it('does not drag a published event back to configured', async () => {
    const update = jest.fn();
    const createConfig = jest.fn().mockResolvedValue({ id: 'cfg-1' });
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.PUBLISHED)),
      createConfig,
      update,
    });

    await service.createConfig('admin-1', newConfig);

    expect(createConfig).toHaveBeenCalled();
    expect(update).not.toHaveBeenCalled();
  });

  it('marks a draft configured once it has a config', async () => {
    const update = jest.fn().mockResolvedValue(undefined);
    const service = await buildService({
      findById: jest.fn().mockResolvedValue(eventWith(EventStatus.DRAFT)),
      createConfig: jest.fn().mockResolvedValue({ id: 'cfg-1' }),
      update,
    });

    await service.createConfig('admin-1', newConfig);

    expect(update).toHaveBeenCalledWith('evt-1', { status: EventStatus.CONFIGURED });
  });
});
```

- [ ] **Step 3: Run them and watch four fail**

Run: `cd services/event-svc && npm test -- events.service`

Expected: four fail — the two `FAILED_PRECONDITION` cases resolve instead of rejecting, and `does not drag a published event back to configured` sees `update` called. `publishes an approved event` and `marks a draft configured` pass already; they are the guard that the new checks do not over-reject.

- [ ] **Step 4: Enforce the order**

In `services/event-svc/src/modules/events/events.service.ts`, add above the `@Injectable()` decorator, next to `CAN_EDIT`:

```ts
// The one state each transition may be entered from.
//   DRAFT -> CONFIGURED -> APPROVED -> PUBLISHED
const APPROVE_FROM = EventStatus.CONFIGURED;
const PUBLISH_FROM = EventStatus.APPROVED;
```

Add a second guard beside `assertRole`:

```ts
  // A transition out of turn is a valid request against the wrong world state,
  // which the taxonomy calls FAILED_PRECONDITION. It is never INTERNAL.
  private assertStatus(event: EventEntity, required: EventStatus): void {
    if (event.status !== required) {
      throw new RpcBusinessException(ErrorCodeEnum.EventStateInvalid);
    }
  }
```

In `approveEvent`, after the role assertion:

```ts
    this.assertStatus(event, APPROVE_FROM);
```

In `publishEvent`, after the role assertion:

```ts
    this.assertStatus(event, PUBLISH_FROM);
```

In `createConfig`, replace the unconditional status write (line 90):

```ts
    const config = await this.repository.createConfig(dto);

    // Only a draft advances. Configuring a live event must not un-publish it.
    if (event.status === EventStatus.DRAFT) {
      await this.repository.update(dto.eventId, { status: EventStatus.CONFIGURED });
    }

    return config;
```

- [ ] **Step 5: Run the suite**

Run: `cd services/event-svc && npm test && npx prettier --check "src/**/*.ts"`

Expected: **24 passed**.

Then confirm the code stays out of the alert rules: `grep -rn "FAILED_PRECONDITION" deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml` must return nothing.

- [ ] **Step 6: Commit**

```bash
git add services/event-svc/src/shared/constants/error-code.constant.ts services/event-svc/src/modules/events/events.service.ts services/event-svc/src/modules/events/events.service.spec.ts
git commit -m "$(cat <<'EOF'
feat(event): the lifecycle refuses a transition out of turn

Draft events could be published without ever being configured or approved,
and configuring a live event dragged it back to CONFIGURED. Out-of-turn is
FAILED_PRECONDITION: the organizer is early, the server is fine.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: An event is never created without an owner

**Files:**
- Modify: `services/event-svc/src/modules/events/repository/events.repository.ts:156-198`
- Modify: `services/event-svc/src/modules/events/events.service.ts` (`create`)
- Test: `services/event-svc/src/modules/events/events.service.spec.ts` (append)

**Interfaces:**
- Consumes: `buildService` from Task 1's spec.
- Produces: nothing.

- [ ] **Step 1: Write the failing test**

Append to `services/event-svc/src/modules/events/events.service.spec.ts`:

Add `CreateEventDto` to the spec imports:

```ts
import { CreateEventDto } from './dtos';
```

Then append:

```ts
describe('EventsService.create', () => {
  const newEvent: CreateEventDto = {
    createdBy: 'admin-1',
    name: 'On sale',
    description: 'd',
    startDate: new Date('2026-01-01T00:00:00.000Z'),
    endDate: new Date('2026-01-02T00:00:00.000Z'),
    thumbnailUrl: 't',
    venue: 'v',
    street: 's',
    city: 'c',
    country: 'co',
    categoryIds: ['cat-1'],
    organizerName: 'o',
    organizerDescription: 'od',
    organizerLogoUrl: 'ol',
  };

  it('creates the event and its admin role in one call', async () => {
    const create = jest.fn().mockResolvedValue(eventWith(EventStatus.DRAFT));
    const createRole = jest.fn();
    const service = await buildService({ create, createRole });

    await service.create(newEvent);

    expect(create).toHaveBeenCalledWith(newEvent);
    // A second statement is a second chance to fail, and an event with no role
    // can never be administered by anyone.
    expect(createRole).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

Run: `cd services/event-svc && npm test -- events.service`

Expected: fails on `expect(createRole).not.toHaveBeenCalled()` — the service calls it as a separate statement today.

- [ ] **Step 3: Nest the role in the event's own insert**

`EventRole` already has an `Event` relation and the repository's `create` already nests `location`, `organizer` and `categories`. Adding `roles` makes the whole thing one statement, which is atomic without needing `$transaction`.

In `services/event-svc/src/modules/events/repository/events.repository.ts`, inside `create`'s `data`, after the `categories` block:

```ts
        roles: {
          create: {
            userId: dto.createdBy,
            role: EventRoleType.ADMIN,
          },
        },
```

Add `EventRoleType` to the Prisma import at the top of that file:

```ts
import { EventRoleType, Prisma } from '@prisma/client';
```

In `services/event-svc/src/modules/events/events.service.ts`, `create` collapses to:

```ts
  async create(dto: CreateEventDto): Promise<EventEntity> {
    // The creator's ADMIN role is nested in the same insert: an event with no
    // role can be administered by nobody, and no method can repair it.
    return this.repository.create(dto);
  }
```

`EventRoleType` may now be unused in the service — check before removing it from that import; Task 1's `CAN_EDIT` and `approveEvent` both still use it, so it stays.

- [ ] **Step 4: Run the suite**

Run: `cd services/event-svc && npm test && npx prettier --check "src/**/*.ts"` and `npx tsc --noEmit --incremental false`

Expected: **25 passed**, tsc clean.

Then grep for other callers of `repository.createRole` — `grep -rn "createRole" services/event-svc/src --include='*.ts'`. If the repository method now has none, leave it in place (it is the obvious home for adding a collaborator later) but say so in your report.

- [ ] **Step 5: Commit**

```bash
git add services/event-svc/src/modules/events/repository/events.repository.ts services/event-svc/src/modules/events/events.service.ts services/event-svc/src/modules/events/events.service.spec.ts
git commit -m "$(cat <<'EOF'
fix(event): an event and its admin role are written together

They were two statements, and a failure between them left an event nobody
holds a role on -- which every method that could repair it is gated behind.
The role is now nested in the event's own insert.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Found, not fixed

- **`CANCELLED` is in both enums and in no transition.** Nothing sets it and nothing refuses it. The state machine in Task 4 treats it as a terminal state only by accident — every transition requires a specific predecessor, so a cancelled event is stuck, which is probably right but is not stated anywhere.
- **`delete` has no authorization and no status check.** `events.service.ts:69-71` forwards straight to the repository, so any caller who can reach the RPC can delete any event, published or not. It is not in the proto's service block, so it is unreachable over gRPC today — which is the only reason this plan does not treat it as a P0.
- **`updateCategory(dto: any)`** at `events.repository.ts:321` — the only `any` in the repository, and it has no caller.
- **`findMany` trusts `dto.filter`.** `events.controller.ts:70` calls `dto.filter.toServiceDto()` with no null check; a `findMany` with no filter throws a `TypeError`, which is now correctly an `INTERNAL`. It should be `INVALID_ARGUMENT`, or `filter` should be optional with a default.
- **`create`'s commented-out `eventCategory.createMany`** at `events.repository.ts:191-196` is superseded by the nested `categories` block above it. Dead, and should be deleted.
- **The proto `EventStatus` carries `UNSPECIFIED = 0`, which neither mapper handles.** `EventStatusMapper.toPrisma(0)` throws a bare `Error`, not an `RpcException` — so an unset status field on the wire produces an `INTERNAL` rather than an `INVALID_ARGUMENT`.

## Self-review

- **Coverage.** Seven defects named in "What is wrong today"; Task 1 fixes two, Tasks 2, 3 and 5 one each, Task 4 fixes two (the missing machine and the status regression). Seven accounted for.
- **Placeholders.** None: every step carries the code or the exact command.
- **Type consistency.** `buildService`, `errorOf`, `rejectionOf` and `eventWith` are declared in Task 1's spec and reused by Tasks 3, 4 and 5 from the same file. `updateConfigByEventId` is named identically in Task 3's Interfaces block, its repository signature and its service call site. `EventRoleEntity` is imported in Task 1, `EventRoleType` added to the repository's Prisma import in Task 5.
- **Fixtures.** Every DTO fixture is fully declared against its real type, because a fixture that names the real shape is what catches a field being dropped. The `eventWith` helper is the one deliberate partial cast, and it is legal: verified with `tsc`, `{ a } as Full` compiles when the literal is a pure subset, and only fails (TS2352) when it carries a property the target does not have.
- **Test-count arithmetic.** 6 existing + 7 (Task 1) = 13; + 4 (Task 2) = 17; + 2 (Task 3) = 19; + 5 (Task 4) = 24; + 1 (Task 5) = 25. Each task's Step 4 states the running total.
