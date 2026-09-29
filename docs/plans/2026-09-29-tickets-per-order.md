# Tickets per order, per event

**Status: COMPLETE 2026-09-29.** An order takes at most its event's limit, default 4;
verified on k3s. Decision:
→ [0021](../decisions/0021-an-order-takes-at-most-its-events-ticket-limit.md), accepted.

**Goal:** An order may take at most its event's `max_tickets_per_order` tickets.
Organizers set the limit through the gateway, event-svc stores it (default 4), and
order-svc refuses an order over it before anything is held.

**Spec:** `docs/design/admission-sizing.md`, *Tickets per order*. This is step 1 of
that design's *What comes next*.

**How this plan was checked:** every change below was made once in a throwaway git
worktree on 2026-09-29, and every test was run there:
- event-svc: 27 of 27 jest tests pass;
- gateway: 37 of 37 jest tests pass, and `tsc` is clean;
- order-svc: `go test ./...` passes, and so does waitroom-svc's.

Each new test was also broken on purpose, and failed. Stub regeneration reproduces the
committed stubs byte for byte, so a regenerated stub shows only the new field.

## Resuming after a break or a compaction

- Progress lives in the ledger, `.superpowers/sdd/2026-09-29-tickets-per-order/progress.md`,
  which is gitignored.
- A task with a `Task N: complete` line there is done: resume at the first task
  without one.
- `git log --oneline` shows the commits each task made; each task's commit message is
  given in the task.
- Read the spec section and this plan's *Global constraints* again before resuming.

## Global constraints

- Commit messages describe the platform, never study progress, and end with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Pushing `dev` needs the architect's go-ahead.
- The k3s box and EKS never run at once: `aws eks list-clusters --region us-east-1` must
  print nothing before the box starts.
- Comment budget: 3 lines inline, 5 on a symbol.
- Order-svc returns declared error vars, never `fmt.Errorf`.
- **Never run `npm run update:proto` in `services/api-gateway`.** It regenerates every
  stub, including the stale `order.pb.ts` that the register's open row describes, and
  that breaks `src/modules/orders/`. Copy `event.proto` and run `npm run proto:event`
  only.
- The wire contract: 0 means "not set" in both directions. Default 4; the gateway
  accepts 1–10; event-svc accepts 0–10; `ORD020` is `INVALID_ARGUMENT`.

## Review focus

1. **A rolling deploy must refuse nothing new.** An order-svc that reads 0 from an
   event-svc without the field checks nothing: Task 3,
   `TestCreate_AnEventWithNoLimitSetRefusesNothing`.
2. **An update that leaves the field out must keep the stored limit.** The gateway
   sends 0, and event-svc turns 0 into "unchanged": Task 1, *passes an unset limit as
   undefined*, and Task 2's `?? 0`.
3. **The limit counts an order's tickets across all its items.** Task 3's over-limit
   test orders 2 + 2 against a limit of 3; no single item is over.
4. **A refused order holds nothing:** no purchase slot and no saga. Task 3's
   over-limit test asserts both.
5. **Existing events get 4.** Task 4 reads the migrated column on the box.

## Setup

For Tasks 1–3, locally:

```bash
docker compose -f services/order-svc/docker-compose.dev.yml up -d   # dynamodb-local for order-svc's create tests
```

order-svc's create tests **skip** when dynamodb-local is down, and a skipped test
proves nothing. Run them with `-v` and check that no line says `SKIP`.

Task 4 uses the k3s box; its setup is in that task.

---

### Task 1: event-svc stores and serves the limit

**Files:**
- Modify: `proto/event.proto` (`EventConfig`, `CreateEventConfigRequest`, `UpdateEventConfigRequest`)
- Regenerate: `services/event-svc/src/protos/event.proto`, `services/event-svc/src/protogen/event.pb.ts`
- Modify: `services/event-svc/prisma/schema/event.prisma`
- Create: `services/event-svc/prisma/migrations/20260929120000_max_tickets_per_order/migration.sql`
- Modify: `services/event-svc/src/modules/events/entities/event-config.entity.ts`,
  `services/event-svc/src/modules/events/repository/events.mapper.ts`,
  `services/event-svc/src/modules/events/dtos/create-config.dto.ts`,
  `services/event-svc/src/modules/events/dtos/update-config.dto.ts`,
  `services/event-svc/src/modules/events/controllers/grpc/dtos/create-config.dto.ts`,
  `services/event-svc/src/modules/events/controllers/grpc/dtos/update-config.dto.ts`,
  `services/event-svc/src/modules/events/controllers/grpc/dtos/event.dto.ts`,
  `services/event-svc/src/modules/events/controllers/grpc/mappers/event.mapper.ts`
- Test: `services/event-svc/src/modules/events/controllers/grpc/dtos/config.dto.spec.ts`

**Interfaces:**
- Produces: the proto field `max_tickets_per_order` (TS `maxTicketsPerOrder: number`,
  Go `MaxTicketsPerOrder int32`, getter `GetMaxTicketsPerOrder()`), on `EventConfig` (10),
  `CreateEventConfigRequest` (11) and `UpdateEventConfigRequest` (11). Tasks 2 and 3
  consume it.

- [ ] **Step 1: Add the field to the contract.** In `proto/event.proto`, end
  `message EventConfig` with:

```proto
    bool is_new_trending = 9;
    // Tickets one order may take; 0 means not set.
    int32 max_tickets_per_order = 10;
}
```

and end both `CreateEventConfigRequest` and `UpdateEventConfigRequest` with:

```proto
    bool is_new_trending = 10;
    // 0 leaves it unset: the default on create, unchanged on update.
    int32 max_tickets_per_order = 11;
}
```

- [ ] **Step 2: Write the failing spec**,
  `services/event-svc/src/modules/events/controllers/grpc/dtos/config.dto.spec.ts`:

```ts
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
```

- [ ] **Step 3: Run it.** `cd services/event-svc && npx jest config.dto`
  Expected: `Test suite failed to run`, with a TypeScript error naming
  `maxTicketsPerOrder`. ts-jest type-checks, and the field exists nowhere yet.

- [ ] **Step 4: Regenerate event-svc's stubs.** `cd services/event-svc && npm run update:proto`
  Expected: `git status --short services/event-svc` shows only `src/protos/event.proto`
  and `src/protogen/event.pb.ts`.

- [ ] **Step 5: Store it.** In `prisma/schema/event.prisma`, `model EventConfig`, after
  `maxAttendees`:

```prisma
  maxAttendees        Int
  maxTicketsPerOrder  Int         @default(4)
```

Create `prisma/migrations/20260929120000_max_tickets_per_order/migration.sql`. The SQL
is what `prisma migrate diff` produced from this schema change:

```sql
-- AlterTable
ALTER TABLE "public"."event_configs" ADD COLUMN     "maxTicketsPerOrder" INTEGER NOT NULL DEFAULT 4;
```

Then `npx prisma generate`. Expected: it succeeds. It needs no database.

- [ ] **Step 6: Carry it through the layers.**
  - `entities/event-config.entity.ts`, after `maxAttendees: number;`:
    `maxTicketsPerOrder: number;`
  - `repository/events.mapper.ts`, `mapToEventConfigEntity`, after the
    `maxAttendees` line: `entity.maxTicketsPerOrder = config.maxTicketsPerOrder;`
  - `dtos/create-config.dto.ts` and `dtos/update-config.dto.ts`, after
    `maxAttendees: number;`:

```ts
  // Unset: the column default on create, the stored value on update.
  maxTicketsPerOrder?: number;
```

  - `controllers/grpc/dtos/create-config.dto.ts` and `.../update-config.dto.ts`:
    - Change the import to
      `import { IsBoolean, IsInt, IsNotEmpty, IsNumber, IsOptional, IsString, Max, Min } from 'class-validator';`.
    - In `toServiceDto()`, after `maxAttendees: this.maxAttendees,`:

```ts
      // 0 is proto3's unset.
      maxTicketsPerOrder: this.maxTicketsPerOrder || undefined,
```

    - After the `maxAttendees` property:

```ts
  @IsOptional()
  @IsInt()
  @Min(0)
  @Max(10)
  maxTicketsPerOrder: number;
```

  - `controllers/grpc/dtos/event.dto.ts`, `class ConfigDto`, after `maxAttendees: number;`:
    `maxTicketsPerOrder: number;`
  - `controllers/grpc/mappers/event.mapper.ts`, `toProtoEventConfig`, after
    `maxAttendees: entity.maxAttendees,`: `maxTicketsPerOrder: entity.maxTicketsPerOrder,`

  The repository needs no change: `createConfig` spreads the DTO into Prisma, and
  `updateConfigByEventId` passes it as `data`. Prisma skips a field that is
  `undefined`, which is what makes 0 mean "default" on create and "unchanged" on
  update.

- [ ] **Step 7: Run it.** `npx tsc --noEmit -p tsconfig.json && npx jest src/modules/events`
  Expected: no output from `tsc`, then `Tests: 27 passed, 27 total`.

- [ ] **Step 8: Prove the checks can go red.** Make each change below to
  `update-config.dto.ts` (gRPC), run `npx jest config.dto`, then restore the file with
  `git checkout`:
  - Drop `|| undefined`. Expected: `✕ passes an unset limit as undefined`.
  - Delete `@Max(10)`. Expected: `✕ refuses a limit above 10`.

- [ ] **Step 9: Commit**

```bash
git add proto/event.proto services/event-svc/src services/event-svc/prisma
git commit -m "feat(event): give each event a tickets-per-order limit"
```

---

### Task 2: The gateway takes and returns the limit

**Files:**
- Sync: `services/api-gateway/src/protos/event.proto`; regenerate `services/api-gateway/src/protogen/event.pb.ts`
- Modify: `services/api-gateway/src/modules/events/dtos/req/create-config.dto.ts`,
  `services/api-gateway/src/modules/events/dtos/req/update-config.dto.ts`,
  `services/api-gateway/src/modules/events/dtos/resp/config.resp.dto.ts`,
  `services/api-gateway/src/modules/events/mappers/config.mapper.ts`,
  `services/api-gateway/src/modules/events/mappers/event.mapper.ts`,
  `services/api-gateway/src/modules/events/events.service.ts`
- Test: `services/api-gateway/src/modules/events/dtos/req/config.dto.spec.ts`

**Interfaces:**
- Consumes: `max_tickets_per_order` in `proto/event.proto` (Task 1).
- Produces: `maxTicketsPerOrder` on `POST`/`PUT /events/:id/config` (optional, 1–10) and
  on every config response.

- [ ] **Step 1: Write the failing spec**, `src/modules/events/dtos/req/config.dto.spec.ts`:

```ts
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
```

- [ ] **Step 2: Run it.** `cd services/api-gateway && npx jest config.dto`
  Expected: `Test suite failed to run`, a TypeScript error naming `maxTicketsPerOrder`.

- [ ] **Step 3: Sync the contract. Only event.proto, never `update:proto`.**

```bash
cp ../../proto/event.proto src/protos/event.proto && npm run proto:event
git status --short .   # expected: src/protos/event.proto, src/protogen/event.pb.ts, the new spec
```

- [ ] **Step 4: Take it, return it, forward it.**
  - `dtos/req/create-config.dto.ts`: change the import to
    `import { IsBoolean, IsDateString, IsInt, IsNotEmpty, IsNumber, IsOptional, Max, Min } from 'class-validator';`.
  - `dtos/req/update-config.dto.ts`: change the import to
    `import { IsBoolean, IsInt, IsNotEmpty, IsNumber, IsOptional, Max, Min } from 'class-validator';`.
  - In both, after the `maxAttendees` property:

```ts

  // Left out: the event's default on create, unchanged on update.
  @IsOptional()
  @IsInt()
  @Min(1)
  @Max(10)
  maxTicketsPerOrder?: number;
```

  - `dtos/resp/config.resp.dto.ts`, after `maxAttendees: number;`: `maxTicketsPerOrder: number;`
  - `mappers/config.mapper.ts`, after `maxAttendees: config.maxAttendees,`:
    `maxTicketsPerOrder: config.maxTicketsPerOrder,`
  - `mappers/event.mapper.ts`, after `maxAttendees: event.config.maxAttendees,`:
    `maxTicketsPerOrder: event.config.maxTicketsPerOrder,`
  - `events.service.ts`: the generated request type makes the field required, so the
    calls send 0 when the organizer left it out. Replace the `createConfig` call's
    argument with:

```ts
      this.eventService.createConfig({
        ...dto,
        maxTicketsPerOrder: dto.maxTicketsPerOrder ?? 0,
        eventId: id,
        userId: user.id,
      }),
```

    and the `updateConfig` call with:

```ts
      this.eventService.updateConfig({
        ...dto,
        // 0 is unset on the wire: event-svc keeps the stored limit.
        maxTicketsPerOrder: dto.maxTicketsPerOrder ?? 0,
        eventId: id,
        userId: user.id,
      }),
```

- [ ] **Step 5: Run it.** `npx tsc --noEmit -p tsconfig.json && npx jest`
  Expected: no `tsc` output, then `Tests: 37 passed, 37 total`.

- [ ] **Step 6: Prove it can go red.** Delete `@Min(1)` from `update-config.dto.ts`,
  then run `npx jest config.dto`. Expected: `✕ refuses 0 and 11`. Restore the file
  with `git checkout`.

- [ ] **Step 7: Commit**

```bash
git add services/api-gateway/src
git commit -m "feat(gateway): let an organizer set an event's tickets-per-order limit"
```

---

### Task 3: order-svc refuses an order over the limit

**Files:**
- Regenerate: `services/order-svc/pkg/grpc/event/event.pb.go`, `services/waitroom-svc/protogen/event/event.pb.go`
- Modify: `services/order-svc/internal/order/errors.go`,
  `services/order-svc/internal/order/delivery/grpc/errors.go`,
  `services/order-svc/internal/order/service/order.go` (`Create`),
  `services/order-svc/internal/order/service/utils.go`,
  `services/order-svc/internal/order/service/create_test.go` (the event stub),
  `services/order-svc/CLAUDE.md`
- Test: `services/order-svc/internal/order/service/ticket_limit_test.go`,
  `services/order-svc/internal/order/delivery/grpc/errors_test.go`

**Interfaces:**
- Consumes: `eCfg.GetMaxTicketsPerOrder() int32` (Task 1's field, via the Go stub).
- Produces: `order.ErrTooManyTicketsInOrder`, and `ErrGRPCTooManyTicketsInOrder`
  (`INVALID_ARGUMENT`, `ORD020`).

- [ ] **Step 1: Regenerate every Go consumer of event.proto.**

```bash
(cd services/order-svc && make protoc PROTO=../../proto/event.proto OUT_DIR=pkg/grpc/event)
(cd services/waitroom-svc && make protoc PROTO=../../proto/event.proto OUT_DIR=protogen/event)
git status --short services/order-svc services/waitroom-svc   # expected: the two event.pb.go files only
```

- [ ] **Step 2: Give the test stub the field.** In `internal/order/service/create_test.go`,
  `stubEventClient` becomes:

```go
type stubEventClient struct {
	event.EventServiceClient
	allowWaitRoom      bool
	maxTicketsPerOrder int32
}
```

and its `GetConfig` returns:

```go
	return &event.GetEventConfigResponse{EventConfig: &event.EventConfig{
		Id:                 in.EventId,
		AllowWaitRoom:      c.allowWaitRoom,
		MaxTicketsPerOrder: c.maxTicketsPerOrder,
	}}, nil
```

- [ ] **Step 3: Write the failing tests.** `internal/order/service/ticket_limit_test.go`:

```go
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
)

func limitedCreateService(t *testing.T, limit int32) (*implService, *fakeTemporalClient, func() string) {
	t.Helper()
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
	svc, r := newCreateService(t, tprCli, false, "")
	svc.evSvc = stubEventClient{maxTicketsPerOrder: limit}
	return svc, tprCli, func() string { return slotHolder(t, r, testUserEventSlotKey) }
}

// The limit counts tickets across every item, and refuses before the purchase
// slot is claimed or the saga starts, so a refused order holds nothing.
func TestCreate_AnOrderOverItsEventsLimitHoldsNothing(t *testing.T) {
	svc, tprCli, holder := limitedCreateService(t, 3)
	in := createInput()
	in.Items = []order.OrderItemInput{{TicketClassID: "tc1", Quantity: 2}, {TicketClassID: "tc2", Quantity: 2}}

	_, err := svc.Create(context.Background(), in)

	if !errors.Is(err, order.ErrTooManyTicketsInOrder) {
		t.Fatalf("err = %v, want ErrTooManyTicketsInOrder", err)
	}
	if tprCli.startedInput != nil {
		t.Fatal("the saga started for a refused order")
	}
	if h := holder(); h != "" {
		t.Fatalf("slot held by %s after a refused order", h)
	}
}

func TestCreate_AnOrderAtItsEventsLimitGoesAhead(t *testing.T) {
	svc, tprCli, _ := limitedCreateService(t, 2)

	if _, err := svc.Create(context.Background(), createInput()); err != nil {
		t.Fatalf("an order of 2 at a limit of 2: %v", err)
	}
	if tprCli.startedInput == nil {
		t.Fatal("the saga did not start")
	}
}

// 0 is what an event-svc that predates the field sends, so it must refuse nothing.
func TestCreate_AnEventWithNoLimitSetRefusesNothing(t *testing.T) {
	svc, tprCli, _ := limitedCreateService(t, 0)
	in := createInput()
	in.Items = []order.OrderItemInput{{TicketClassID: "tc1", Quantity: 50}}

	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("an order of 50 with no limit set: %v", err)
	}
	if tprCli.startedInput == nil {
		t.Fatal("the saga did not start")
	}
}
```

and `internal/order/delivery/grpc/errors_test.go`:

```go
package grpc

import (
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	pkgErrors "github.com/vogiaan1904/ticketbottle-order/pkg/errors"
	"google.golang.org/grpc/codes"
)

// Asking for too many tickets is the buyer's to fix, so it must not reach the
// gateway as INTERNAL, which pages.
func TestMapError_TooManyTicketsIsInvalidArgument(t *testing.T) {
	got, ok := (&grpcService{}).mapError(order.ErrTooManyTicketsInOrder).(*pkgErrors.GRPCError)
	if !ok || got.GrpcCode != codes.InvalidArgument {
		t.Fatalf("mapError = %v, want an InvalidArgument GRPCError", got)
	}
}
```

- [ ] **Step 4: Run them.**
  `cd services/order-svc && go test ./internal/order/service/ ./internal/order/delivery/grpc/ -run 'TestCreate_An|TestMapError' -count=1 -v`
  Expected: a build failure, `undefined: order.ErrTooManyTicketsInOrder`.

- [ ] **Step 5: Declare the error.** In `internal/order/errors.go`, after
  `ErrEventConfigNotFound`:

```go
	// The order asks for more tickets than its event lets one order take.
	ErrTooManyTicketsInOrder = errors.New("too many tickets in one order")
```

Run Step 4's command again. Expected: `--- FAIL: TestCreate_AnOrderOverItsEventsLimitHoldsNothing`
(`err = <nil>`) and `--- FAIL: TestMapError_TooManyTicketsIsInvalidArgument`. The
other two pass: at those limits, nothing should be refused.

- [ ] **Step 6: Refuse it and map it.**
  - `internal/order/delivery/grpc/errors.go`, after `ErrGRPCEventConfigNotFound`:

```go
	// InvalidArgument: the buyer fixes the request by asking for fewer.
	ErrGRPCTooManyTicketsInOrder = pkgErrors.NewGRPCError(codes.InvalidArgument, "ORD020", "Too many tickets in one order for this event")
```

    and in `mapError`, after the `ErrEventConfigNotFound` case:

```go
	case errors.Is(err, order.ErrTooManyTicketsInOrder):
		return ErrGRPCTooManyTicketsInOrder
```

  - `internal/order/service/order.go`, in `Create`, right after the
    `ErrEventNotReadyForSale` check:

```go

	// Checked before the slot or any hold is taken; 0 is an event with no limit set.
	if limit := eCfg.GetMaxTicketsPerOrder(); limit > 0 && ticketsInOrder(in.Items) > limit {
		return order.CreateOrderOutput{}, order.ErrTooManyTicketsInOrder
	}
```

  - `internal/order/service/utils.go`, at the end:

```go

// ticketsInOrder is how many tickets an order takes, across all its items.
func ticketsInOrder(items []order.OrderItemInput) int32 {
	var n int32
	for _, item := range items {
		n += item.Quantity
	}
	return n
}
```

- [ ] **Step 7: Run it.** Step 4's command, then:

```bash
gofmt -l internal/ && go test ./... -count=1 2>&1 | grep -v 'no test files'
(cd ../waitroom-svc && go build ./... && go test ./... -count=1 2>&1 | grep -v 'no test files')
```

  Expected: the four tests `--- PASS`, and **no line with `SKIP`**. `gofmt -l` prints
  nothing, and every order-svc and waitroom-svc package is `ok`.

- [ ] **Step 8: Prove the checks can go red.** Make each change below, run Step 4's
  command, then restore with `git checkout` (or undo the edit):
  - Replace `limit > 0 &&` with `false &&`. Expected:
    `--- FAIL: TestCreate_AnOrderOverItsEventsLimitHoldsNothing`.
  - Replace `limit > 0 &&` with `limit >= 0 &&`. Expected:
    `--- FAIL: TestCreate_AnEventWithNoLimitSetRefusesNothing`.
  - Delete the `ErrTooManyTicketsInOrder` case from `mapError`. Expected:
    `--- FAIL: TestMapError_TooManyTicketsIsInvalidArgument`.

- [ ] **Step 9: Record the rule** in `services/order-svc/CLAUDE.md`, as a bullet after
  the `CreateOrder` one:

```markdown
- **An order takes at most its event's `max_tickets_per_order` tickets**, counted across
  its items and checked in `Create` before the purchase slot or any hold
  ([0021](../../docs/decisions/0021-an-order-takes-at-most-its-events-ticket-limit.md)).
  Over it: `INVALID_ARGUMENT`, `ORD020`. 0 means no limit is set, which is what an
  event-svc that predates the field sends.
```

- [ ] **Step 10: Commit**

```bash
git add services/order-svc services/waitroom-svc/protogen/event
git commit -m "feat(order): refuse an order over its event's ticket limit"
```

---

### Task 4: Prove it on the box, and record it

**Files:**
- Modify: `deploy/scripts/gate1-purchase-flow.sh`
- Modify: `docs/decisions/0021-an-order-takes-at-most-its-events-ticket-limit.md`,
  `docs/decisions/README.md` (generated), `docs/design/admission-sizing.md`,
  `CLAUDE.md`, this plan

- [ ] **Step 1: Teach the acceptance test the limit.** In
  `deploy/scripts/gate1-purchase-flow.sh`, section 3:
  - The config body gains `"maxTicketsPerOrder":2`, so its last line reads
    `"allowWaitRoom":true,"isNewTrending":false,"maxTicketsPerOrder":2`.
  - After `echo "  config response: $CFG"`, add:

```bash
[ "$(echo "$CFG" | getval data.maxTicketsPerOrder)" = 2 ] || fail "config did not keep maxTicketsPerOrder: $CFG"
```

  Then, just before `echo "== 8. create order =="`:

```bash
echo "== 7b. an order over the event's limit of 2 is refused =="
OVER=$(curl -s -o /tmp/g1-over.json -w '%{http_code}' -X POST "$GW/orders" -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"eventId\":\"$EVENT_ID\",\"userFullname\":\"Gate One\",\"userEmail\":\"$EMAIL\",
  \"userPhone\":\"0900000000\",\"paymentMethod\":\"ZALOPAY\",
  \"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":3}],\"currency\":\"VND\",
  \"checkoutToken\":\"$CHECKOUT\",\"redirectUrl\":\"https://example.com/done\"}")
[ "$OVER" = 400 ] || fail "an order of 3 at a limit of 2 answered $OVER: $(cat /tmp/g1-over.json)"
grep -q ORD020 /tmp/g1-over.json || fail "the refusal is not ORD020: $(cat /tmp/g1-over.json)"
echo "  refused: 400 ORD020"
```

  The refusal comes before the checkout token is checked or the purchase slot is
  taken, so the real order in section 8 goes ahead on the same token.

- [ ] **Step 2: Push, with the architect's go-ahead.** Run `git push origin dev`, then
  wait until all five workflows on the pushed commit report `success`
  (`gh run list --branch dev`): `system-map`, `chart-assertions`, `decision-records`,
  `go-tests`, `build-push-ecr`.

- [ ] **Step 3: Start the box.**

```bash
aws eks list-clusters --region us-east-1                  # must print nothing
make -C deploy start-ec2-k3s
make -C deploy my-ip                                      # differs from the tfvars? then:
make -C deploy k3s-allow-ip                               # plan: only the SG changes; apply
```

  The local network leaves from two rotating addresses. Sample them with
  `for i in $(seq 1 12); do curl -fsS https://api.ipify.org; echo; done | sort -u`, and add
  any second one as a /32 SSH rule:
  `aws ec2 authorize-security-group-ingress --region us-east-1 --group-id sg-0a44018bf9c2246f7 --ip-permissions 'IpProtocol=tcp,FromPort=22,ToPort=22,IpRanges=[{CidrIp=<ip>/32}]'`.
  Then:

```bash
ssh -N -o ServerAliveInterval=30 -L 6443:127.0.0.1:6443 -L 3000:127.0.0.1:30000 ec2-user@<box ip> &
make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml
```

  After a cold boot, `order-service` and `order-consumer` crash-loop until Temporal is
  up; wait until every pod is Running.

- [ ] **Step 4: Deploy and check the migration.** Run `make -C deploy k3s-deploy`. It
  deploys the build `:dev` points at, which is the pushed commit. Then:

```bash
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_event -tAc \
  'SELECT count(*), min("maxTicketsPerOrder"), max("maxTicketsPerOrder") FROM event_configs;'
```

  Expected: every existing config reads 4, as `<n>|4|4`.

- [ ] **Step 5: Run the acceptance test.** `make -C deploy k3s-gate2`
  Expected: `refused: 400 ORD020`, then the order completes and the gate passes.

- [ ] **Step 6: Record.**
  - **0021:** `accepted`, with an `## Outcome` citing:
    - the three commits;
    - the tests that went red then green, and the mutations;
    - Step 4's count;
    - Step 5's refusal.
  - **Design:** the *Tickets per order* status becomes built.
  - **Root `CLAUDE.md`:** a Decided row, "How many tickets may one order take?" →
    `docs/decisions/0021`, "A limit per event, default 4; refused before anything is
    held; built and verified on k3s <date>".
  - **This plan:** status COMPLETE, and *Results* filled in.
  - Then run `python3 docs/decisions/index.py`, `python3 docs/decisions/index.py --check`
    and `python3 .claude/skills/system-map/scripts/check_map.py`, and commit:

```bash
git add deploy/scripts/gate1-purchase-flow.sh docs CLAUDE.md
git commit -m "docs: record the tickets-per-order limit as built and verified"
```

- [ ] **Step 7: Stop the box.** Kill the tunnel, run `make -C deploy stop-ec2-k3s`, and
  confirm the instance is `stopped`. Push the last commit only with the architect's
  go-ahead.

## Results

Built in `c53944f` (event-svc), `1ec0ee6` (gateway) and `be65603` (order-svc), pushed
2026-09-29; CI green on `be65603`.

- Every new test was seen failing first. Final runs: event-svc 27/27, gateway 37/37,
  order-svc and waitroom-svc `go test ./...` ok with no SKIP.
- Every mutation in Tasks 1–3 failed the test named for it.
- On k3s, 2026-09-29, on `sha-be65603`, `make -C deploy k3s-gate2` passed:
  - the config echoed `maxTicketsPerOrder: 2`;
  - an order of 3 was refused with `400 ORD020`;
  - order `TB-GATE1-20260929-637GKSXE` then completed on the same checkout token.
- Step 4's count of existing configs was not observed. `ADD COLUMN … NOT NULL DEFAULT 4`
  fills every existing row, and the gate read the column back.
- The deploy ran the migration after the new code. event-service started at
  16:01:04Z; the migration finished at 16:02:35Z. For 91s, event-svc served against a
  table without the column. Nobody was buying. During a sale, every config read would
  have failed, and every order create makes one.

## Found, not fixed

- **No cap on one buyer's total across orders.** A buyer can finish an order, queue
  again and buy more. This is a separate anti-scalping rule.
- **A migration runs after the code that needs it.** The migration Jobs are
  `post-upgrade` hooks, which Helm runs only once every pod is Ready (*Results*).
- **The gateway's `order.pb.ts` is still stale** (the register's open row), which is
  why Task 2 syncs `event.proto` alone.
