# TicketBottle Console Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a minimal, functional web console that lets a developer drive the entire TicketBottle backend from a browser — sign up/in, create events + ticket classes, browse events with live availability, run the full waitroom→order→payment buy flow, and view orders/profile.

**Architecture:** A new Vite + React 18 app (`apps/console`) added to the existing `ticketbottle-fe` Nx workspace, reusing its already-installed tooling (Chakra UI v3, TanStack Router + Query, Zustand, axios). All HTTP goes through a Vite dev proxy (`/api` → `http://localhost:3000`) so calls are same-origin (no CORS changes, and the unauthenticated SSE `EventSource` works). One small backend change lands first: the API Gateway's stub `InventoryModule` is wired to the inventory gRPC service and exposes three REST routes the buy/admin flows need.

**Tech Stack:** TypeScript 5.6, Vite, React 18.3, Chakra UI v3, @tanstack/react-router (file-based) + @tanstack/react-query v5, Zustand v5, axios, vitest (frontend logic tests); NestJS 10 + gRPC (backend gateway), jest (backend tests).

## Global Constraints

- **Two repos.** Backend changes: `/Users/vogiaan/coding/projects/TicketEventPF` (branch off `main`). Frontend: `/Users/vogiaan/coding/projects/ticketbottle-fe` (separate git; branch off its default). Every task states which repo and gives absolute paths.
- **No new frontend dependencies.** Chakra v3, TanStack Router/Query, zustand, axios, vitest, testing-library are already in the `ticketbottle-fe` root `node_modules`. Do not add packages.
- **Package manager:** `ticketbottle-fe` uses **pnpm** (`pnpm-lock.yaml` is authoritative). Run installs/commands with `pnpm` / `npx` from the repo root.
- **API base + envelope.** Base URL is `/api` (proxied). Every REST response is wrapped by the gateway as `{ success: boolean; message: string; data: T }`. Paginated list endpoints put `{ data: T[]; meta: PaginationMeta }` inside that outer `data`.
- **Auth.** All gateway endpoints require `Authorization: Bearer <accessToken>` **except** `GET /api/waitroom/position/:sessionId` (SSE, unguarded). Token pair `{ accessToken, refreshToken, expiresIn }` is stored in `localStorage` + Zustand.
- **`/auth/me` returns only `{ id, email }`** — there is no GET-full-profile endpoint. Profile name/avatar are write-only via `PATCH /users/:id`. This is a known limitation; do not try to pre-fill them.
- **Money is in cents** (`priceCents`, `totalAmountCents`, `priceCents` per item). `int64` gRPC fields arrive as strings over gRPC-JSON — always coerce with `Number(...)` (mirror the existing `Number(proto.position)` pattern in `services/api-gateway/src/modules/waitroom/mappers`).
- **Inventory proto quirk:** `proto/inventory.proto` declares `package event;` (shared with event-svc). There is **no** `INVENTORY_PACKAGE_NAME`; use the literal package string `'event'` and the service token `INVENTORY_SERVICE_NAME` (`"InventoryService"`) from `src/protogen/inventory.pb.ts`. Inventory gRPC port is **50057**.
- **Precondition for all frontend verification:** the backend stack must be running: `cd /Users/vogiaan/coding/projects/TicketEventPF/development && make up-aws`. The gateway listens on `http://localhost:3000`.
- **Commits:** one commit per task. Backend commit messages end with the repo's `Co-Authored-By` trailer per `TicketEventPF/CLAUDE.md`; frontend repo has no such rule.

---

## File Structure

### Backend (`TicketEventPF/services/api-gateway`)
- `src/shared/services/config.service.ts` — **modify**: add `inventoryServiceUrl` to `microservicesConfig`.
- `development/envs/.env.api-gateway` — **modify**: add `INVENTORY_SERVICE`.
- `src/modules/inventory/inventory.module.ts` — **modify**: register the inventory gRPC client.
- `src/modules/inventory/inventory.service.ts` — **modify**: implement 3 gRPC-calling methods + proto→DTO mapping.
- `src/modules/inventory/inventory.service.spec.ts` — **create**: unit tests (mock `ClientGrpc`).
- `src/modules/inventory/dtos/req/create-ticket-class.dto.ts` — **create**.
- `src/modules/inventory/dtos/resp/ticket-class.resp.dto.ts` — **create**.
- `src/modules/inventory/dtos/resp/availability.resp.dto.ts` — **create**.
- `src/modules/inventory/dtos/{req,resp}/index.ts` — **create**.
- `src/modules/inventory/inventory.controller.ts` — **modify**: add 3 REST routes.
- `src/modules/waitroom/waitroom.controller.ts` — **modify (conditional)**: add response passthrough to the SSE route only if verification shows double-wrapping.
- `src/common/decorators/response-passthrough.decorator.ts` — **create (conditional)**.

### Frontend (`ticketbottle-fe/apps/console`)
- `package.json`, `vite.config.ts`, `tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`, `eslint.config.js`, `index.html`, `.env` — **create**: app config (mirrors `apps/ticketbottle`).
- `src/main.tsx` — **create**: providers + router bootstrap.
- `src/vite-env.d.ts` — **create**.
- `src/routeTree.gen.ts` — **generated** by the TanStack Router Vite plugin (never hand-edit).
- `src/components/ui/*`, `src/configs/theme.config.ts` — **copy** from `apps/ticketbottle` (Chakra v3 snippets + theme).
- `src/lib/apiClient.ts` — **create**: axios instance (base `/api`, auth header, envelope unwrap, error normalize).
- `src/lib/apiClient.test.ts` — **create**: vitest unit tests for unwrap/error.
- `src/store/authStore.ts` — **create**: Zustand auth slice + localStorage.
- `src/types/api.ts`, `src/types/domain.ts` — **create**: response envelope + domain types.
- `src/api/{auth,users,events,inventory,orders,waitroom}.api.ts` — **create**: typed API modules.
- `src/lib/waitroom.ts` + `src/lib/waitroom.test.ts` — **create**: SSE subscribe helper + admit-detection unit test.
- `src/components/{NavBar,Field wrappers}` as needed — **create** small helpers.
- `src/routes/*` — **create**: `__root.tsx`, `login.tsx`, `signup.tsx`, `_app.tsx` (auth-guard layout), and under `_app/`: `index.tsx`, `events/index.tsx`, `events/new.tsx`, `events/$eventId.tsx`, `buy/$eventId.tsx`, `orders/index.tsx`, `orders/$code.tsx`, `profile.tsx`.

---

## PHASE A — Backend: Gateway inventory REST endpoints

### Task A1: Wire the inventory gRPC client + implement InventoryService (with unit tests)

**Repo:** `TicketEventPF` · branch off `main` (e.g. `feat/gateway-inventory-endpoints`).

**Files:**
- Modify: `services/api-gateway/src/shared/services/config.service.ts` (add `inventoryServiceUrl`, ~line 68)
- Modify: `development/envs/.env.api-gateway` (add `INVENTORY_SERVICE`)
- Modify: `services/api-gateway/src/modules/inventory/inventory.module.ts`
- Modify: `services/api-gateway/src/modules/inventory/inventory.service.ts`
- Create: `services/api-gateway/src/modules/inventory/dtos/resp/ticket-class.resp.dto.ts`
- Create: `services/api-gateway/src/modules/inventory/dtos/resp/availability.resp.dto.ts`
- Create: `services/api-gateway/src/modules/inventory/dtos/resp/index.ts`
- Test: `services/api-gateway/src/modules/inventory/inventory.service.spec.ts`

**Interfaces:**
- Produces: `InventoryService` with
  - `findTicketClassesByEvent(eventId: string): Promise<TicketClassRespDto[]>`
  - `getAvailability(ticketClassId: string): Promise<AvailabilityRespDto>` where `AvailabilityRespDto = { availableQuantity: number }`
  - `createTicketClass(dto: CreateTicketClassDto): Promise<TicketClassRespDto>`
  - `TicketClassRespDto = { id, eventId, name, priceCents: number, currency, total: number, startSaleAt, endSaleAt, createdAt, updatedAt }`

- [ ] **Step 1: Create the response DTOs**

Create `services/api-gateway/src/modules/inventory/dtos/resp/ticket-class.resp.dto.ts`:

```ts
export class TicketClassRespDto {
  id: string;
  eventId: string;
  name: string;
  priceCents: number;
  currency: string;
  total: number;
  startSaleAt: string;
  endSaleAt: string;
  createdAt: string;
  updatedAt: string;
}
```

Create `services/api-gateway/src/modules/inventory/dtos/resp/availability.resp.dto.ts`:

```ts
export class AvailabilityRespDto {
  availableQuantity: number;
}
```

Create `services/api-gateway/src/modules/inventory/dtos/resp/index.ts`:

```ts
export * from './ticket-class.resp.dto';
export * from './availability.resp.dto';
```

- [ ] **Step 2: Write the failing unit test for the service**

Create `services/api-gateway/src/modules/inventory/inventory.service.spec.ts`:

```ts
import { Test } from '@nestjs/testing';
import { of } from 'rxjs';
import { InventoryService } from './inventory.service';
import { INVENTORY_SERVICE_NAME } from '@/protogen/inventory.pb';

describe('InventoryService', () => {
  let service: InventoryService;

  const grpcMock = {
    findManyTicketClass: jest.fn(),
    getAvailability: jest.fn(),
    createTicketClass: jest.fn(),
  };

  beforeEach(async () => {
    const moduleRef = await Test.createTestingModule({
      providers: [
        InventoryService,
        { provide: INVENTORY_SERVICE_NAME, useValue: { getService: () => grpcMock } },
      ],
    }).compile();

    service = moduleRef.get(InventoryService);
    service.onModuleInit();
    jest.clearAllMocks();
  });

  it('maps ticket classes and coerces int64 priceCents/total to numbers', async () => {
    grpcMock.findManyTicketClass.mockReturnValue(
      of({
        ticketClasses: [
          {
            id: 'tc1',
            eventId: 'e1',
            name: 'GA',
            priceCents: '15000', // int64 arrives as string
            currency: 'USD',
            total: 100,
            startSaleAt: '2026-07-01T00:00:00Z',
            endSaleAt: '2026-07-20T00:00:00Z',
            createdAt: '2026-06-01T00:00:00Z',
            updatedAt: '2026-06-01T00:00:00Z',
          },
        ],
      }),
    );

    const result = await service.findTicketClassesByEvent('e1');

    expect(grpcMock.findManyTicketClass).toHaveBeenCalledWith({ eventId: 'e1', ids: [] });
    expect(result).toHaveLength(1);
    expect(result[0].priceCents).toBe(15000);
    expect(typeof result[0].priceCents).toBe('number');
    expect(result[0].total).toBe(100);
  });

  it('returns availability with a numeric quantity', async () => {
    grpcMock.getAvailability.mockReturnValue(of({ availableQuantity: 42 }));
    const result = await service.getAvailability('tc1');
    expect(grpcMock.getAvailability).toHaveBeenCalledWith({ ticketClassId: 'tc1' });
    expect(result).toEqual({ availableQuantity: 42 });
  });

  it('maps a created ticket class', async () => {
    grpcMock.createTicketClass.mockReturnValue(
      of({ ticketClass: { id: 'tc9', eventId: 'e1', name: 'VIP', priceCents: '50000', currency: 'USD', total: 10, startSaleAt: '', endSaleAt: '', createdAt: '', updatedAt: '' } }),
    );
    const result = await service.createTicketClass({
      eventId: 'e1', name: 'VIP', priceCents: 50000, currency: 'USD', total: 10, startSaleAt: '', endSaleAt: '',
    } as any);
    expect(result.id).toBe('tc9');
    expect(result.priceCents).toBe(50000);
  });
});
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd services/api-gateway && npm run test -- inventory.service`
Expected: FAIL — `service.findTicketClassesByEvent is not a function` (service is still the empty stub).

- [ ] **Step 4: Implement the service**

Replace `services/api-gateway/src/modules/inventory/inventory.service.ts` with:

```ts
import {
  CreateTicketClassRequest,
  INVENTORY_SERVICE_NAME,
  InventoryServiceClient,
  TicketClass,
} from '@/protogen/inventory.pb';
import { Inject, Injectable } from '@nestjs/common';
import { ClientGrpc } from '@nestjs/microservices';
import { firstValueFrom } from 'rxjs';
import { AvailabilityRespDto, TicketClassRespDto } from './dtos/resp';

@Injectable()
export class InventoryService {
  private inventoryService: InventoryServiceClient;
  constructor(@Inject(INVENTORY_SERVICE_NAME) private inventoryServiceClient: ClientGrpc) {}

  public onModuleInit(): void {
    this.inventoryService =
      this.inventoryServiceClient.getService<InventoryServiceClient>(INVENTORY_SERVICE_NAME);
  }

  private toDto(proto: TicketClass): TicketClassRespDto {
    return {
      id: proto.id,
      eventId: proto.eventId,
      name: proto.name,
      priceCents: Number(proto.priceCents),
      currency: proto.currency,
      total: Number(proto.total),
      startSaleAt: proto.startSaleAt,
      endSaleAt: proto.endSaleAt,
      createdAt: proto.createdAt,
      updatedAt: proto.updatedAt,
    };
  }

  async findTicketClassesByEvent(eventId: string): Promise<TicketClassRespDto[]> {
    const resp = await firstValueFrom(
      this.inventoryService.findManyTicketClass({ eventId, ids: [] }),
    );
    return (resp.ticketClasses || []).map((tc) => this.toDto(tc));
  }

  async getAvailability(ticketClassId: string): Promise<AvailabilityRespDto> {
    const resp = await firstValueFrom(this.inventoryService.getAvailability({ ticketClassId }));
    return { availableQuantity: Number(resp.availableQuantity) };
  }

  async createTicketClass(dto: CreateTicketClassRequest): Promise<TicketClassRespDto> {
    const resp = await firstValueFrom(this.inventoryService.createTicketClass(dto));
    return this.toDto(resp.ticketClass);
  }
}
```

- [ ] **Step 5: Wire config + env + module**

In `services/api-gateway/src/shared/services/config.service.ts`, inside `get microservicesConfig()` add after the `orderServiceUrl` line:

```ts
      inventoryServiceUrl: this.get('INVENTORY_SERVICE'),
```

In `development/envs/.env.api-gateway`, add (docker-compose service DNS name + port):

```
INVENTORY_SERVICE=inventory-svc:50057
```

> If you run the gateway locally with `npm run start:dev` instead of in compose, set `INVENTORY_SERVICE=localhost:50057` in that environment.

Replace `services/api-gateway/src/modules/inventory/inventory.module.ts` with:

```ts
import { Module } from '@nestjs/common';
import { ClientsModule, Transport } from '@nestjs/microservices';
import { join } from 'path';
import { INVENTORY_SERVICE_NAME } from '@/protogen/inventory.pb';
import { AppConfigService } from '@/shared/services/config.service';
import { InventoryController } from './inventory.controller';
import { InventoryService } from './inventory.service';

@Module({
  imports: [
    ClientsModule.registerAsync([
      {
        name: INVENTORY_SERVICE_NAME,
        useFactory: async (config: AppConfigService) => ({
          transport: Transport.GRPC,
          options: {
            url: config.microservicesConfig.inventoryServiceUrl,
            // inventory.proto shares `package event;` — this is intentional, not a typo.
            package: 'event',
            protoPath: join(__dirname, '../../protos', 'inventory.proto'),
          },
        }),
        inject: [AppConfigService],
      },
    ]),
  ],
  controllers: [InventoryController],
  providers: [InventoryService],
})
export class InventoryModule {}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd services/api-gateway && npm run test -- inventory.service`
Expected: PASS (3 tests green).

- [ ] **Step 7: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/api-gateway/src/modules/inventory services/api-gateway/src/shared/services/config.service.ts development/envs/.env.api-gateway
git commit -m "feat(gateway): wire inventory gRPC client + service with ticket-class/availability mapping

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task A2: Add the InventoryController REST routes

**Repo:** `TicketEventPF` (same branch).

**Files:**
- Create: `services/api-gateway/src/modules/inventory/dtos/req/create-ticket-class.dto.ts`
- Create: `services/api-gateway/src/modules/inventory/dtos/req/index.ts`
- Modify: `services/api-gateway/src/modules/inventory/inventory.controller.ts`

**Interfaces:**
- Consumes: `InventoryService` from Task A1.
- Produces REST routes (all under global prefix `api`):
  - `GET /api/inventory/events/:eventId/ticket-classes` → `TicketClassRespDto[]`
  - `GET /api/inventory/ticket-classes/:id/availability` → `AvailabilityRespDto`
  - `POST /api/inventory/ticket-classes` → `TicketClassRespDto`

- [ ] **Step 1: Create the request DTO**

Create `services/api-gateway/src/modules/inventory/dtos/req/create-ticket-class.dto.ts`:

```ts
import { IsDateString, IsInt, IsNotEmpty, IsString, Min } from 'class-validator';

export class CreateTicketClassDto {
  @IsNotEmpty()
  @IsString()
  eventId: string;

  @IsNotEmpty()
  @IsString()
  name: string;

  @IsInt()
  @Min(0)
  priceCents: number;

  @IsNotEmpty()
  @IsString()
  currency: string;

  @IsInt()
  @Min(1)
  total: number;

  @IsNotEmpty()
  @IsDateString()
  startSaleAt: string;

  @IsNotEmpty()
  @IsDateString()
  endSaleAt: string;
}
```

Create `services/api-gateway/src/modules/inventory/dtos/req/index.ts`:

```ts
export * from './create-ticket-class.dto';
```

- [ ] **Step 2: Implement the controller**

Replace `services/api-gateway/src/modules/inventory/inventory.controller.ts` with:

```ts
import { AccessGuard } from '@/common/guards/access.guard';
import { ResponseDto } from '@/common/interceptors/transfrom.interceptor';
import { Body, Controller, Get, Param, Post, UseGuards } from '@nestjs/common';
import { CreateTicketClassDto } from './dtos/req';
import { AvailabilityRespDto, TicketClassRespDto } from './dtos/resp';
import { InventoryService } from './inventory.service';

@Controller('inventory')
export class InventoryController {
  constructor(private readonly inventoryService: InventoryService) {}

  @Get('events/:eventId/ticket-classes')
  @UseGuards(AccessGuard)
  @ResponseDto(TicketClassRespDto)
  async findTicketClasses(@Param('eventId') eventId: string): Promise<TicketClassRespDto[]> {
    return this.inventoryService.findTicketClassesByEvent(eventId);
  }

  @Get('ticket-classes/:id/availability')
  @UseGuards(AccessGuard)
  @ResponseDto(AvailabilityRespDto)
  async getAvailability(@Param('id') id: string): Promise<AvailabilityRespDto> {
    return this.inventoryService.getAvailability(id);
  }

  @Post('ticket-classes')
  @UseGuards(AccessGuard)
  @ResponseDto(TicketClassRespDto)
  async createTicketClass(@Body() dto: CreateTicketClassDto): Promise<TicketClassRespDto> {
    return this.inventoryService.createTicketClass(dto);
  }
}
```

- [ ] **Step 3: Build the gateway to confirm it compiles**

Run: `cd services/api-gateway && npm run build`
Expected: build succeeds with no TypeScript errors.

- [ ] **Step 4: Verify end-to-end against the running stack**

Start the stack if not already up: `cd /Users/vogiaan/coding/projects/TicketEventPF/development && make up-aws` (wait for containers healthy: `make status`).

Get a token (create a user first if needed), then exercise the routes. Replace `$TOKEN` and `$EVENT_ID` with real values:

```bash
# 1) sign up -> capture accessToken
curl -s -X POST http://localhost:3000/api/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"firstName":"Dev","lastName":"One","email":"dev+console@test.com","password":"Str0ng!Pass1"}'

# 2) create a ticket class (use the accessToken from step 1 and a real eventId)
curl -s -X POST http://localhost:3000/api/inventory/ticket-classes \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"eventId":"'"$EVENT_ID"'","name":"GA","priceCents":15000,"currency":"USD","total":100,"startSaleAt":"2026-07-01T00:00:00Z","endSaleAt":"2026-07-31T00:00:00Z"}'

# 3) list ticket classes for the event
curl -s http://localhost:3000/api/inventory/events/$EVENT_ID/ticket-classes -H "Authorization: Bearer $TOKEN"

# 4) availability for a ticket class id from step 2/3
curl -s http://localhost:3000/api/inventory/ticket-classes/$TICKET_CLASS_ID/availability -H "Authorization: Bearer $TOKEN"
```

Expected: step 2 returns `{"success":true,...,"data":{"id":...,"priceCents":15000,...}}`; step 3 returns a `data` array; step 4 returns `{"availableQuantity":<n>}`. If a call returns a gRPC "unavailable" error, confirm `inventory-svc` is healthy (`make status`) and `INVENTORY_SERVICE` points at it.

- [ ] **Step 5: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add services/api-gateway/src/modules/inventory
git commit -m "feat(gateway): expose inventory ticket-class + availability REST routes

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## PHASE B — Frontend scaffold

### Task B1: Create the `console` app skeleton and prove it runs

**Repo:** `ticketbottle-fe` · branch off default (e.g. `feat/console-app`).

**Files (all under `/Users/vogiaan/coding/projects/ticketbottle-fe/apps/console/`):**
- Create: `package.json`, `index.html`, `.env`, `vite.config.ts`, `tsconfig.json`, `tsconfig.app.json`, `tsconfig.node.json`, `eslint.config.js`
- Create: `src/vite-env.d.ts`, `src/main.tsx`, `src/routes/__root.tsx`, `src/routes/index.tsx`
- Copy: `src/components/ui/**`, `src/configs/theme.config.ts` (from `apps/ticketbottle`)

**Interfaces:**
- Produces: a runnable Vite app on port **5545** with a Vite proxy `/api` → `http://localhost:3000`, Chakra provider, TanStack Query + Router bootstrapped. `Provider` component available at `@/components/ui/provider`. `toaster`/`Toaster` at `@/components/ui/toaster`.

- [ ] **Step 1: Ensure the workspace installs cleanly (catches broken node_modules early)**

Run:
```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
pnpm install
npx nx dev ticketbottle --help >/dev/null 2>&1 || true
```
Expected: `pnpm install` completes without unmet-peer/errors. (This mirrors the known "clean reinstall before any TS build" caution.)

- [ ] **Step 2: Copy reusable Chakra assets from the existing app**

Run:
```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
mkdir -p apps/console/src/components apps/console/src/configs apps/console/src/routes apps/console/public
cp -R apps/ticketbottle/src/components/ui apps/console/src/components/ui
cp apps/ticketbottle/src/configs/theme.config.ts apps/console/src/configs/theme.config.ts
```
Expected: `apps/console/src/components/ui/provider.tsx`, `toaster.tsx`, `color-mode.tsx`, `field.tsx` etc. exist.

- [ ] **Step 3: Create app config files**

Create `apps/console/package.json`:

```json
{
  "name": "console",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "lint": "eslint .",
    "preview": "vite preview"
  }
}
```

Create `apps/console/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>TicketBottle Console</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

Create `apps/console/.env`:

```
VITE_API_URL=/api
```

Create `apps/console/vite.config.ts`:

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react-swc';
import tsconfigPaths from 'vite-tsconfig-paths';
import { TanStackRouterVite } from '@tanstack/router-plugin/vite';
import { join } from 'path';

export default defineConfig({
  plugins: [
    TanStackRouterVite({
      routesDirectory: join(__dirname, 'src/routes'),
      generatedRouteTree: join(__dirname, 'src/routeTree.gen.ts'),
      quoteStyle: 'single',
    }),
    react(),
    tsconfigPaths(),
  ],
  server: {
    port: 5545,
    proxy: {
      '/api': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
    },
  },
});
```

Create `apps/console/tsconfig.json`:

```json
{
  "files": [],
  "references": [
    { "path": "./tsconfig.app.json" },
    { "path": "./tsconfig.node.json" }
  ]
}
```

Create `apps/console/tsconfig.app.json`:

```json
{
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.app.tsbuildinfo",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "skipLibCheck": true,
    "target": "ESNext",
    "module": "ESNext",
    "paths": { "@/*": ["./src/*"] },
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "noUncheckedSideEffectImports": true,
    "types": ["vite/client"]
  },
  "include": ["src"]
}
```

Create `apps/console/tsconfig.node.json`:

```json
{
  "compilerOptions": {
    "tsBuildInfoFile": "./node_modules/.tmp/tsconfig.node.tsbuildinfo",
    "target": "ES2022",
    "lib": ["ES2023"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "isolatedModules": true,
    "moduleDetection": "force",
    "noEmit": true,
    "strict": true
  },
  "include": ["vite.config.ts", "vitest.config.ts"]
}
```

Create `apps/console/eslint.config.js`:

```js
import js from '@eslint/js';
import globals from 'globals';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';
import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['dist', 'src/routeTree.gen.ts'] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ['**/*.{ts,tsx}'],
    languageOptions: { ecmaVersion: 2020, globals: globals.browser },
    plugins: { 'react-hooks': reactHooks, 'react-refresh': reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': ['warn', { allowConstantExport: true }],
    },
  },
);
```

- [ ] **Step 4: Create the entry point and the first two routes**

Create `apps/console/src/vite-env.d.ts`:

```ts
/// <reference types="vite/client" />
```

Create `apps/console/src/main.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createRouter } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { routeTree } from './routeTree.gen';
import { Provider } from '@/components/ui/provider';
import '@fontsource/be-vietnam-pro/400.css';
import '@fontsource/be-vietnam-pro/500.css';
import '@fontsource/be-vietnam-pro/600.css';
import '@fontsource/be-vietnam-pro/700.css';

const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <Provider forcedTheme="light">
        <RouterProvider router={router} />
      </Provider>
    </QueryClientProvider>
  </StrictMode>,
);
```

Create `apps/console/src/routes/__root.tsx`:

```tsx
import { Toaster } from '@/components/ui/toaster';
import { Box } from '@chakra-ui/react';
import { createRootRoute, Outlet } from '@tanstack/react-router';

export const Route = createRootRoute({
  component: Root,
  errorComponent: ({ error }) => <Box p={6}>Error: {String(error)}</Box>,
});

function Root() {
  return (
    <>
      <Outlet />
      <Toaster />
    </>
  );
}
```

Create `apps/console/src/routes/index.tsx` (temporary landing so the app renders; replaced in Task C2):

```tsx
import { Box, Heading, Text } from '@chakra-ui/react';
import { createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/')({
  component: () => (
    <Box p={8}>
      <Heading size="lg">TicketBottle Console</Heading>
      <Text mt={2}>Scaffold OK.</Text>
    </Box>
  ),
});
```

- [ ] **Step 5: Run the dev server and verify it renders + proxies**

Run (leave running in one terminal):
```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe/apps/console && npx vite
```
Then verify:
1. Open `http://localhost:5545` → shows "TicketBottle Console — Scaffold OK." with no console errors.
2. Confirm `apps/console/src/routeTree.gen.ts` was auto-generated by the plugin.
3. With the backend up (`make up-aws`), verify the proxy: `curl -s -X POST http://localhost:5545/api/auth/signin -H 'Content-Type: application/json' -d '{"email":"x@x.com","password":"nope"}'` returns a JSON error envelope from the gateway (not a Vite 404) — proving `/api` proxies to `localhost:3000`.

Expected: page renders; `routeTree.gen.ts` exists; proxied curl hits the gateway.

- [ ] **Step 6: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console
git commit -m "feat(console): scaffold Vite+React app with Chakra provider and API proxy"
```

---

### Task B2: API client, envelope/domain types, and auth store (with unit tests)

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/vitest.config.ts`
- Create: `apps/console/src/types/api.ts`
- Create: `apps/console/src/types/domain.ts`
- Create: `apps/console/src/store/authStore.ts`
- Create: `apps/console/src/lib/apiClient.ts`
- Test: `apps/console/src/lib/apiClient.test.ts`

**Interfaces:**
- Produces:
  - `types/api.ts`: `ApiEnvelope<T> = { success: boolean; message: string; data: T }`; `PaginationMeta = { currentPage: number; perPage: number; total: number; lastPage: number; hasNext: boolean; hasPrevious: boolean }`; `Paginated<T> = { data: T[]; meta: PaginationMeta }`.
  - `types/domain.ts`: `TokenPair`, `AuthUser`, `EventItem`, `EventConfig`, `TicketClass`, `Availability`, `OrderItem`, `Order`, `JoinQueueResult`, `PositionUpdate` (exact fields in Step 2).
  - `store/authStore.ts`: `useAuthStore` Zustand hook with `{ token: TokenPair | null; user: AuthUser | null; setToken; setUser; logout }`, persisted to `localStorage` key `console.auth`. Non-hook accessor `getAccessToken(): string | null`.
  - `lib/apiClient.ts`: `api` (axios instance) and `unwrap<T>(promise)` helper returning `T`; `apiError(e): string` normalizer. Request interceptor attaches bearer token; response interceptor returns `response.data` (the envelope).

- [ ] **Step 1: Create the vitest config**

Create `apps/console/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config';
import tsconfigPaths from 'vite-tsconfig-paths';

export default defineConfig({
  plugins: [tsconfigPaths()],
  test: {
    environment: 'node',
    globals: true,
  },
});
```

- [ ] **Step 2: Create the types**

Create `apps/console/src/types/api.ts`:

```ts
export interface ApiEnvelope<T> {
  success: boolean;
  message: string;
  data: T;
}

export interface PaginationMeta {
  currentPage: number;
  perPage: number;
  total: number;
  lastPage: number;
  hasNext: boolean;
  hasPrevious: boolean;
}

export interface Paginated<T> {
  data: T[];
  meta: PaginationMeta;
}
```

Create `apps/console/src/types/domain.ts`:

```ts
export interface TokenPair {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
}

export interface AuthUser {
  id: string;
  email: string;
}

export interface EventCategory {
  id: string;
  name: string;
}

export interface EventConfig {
  id: string;
  ticketSaleStartDate: string;
  ticketSaleEndDate: string;
  isFree: boolean;
  maxAttendees: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;
}

export interface EventItem {
  id: string;
  name: string;
  description: string;
  startDate: string;
  endDate: string;
  thumbnailUrl: string;
  categories: EventCategory[];
  status: string;
  location?: { id: string; venue: string; address: string };
  config?: EventConfig;
  organizer?: { id: string; name: string; description: string; logoUrl: string };
  createdAt: string;
  updatedAt: string;
}

export interface TicketClass {
  id: string;
  eventId: string;
  name: string;
  priceCents: number;
  currency: string;
  total: number;
  startSaleAt: string;
  endSaleAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface Availability {
  availableQuantity: number;
}

export interface OrderItem {
  ticketClassId: string;
  quantity: number;
  priceCents: number;
}

export interface Order {
  id: string;
  code: string;
  eventId: string;
  userId: string;
  userFullname: string;
  userEmail: string;
  userPhone: string;
  totalAmountCents: number;
  currency: string;
  status: string;
  paymentMethod: string;
  items: OrderItem[];
  createdAt: string;
  updatedAt: string;
}

export interface JoinQueueResult {
  sessionId: string;
  position: number;
  queueLength: number;
  queuedAt: string;
  expiresAt: string;
  websocketUrl: string;
}

export interface PositionUpdate {
  position: number;
  queueLength: number;
  status: string;
  checkoutUrl: string;
  checkoutToken: string;
  updatedAt: string;
}
```

- [ ] **Step 3: Create the auth store**

Create `apps/console/src/store/authStore.ts`:

```ts
import { create } from 'zustand';
import type { AuthUser, TokenPair } from '@/types/domain';

const STORAGE_KEY = 'console.auth';

interface PersistedAuth {
  token: TokenPair | null;
  user: AuthUser | null;
}

function load(): PersistedAuth {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { token: null, user: null };
    return JSON.parse(raw) as PersistedAuth;
  } catch {
    return { token: null, user: null };
  }
}

function persist(state: PersistedAuth) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
}

interface AuthState extends PersistedAuth {
  setToken: (token: TokenPair | null) => void;
  setUser: (user: AuthUser | null) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  ...load(),
  setToken: (token) => {
    set({ token });
    persist({ token, user: get().user });
  },
  setUser: (user) => {
    set({ user });
    persist({ token: get().token, user });
  },
  logout: () => {
    set({ token: null, user: null });
    localStorage.removeItem(STORAGE_KEY);
  },
}));

// Non-hook accessor for the axios interceptor.
export function getAccessToken(): string | null {
  return useAuthStore.getState().token?.accessToken ?? null;
}
```

- [ ] **Step 4: Write the failing test for the API client helpers**

Create `apps/console/src/lib/apiClient.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { apiError } from './apiClient';

describe('apiError', () => {
  it('extracts the gateway envelope message', () => {
    const axiosLike = {
      isAxiosError: true,
      response: { data: { message: 'Invalid credentials', success: false } },
    };
    expect(apiError(axiosLike)).toBe('Invalid credentials');
  });

  it('falls back to a generic message when none present', () => {
    expect(apiError(new Error('boom'))).toBe('boom');
    expect(apiError({})).toBe('Something went wrong');
  });
});
```

- [ ] **Step 5: Run the test to verify it fails**

Run: `cd apps/console && npx vitest run src/lib/apiClient.test.ts`
Expected: FAIL — cannot import `apiError` (module not created yet).

- [ ] **Step 6: Implement the API client**

Create `apps/console/src/lib/apiClient.ts`:

```ts
import axios, { AxiosError } from 'axios';
import { getAccessToken, useAuthStore } from '@/store/authStore';
import type { ApiEnvelope } from '@/types/api';

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/api',
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use((config) => {
  const token = getAccessToken();
  if (token) config.headers['Authorization'] = 'Bearer ' + token;
  return config;
});

api.interceptors.response.use(
  (response) => response.data, // unwrap to the { success, message, data } envelope
  (error: AxiosError) => {
    if (error.response?.status === 401) {
      useAuthStore.getState().logout();
    }
    return Promise.reject(error);
  },
);

// Given a request promise that resolves to an ApiEnvelope<T>, return T.
export async function unwrap<T>(p: Promise<unknown>): Promise<T> {
  const env = (await p) as ApiEnvelope<T>;
  return env.data;
}

export function apiError(e: unknown): string {
  const anyE = e as { isAxiosError?: boolean; response?: { data?: { message?: string } }; message?: string };
  if (anyE?.response?.data?.message) return anyE.response.data.message;
  if (anyE?.message) return anyE.message;
  return 'Something went wrong';
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd apps/console && npx vitest run src/lib/apiClient.test.ts`
Expected: PASS (2 tests).

- [ ] **Step 8: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console
git commit -m "feat(console): api client, domain/envelope types, and auth store"
```

---

## PHASE C — Auth flow

### Task C1: Auth + users API modules

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/api/auth.api.ts`
- Create: `apps/console/src/api/users.api.ts`

**Interfaces:**
- Consumes: `api`, `unwrap` from `@/lib/apiClient`; domain types.
- Produces `authApi`:
  - `signup(body: { firstName; lastName; email; password }): Promise<TokenPair>`
  - `signin(body: { email; password }): Promise<TokenPair>`
  - `me(): Promise<AuthUser>`
  - `logout(refreshToken: string): Promise<void>`
  - `changePassword(body: { oldPassword; newPassword }): Promise<void>`
- Produces `usersApi.update(id: string, body: { firstName?; lastName?; avatar? }): Promise<AuthUser & { firstName?: string; lastName?: string; avatar?: string }>`

- [ ] **Step 1: Create the auth API module**

Create `apps/console/src/api/auth.api.ts`:

```ts
import { api, unwrap } from '@/lib/apiClient';
import type { AuthUser, TokenPair } from '@/types/domain';

export const authApi = {
  signup: (body: { firstName: string; lastName: string; email: string; password: string }) =>
    unwrap<TokenPair>(api.post('/auth/signup', body)),

  signin: (body: { email: string; password: string }) =>
    unwrap<TokenPair>(api.post('/auth/signin', body)),

  me: () => unwrap<AuthUser>(api.get('/auth/me')),

  logout: (refreshToken: string) => unwrap<void>(api.post('/auth/logout', { refreshToken })),

  changePassword: (body: { oldPassword: string; newPassword: string }) =>
    unwrap<void>(api.post('/auth/change-password', body)),
};
```

- [ ] **Step 2: Create the users API module**

Create `apps/console/src/api/users.api.ts`:

```ts
import { api, unwrap } from '@/lib/apiClient';

export interface UpdateUserResult {
  id: string;
  email: string;
  firstName?: string;
  lastName?: string;
  avatar?: string;
}

export const usersApi = {
  update: (id: string, body: { firstName?: string; lastName?: string; avatar?: string }) =>
    unwrap<UpdateUserResult>(api.patch(`/users/${id}`, body)),
};
```

- [ ] **Step 3: Typecheck**

Run: `cd apps/console && npx tsc -b`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src/api
git commit -m "feat(console): auth and users API modules"
```

---

### Task C2: Login/signup screens, auth-guard layout, and nav

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/components/NavBar.tsx`
- Create: `apps/console/src/routes/_app.tsx` (pathless auth-guard layout)
- Create: `apps/console/src/routes/login.tsx`
- Create: `apps/console/src/routes/signup.tsx`
- Modify: `apps/console/src/routes/index.tsx` (redirect to `/events`)
- Create: `apps/console/src/routes/_app/index.tsx`

**Interfaces:**
- Consumes: `authApi`, `useAuthStore`, `apiError`, `toaster`.
- Produces: routes `/login`, `/signup`; a pathless `_app` layout that redirects unauthenticated users to `/login?redirect=<path>` via `beforeLoad` and renders `<NavBar/>` + `<Outlet/>`. `/` redirects to `/events`. `_app/index.tsx` also redirects to `/events`.

- [ ] **Step 1: Create the NavBar**

Create `apps/console/src/components/NavBar.tsx`:

```tsx
import { useAuthStore } from '@/store/authStore';
import { Button, Flex, HStack, Text } from '@chakra-ui/react';
import { Link, useNavigate } from '@tanstack/react-router';

export function NavBar() {
  const user = useAuthStore((s) => s.user);
  const logout = useAuthStore((s) => s.logout);
  const navigate = useNavigate();

  return (
    <Flex align="center" justify="space-between" px={6} py={3} borderBottomWidth="1px" bg="gray.50">
      <HStack gap={4}>
        <Text fontWeight="bold">TicketBottle Console</Text>
        <Link to="/events">Events</Link>
        <Link to="/orders">My Orders</Link>
        <Link to="/profile">Profile</Link>
      </HStack>
      <HStack gap={3}>
        {user && <Text fontSize="sm" color="gray.600">{user.email}</Text>}
        <Button size="sm" variant="outline" onClick={() => { logout(); navigate({ to: '/login' }); }}>
          Log out
        </Button>
      </HStack>
    </Flex>
  );
}
```

- [ ] **Step 2: Create the auth-guard layout**

Create `apps/console/src/routes/_app.tsx`:

```tsx
import { NavBar } from '@/components/NavBar';
import { useAuthStore } from '@/store/authStore';
import { Box } from '@chakra-ui/react';
import { createFileRoute, Outlet, redirect } from '@tanstack/react-router';

export const Route = createFileRoute('/_app')({
  beforeLoad: ({ location }) => {
    const token = useAuthStore.getState().token;
    if (!token) {
      throw redirect({ to: '/login', search: { redirect: location.href } });
    }
  },
  component: () => (
    <Box>
      <NavBar />
      <Box maxW="960px" mx="auto" p={6}>
        <Outlet />
      </Box>
    </Box>
  ),
});
```

- [ ] **Step 3: Create the login screen**

Create `apps/console/src/routes/login.tsx`:

```tsx
import { authApi } from '@/api/auth.api';
import { apiError } from '@/lib/apiClient';
import { useAuthStore } from '@/store/authStore';
import { toaster } from '@/components/ui/toaster';
import { Box, Button, Heading, Input, Stack, Text } from '@chakra-ui/react';
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

export const Route = createFileRoute('/login')({
  validateSearch: (s: Record<string, unknown>) => ({ redirect: (s.redirect as string) || '/events' }),
  component: Login,
});

function Login() {
  const { redirect } = Route.useSearch();
  const navigate = useNavigate();
  const setToken = useAuthStore((s) => s.setToken);
  const setUser = useAuthStore((s) => s.setUser);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const token = await authApi.signin({ email, password });
      setToken(token);
      const me = await authApi.me();
      setUser(me);
      navigate({ to: redirect });
    } catch (err) {
      toaster.create({ title: apiError(err), type: 'error' });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Box maxW="400px" mx="auto" mt="10vh" p={6} borderWidth="1px" borderRadius="md">
      <Heading size="lg" mb={4}>Sign in</Heading>
      <form onSubmit={onSubmit}>
        <Stack gap={3}>
          <Input placeholder="Email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          <Input placeholder="Password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          <Button type="submit" loading={busy} colorPalette="blue">Sign in</Button>
        </Stack>
      </form>
      <Text mt={4} fontSize="sm">No account? <Link to="/signup">Sign up</Link></Text>
    </Box>
  );
}
```

- [ ] **Step 4: Create the signup screen**

Create `apps/console/src/routes/signup.tsx`:

```tsx
import { authApi } from '@/api/auth.api';
import { apiError } from '@/lib/apiClient';
import { useAuthStore } from '@/store/authStore';
import { toaster } from '@/components/ui/toaster';
import { Box, Button, Heading, Input, Stack, Text } from '@chakra-ui/react';
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

export const Route = createFileRoute('/signup')({ component: Signup });

function Signup() {
  const navigate = useNavigate();
  const setToken = useAuthStore((s) => s.setToken);
  const setUser = useAuthStore((s) => s.setUser);
  const [form, setForm] = useState({ firstName: '', lastName: '', email: '', password: '' });
  const [busy, setBusy] = useState(false);

  const upd = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const token = await authApi.signup(form);
      setToken(token);
      const me = await authApi.me();
      setUser(me);
      navigate({ to: '/events' });
    } catch (err) {
      toaster.create({ title: apiError(err), type: 'error' });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Box maxW="400px" mx="auto" mt="10vh" p={6} borderWidth="1px" borderRadius="md">
      <Heading size="lg" mb={4}>Sign up</Heading>
      <form onSubmit={onSubmit}>
        <Stack gap={3}>
          <Input placeholder="First name" value={form.firstName} onChange={upd('firstName')} required />
          <Input placeholder="Last name" value={form.lastName} onChange={upd('lastName')} required />
          <Input placeholder="Email" type="email" value={form.email} onChange={upd('email')} required />
          <Input placeholder="Password (strong)" type="password" value={form.password} onChange={upd('password')} required />
          <Button type="submit" loading={busy} colorPalette="blue">Create account</Button>
        </Stack>
      </form>
      <Text mt={4} fontSize="sm">Have an account? <Link to="/login">Sign in</Link></Text>
    </Box>
  );
}
```

> Note: gateway signup requires a **strong** password (`@IsStrongPassword()`): mix upper/lower/number/symbol, e.g. `Str0ng!Pass1`.

- [ ] **Step 5: Redirect the index routes to /events**

Replace `apps/console/src/routes/index.tsx`:

```tsx
import { createFileRoute, redirect } from '@tanstack/react-router';

export const Route = createFileRoute('/')({
  beforeLoad: () => { throw redirect({ to: '/events' }); },
});
```

Create `apps/console/src/routes/_app/index.tsx`:

```tsx
import { createFileRoute, redirect } from '@tanstack/react-router';

export const Route = createFileRoute('/_app/')({
  beforeLoad: () => { throw redirect({ to: '/events' }); },
});
```

- [ ] **Step 6: Verify the auth flow end-to-end**

With backend up and `npx vite` running:
1. Open `http://localhost:5545/` → you are redirected to `/login` (unauthenticated, because `/events` is under `_app`).
2. Click "Sign up", register with a strong password → lands on `/events` (will 404/empty until Phase D, that's fine) and NavBar shows your email.
3. Reload the page → you stay authenticated (token persisted).
4. Click "Log out" → back to `/login`; visiting `/orders` redirects to `/login?redirect=/orders`.

Expected: all four behaviors hold; no console errors on login/signup.

- [ ] **Step 7: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src
git commit -m "feat(console): login/signup screens, auth-guard layout, navbar"
```

---

## PHASE D — Events: browse, detail, admin create, ticket classes

### Task D1: Events + inventory API modules and the events list screen

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/api/events.api.ts`
- Create: `apps/console/src/api/inventory.api.ts`
- Create: `apps/console/src/routes/_app/events/index.tsx`

**Interfaces:**
- Produces `eventsApi`:
  - `list(params: { page?; limit?; searchQuery? }): Promise<Paginated<EventItem>>`
  - `get(id: string): Promise<EventItem>`
  - `create(body: CreateEventBody): Promise<EventItem>`
  - `createConfig(id: string, body: CreateConfigBody): Promise<EventConfig>`
  - `getConfig(id: string): Promise<EventConfig>`
  - Exported types `CreateEventBody`, `CreateConfigBody`.
- Produces `inventoryApi`:
  - `listTicketClasses(eventId: string): Promise<TicketClass[]>`
  - `getAvailability(ticketClassId: string): Promise<Availability>`
  - `createTicketClass(body: CreateTicketClassBody): Promise<TicketClass>` with exported `CreateTicketClassBody`.

- [ ] **Step 1: Create the events API module**

Create `apps/console/src/api/events.api.ts`:

```ts
import { api, unwrap } from '@/lib/apiClient';
import type { Paginated } from '@/types/api';
import type { EventConfig, EventItem } from '@/types/domain';

export interface CreateEventBody {
  name: string;
  description: string;
  startDate: string;
  endDate: string;
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

export interface CreateConfigBody {
  ticketSaleStartDate: string;
  ticketSaleEndDate: string;
  isFree: boolean;
  maxAttendees: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;
}

export const eventsApi = {
  list: (params: { page?: number; limit?: number; searchQuery?: string }) =>
    unwrap<Paginated<EventItem>>(api.get('/events', { params })),

  get: (id: string) => unwrap<EventItem>(api.get(`/events/${id}`)),

  create: (body: CreateEventBody) => unwrap<EventItem>(api.post('/events', body)),

  createConfig: (id: string, body: CreateConfigBody) =>
    unwrap<EventConfig>(api.post(`/events/${id}/config`, body)),

  getConfig: (id: string) => unwrap<EventConfig>(api.get(`/events/${id}/config`)),
};
```

- [ ] **Step 2: Create the inventory API module**

Create `apps/console/src/api/inventory.api.ts`:

```ts
import { api, unwrap } from '@/lib/apiClient';
import type { Availability, TicketClass } from '@/types/domain';

export interface CreateTicketClassBody {
  eventId: string;
  name: string;
  priceCents: number;
  currency: string;
  total: number;
  startSaleAt: string;
  endSaleAt: string;
}

export const inventoryApi = {
  listTicketClasses: (eventId: string) =>
    unwrap<TicketClass[]>(api.get(`/inventory/events/${eventId}/ticket-classes`)),

  getAvailability: (ticketClassId: string) =>
    unwrap<Availability>(api.get(`/inventory/ticket-classes/${ticketClassId}/availability`)),

  createTicketClass: (body: CreateTicketClassBody) =>
    unwrap<TicketClass>(api.post('/inventory/ticket-classes', body)),
};
```

- [ ] **Step 3: Create the events list screen**

Create `apps/console/src/routes/_app/events/index.tsx`:

```tsx
import { eventsApi } from '@/api/events.api';
import { apiError } from '@/lib/apiClient';
import { Box, Button, Flex, Heading, Input, Spinner, Stack, Table, Text } from '@chakra-ui/react';
import { useQuery } from '@tanstack/react-query';
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

export const Route = createFileRoute('/_app/events/')({ component: EventsList });

function EventsList() {
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');

  const q = useQuery({
    queryKey: ['events', query],
    queryFn: () => eventsApi.list({ page: 1, limit: 50, searchQuery: query || undefined }),
  });

  return (
    <Stack gap={4}>
      <Flex justify="space-between" align="center">
        <Heading size="lg">Events</Heading>
        <Button colorPalette="blue" onClick={() => navigate({ to: '/events/new' })}>New event</Button>
      </Flex>

      <form onSubmit={(e) => { e.preventDefault(); setQuery(search); }}>
        <Flex gap={2}>
          <Input placeholder="Search events…" value={search} onChange={(e) => setSearch(e.target.value)} />
          <Button type="submit">Search</Button>
        </Flex>
      </form>

      {q.isLoading && <Spinner />}
      {q.isError && <Text color="red.500">{apiError(q.error)}</Text>}
      {q.data && q.data.data.length === 0 && <Text color="gray.500">No events found.</Text>}

      {q.data && q.data.data.length > 0 && (
        <Table.Root size="sm" variant="line">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeader>Name</Table.ColumnHeader>
              <Table.ColumnHeader>Status</Table.ColumnHeader>
              <Table.ColumnHeader>Starts</Table.ColumnHeader>
              <Table.ColumnHeader></Table.ColumnHeader>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {q.data.data.map((ev) => (
              <Table.Row key={ev.id}>
                <Table.Cell>{ev.name}</Table.Cell>
                <Table.Cell>{ev.status}</Table.Cell>
                <Table.Cell>{new Date(ev.startDate).toLocaleString()}</Table.Cell>
                <Table.Cell>
                  <Link to="/events/$eventId" params={{ eventId: ev.id }}>Open</Link>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
      <Box />
    </Stack>
  );
}
```

- [ ] **Step 4: Verify**

With backend up and dev server running, sign in, go to `/events`:
- The list loads (may be empty if no events exist yet — that's expected; Task D3 creates one).
- Type a query + Search re-fetches without errors.
- `curl` cross-check the raw shape once: `curl -s 'http://localhost:5545/api/events?page=1&limit=5' -H "Authorization: Bearer $TOKEN"` returns `{"success":true,...,"data":{"data":[...],"meta":{...}}}`, confirming the `Paginated` unwrap is correct.

Expected: list renders, no console errors, envelope shape matches.

- [ ] **Step 5: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src
git commit -m "feat(console): events + inventory API modules and events list screen"
```

---

### Task D2: Create-event (with config) admin screen

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/routes/_app/events/new.tsx`

**Interfaces:**
- Consumes: `eventsApi.create`, `eventsApi.createConfig`.
- Produces: route `/events/new` that creates an event, then immediately creates its config, then navigates to `/events/$eventId`.

- [ ] **Step 1: Create the new-event screen**

Create `apps/console/src/routes/_app/events/new.tsx`:

```tsx
import { eventsApi } from '@/api/events.api';
import { apiError } from '@/lib/apiClient';
import { toaster } from '@/components/ui/toaster';
import { Box, Button, Checkbox, Heading, Input, Stack, Text, Textarea } from '@chakra-ui/react';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

export const Route = createFileRoute('/_app/events/new')({ component: NewEvent });

function isoInDays(days: number) {
  return new Date(Date.now() + days * 86400000).toISOString();
}

function NewEvent() {
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);
  const [ev, setEv] = useState({
    name: 'Demo Concert',
    description: 'A demo event created from the console.',
    startDate: isoInDays(14),
    endDate: isoInDays(14),
    thumbnailUrl: 'https://picsum.photos/seed/tb/800/400',
    venue: 'Main Arena',
    street: '1 Demo St',
    city: 'Hanoi',
    country: 'VN',
    organizerName: 'Demo Org',
    organizerDescription: 'We run demos.',
    organizerLogoUrl: 'https://picsum.photos/seed/org/200/200',
  });
  const [cfg, setCfg] = useState({
    ticketSaleStartDate: isoInDays(0),
    ticketSaleEndDate: isoInDays(13),
    isFree: false,
    maxAttendees: 500,
    isPublic: true,
    requiresApproval: false,
    allowWaitRoom: true,
    isNewTrending: false,
  });

  const evField = (k: keyof typeof ev) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    setEv((s) => ({ ...s, [k]: e.target.value }));

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const created = await eventsApi.create({ ...ev, categoryIds: [] });
      await eventsApi.createConfig(created.id, cfg);
      toaster.create({ title: 'Event created', type: 'success' });
      navigate({ to: '/events/$eventId', params: { eventId: created.id } });
    } catch (err) {
      toaster.create({ title: apiError(err), type: 'error' });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Box>
      <Heading size="lg" mb={4}>New event</Heading>
      <form onSubmit={onSubmit}>
        <Stack gap={3}>
          <Text fontWeight="semibold">Event</Text>
          <Input placeholder="Name" value={ev.name} onChange={evField('name')} required />
          <Textarea placeholder="Description" value={ev.description} onChange={evField('description')} required />
          <Text fontSize="sm" color="gray.500">Start / End (ISO, must be full datetime)</Text>
          <Input value={ev.startDate} onChange={evField('startDate')} required />
          <Input value={ev.endDate} onChange={evField('endDate')} required />
          <Input placeholder="Thumbnail URL" value={ev.thumbnailUrl} onChange={evField('thumbnailUrl')} required />
          <Input placeholder="Venue" value={ev.venue} onChange={evField('venue')} required />
          <Input placeholder="Street" value={ev.street} onChange={evField('street')} required />
          <Input placeholder="City" value={ev.city} onChange={evField('city')} required />
          <Input placeholder="Country" value={ev.country} onChange={evField('country')} required />
          <Input placeholder="Organizer name" value={ev.organizerName} onChange={evField('organizerName')} required />
          <Input placeholder="Organizer description" value={ev.organizerDescription} onChange={evField('organizerDescription')} required />
          <Input placeholder="Organizer logo URL" value={ev.organizerLogoUrl} onChange={evField('organizerLogoUrl')} required />

          <Text fontWeight="semibold" mt={2}>Config</Text>
          <Input placeholder="Ticket sale start (ISO)" value={cfg.ticketSaleStartDate} onChange={(e) => setCfg((s) => ({ ...s, ticketSaleStartDate: e.target.value }))} required />
          <Input placeholder="Ticket sale end (ISO)" value={cfg.ticketSaleEndDate} onChange={(e) => setCfg((s) => ({ ...s, ticketSaleEndDate: e.target.value }))} required />
          <Input placeholder="Max attendees" type="number" value={cfg.maxAttendees} onChange={(e) => setCfg((s) => ({ ...s, maxAttendees: Number(e.target.value) }))} required />
          <Checkbox.Root checked={cfg.allowWaitRoom} onCheckedChange={(d) => setCfg((s) => ({ ...s, allowWaitRoom: !!d.checked }))}>
            <Checkbox.HiddenInput /><Checkbox.Control /><Checkbox.Label>Allow wait room</Checkbox.Label>
          </Checkbox.Root>
          <Checkbox.Root checked={cfg.isPublic} onCheckedChange={(d) => setCfg((s) => ({ ...s, isPublic: !!d.checked }))}>
            <Checkbox.HiddenInput /><Checkbox.Control /><Checkbox.Label>Public</Checkbox.Label>
          </Checkbox.Root>
          <Checkbox.Root checked={cfg.isFree} onCheckedChange={(d) => setCfg((s) => ({ ...s, isFree: !!d.checked }))}>
            <Checkbox.HiddenInput /><Checkbox.Control /><Checkbox.Label>Free</Checkbox.Label>
          </Checkbox.Root>

          <Button type="submit" colorPalette="blue" loading={busy}>Create event + config</Button>
        </Stack>
      </form>
    </Box>
  );
}
```

> The gateway's `CreateEventDto` requires `startDate`/`endDate` as full `@IsDateString()` (ISO datetime), and `CreateConfigDto` requires ISO datetime for sale dates. Keep the ISO defaults; if the backend rejects a value it surfaces as a toast.

- [ ] **Step 2: Verify**

Sign in → `/events` → "New event" → submit with defaults.
Expected: a success toast, redirect to the new event's detail page (blank sections until D3, that's fine), and the event now appears in `/events`. If validation fails, the toast shows the gateway message; fix the offending field and retry.

- [ ] **Step 3: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src/routes/_app/events/new.tsx
git commit -m "feat(console): admin create-event + config screen"
```

---

### Task D3: Event detail — info, config, ticket classes with availability, and add-ticket-class

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/components/TicketClassRow.tsx`
- Create: `apps/console/src/routes/_app/events/$eventId.tsx`

**Interfaces:**
- Consumes: `eventsApi.get`, `inventoryApi.listTicketClasses`, `inventoryApi.getAvailability`, `inventoryApi.createTicketClass`.
- Produces: route `/events/$eventId` showing event info + config, a table of ticket classes each with a live availability count, an "Add ticket class" form (admin), and a "Buy tickets" button linking to `/buy/$eventId`.

- [ ] **Step 1: Create the availability-aware ticket class row**

Create `apps/console/src/components/TicketClassRow.tsx`:

```tsx
import { inventoryApi } from '@/api/inventory.api';
import type { TicketClass } from '@/types/domain';
import { Table, Text } from '@chakra-ui/react';
import { useQuery } from '@tanstack/react-query';

export function TicketClassRow({ tc }: { tc: TicketClass }) {
  const avail = useQuery({
    queryKey: ['availability', tc.id],
    queryFn: () => inventoryApi.getAvailability(tc.id),
    refetchInterval: 5000,
  });

  return (
    <Table.Row>
      <Table.Cell>{tc.name}</Table.Cell>
      <Table.Cell>{(tc.priceCents / 100).toFixed(2)} {tc.currency}</Table.Cell>
      <Table.Cell>{tc.total}</Table.Cell>
      <Table.Cell>
        {avail.isLoading ? '…' : <Text>{avail.data?.availableQuantity ?? '—'}</Text>}
      </Table.Cell>
    </Table.Row>
  );
}
```

- [ ] **Step 2: Create the event detail screen**

Create `apps/console/src/routes/_app/events/$eventId.tsx`:

```tsx
import { eventsApi } from '@/api/events.api';
import { inventoryApi } from '@/api/inventory.api';
import { apiError } from '@/lib/apiClient';
import { toaster } from '@/components/ui/toaster';
import { TicketClassRow } from '@/components/TicketClassRow';
import { Box, Button, Flex, Heading, Input, Spinner, Stack, Table, Text } from '@chakra-ui/react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useState } from 'react';

export const Route = createFileRoute('/_app/events/$eventId')({ component: EventDetail });

function isoInDays(days: number) {
  return new Date(Date.now() + days * 86400000).toISOString();
}

function EventDetail() {
  const { eventId } = Route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const eventQ = useQuery({ queryKey: ['event', eventId], queryFn: () => eventsApi.get(eventId) });
  const tcQ = useQuery({ queryKey: ['ticket-classes', eventId], queryFn: () => inventoryApi.listTicketClasses(eventId) });

  const [tc, setTc] = useState({ name: 'General Admission', priceCents: 15000, currency: 'USD', total: 100 });
  const createTc = useMutation({
    mutationFn: () =>
      inventoryApi.createTicketClass({
        eventId,
        name: tc.name,
        priceCents: tc.priceCents,
        currency: tc.currency,
        total: tc.total,
        startSaleAt: isoInDays(0),
        endSaleAt: isoInDays(13),
      }),
    onSuccess: () => {
      toaster.create({ title: 'Ticket class added', type: 'success' });
      qc.invalidateQueries({ queryKey: ['ticket-classes', eventId] });
    },
    onError: (err) => toaster.create({ title: apiError(err), type: 'error' }),
  });

  if (eventQ.isLoading) return <Spinner />;
  if (eventQ.isError) return <Text color="red.500">{apiError(eventQ.error)}</Text>;
  const ev = eventQ.data!;

  return (
    <Stack gap={5}>
      <Flex justify="space-between" align="center">
        <Heading size="lg">{ev.name}</Heading>
        <Button colorPalette="green" onClick={() => navigate({ to: '/buy/$eventId', params: { eventId } })}>
          Buy tickets
        </Button>
      </Flex>
      <Text color="gray.600">{ev.description}</Text>
      <Text fontSize="sm">Status: {ev.status} · {new Date(ev.startDate).toLocaleString()} → {new Date(ev.endDate).toLocaleString()}</Text>
      {ev.config && (
        <Text fontSize="sm" color="gray.600">
          Wait room: {String(ev.config.allowWaitRoom)} · Max attendees: {ev.config.maxAttendees} · Free: {String(ev.config.isFree)}
        </Text>
      )}

      <Box>
        <Heading size="md" mb={2}>Ticket classes</Heading>
        {tcQ.isLoading && <Spinner />}
        {tcQ.isError && <Text color="red.500">{apiError(tcQ.error)}</Text>}
        {tcQ.data && tcQ.data.length === 0 && <Text color="gray.500">No ticket classes yet.</Text>}
        {tcQ.data && tcQ.data.length > 0 && (
          <Table.Root size="sm" variant="line">
            <Table.Header>
              <Table.Row>
                <Table.ColumnHeader>Name</Table.ColumnHeader>
                <Table.ColumnHeader>Price</Table.ColumnHeader>
                <Table.ColumnHeader>Total</Table.ColumnHeader>
                <Table.ColumnHeader>Available</Table.ColumnHeader>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {tcQ.data.map((t) => <TicketClassRow key={t.id} tc={t} />)}
            </Table.Body>
          </Table.Root>
        )}
      </Box>

      <Box borderWidth="1px" borderRadius="md" p={4}>
        <Heading size="sm" mb={3}>Add ticket class</Heading>
        <form onSubmit={(e) => { e.preventDefault(); createTc.mutate(); }}>
          <Flex gap={2} wrap="wrap">
            <Input w="200px" placeholder="Name" value={tc.name} onChange={(e) => setTc((s) => ({ ...s, name: e.target.value }))} required />
            <Input w="140px" type="number" placeholder="Price (cents)" value={tc.priceCents} onChange={(e) => setTc((s) => ({ ...s, priceCents: Number(e.target.value) }))} required />
            <Input w="100px" placeholder="Currency" value={tc.currency} onChange={(e) => setTc((s) => ({ ...s, currency: e.target.value }))} required />
            <Input w="120px" type="number" placeholder="Total" value={tc.total} onChange={(e) => setTc((s) => ({ ...s, total: Number(e.target.value) }))} required />
            <Button type="submit" loading={createTc.isPending} colorPalette="blue">Add</Button>
          </Flex>
        </form>
      </Box>
    </Stack>
  );
}
```

- [ ] **Step 3: Verify (this exercises the Phase A backend endpoints through the UI)**

Sign in → open the event created in D2 → "Add ticket class" with the defaults → submit.
Expected:
- Success toast; the ticket class appears in the table with `Price 150.00 USD`, `Total 100`, and an **Available** count (from `getAvailability`, refreshing every 5s).
- The detail page shows the event info + config (wait room true).

- [ ] **Step 4: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src
git commit -m "feat(console): event detail with ticket classes, live availability, add-ticket-class"
```

---

## PHASE E — Buy flow (waitroom → order → payment)

### Task E1: Verify the SSE position stream shape (and fix double-wrapping if present)

**Repos:** verification is read-only; the conditional fix is in `TicketEventPF`.

**Files (conditional):**
- Create: `services/api-gateway/src/common/decorators/response-passthrough.decorator.ts`
- Modify: `services/api-gateway/src/modules/waitroom/waitroom.controller.ts`

**Interfaces:**
- Produces certainty about the raw SSE frame shape so Task E2's parser is correct. If a fix is applied, the SSE `data:` field becomes exactly `JSON.stringify(PositionUpdateRespDto)`.

- [ ] **Step 1: Capture a raw SSE frame from a live queue**

With backend up, get a token and an event that has ticket classes, then:
```bash
# join the queue -> capture sessionId from the JSON
curl -s -X POST http://localhost:3000/api/waitroom/join -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"eventId":"'"$EVENT_ID"'"}'
# stream position for ~5s (SSE endpoint is unguarded)
curl -N http://localhost:3000/api/waitroom/position/$SESSION_ID
```
Observe the `data:` lines. Record whether each frame is:
- **(a)** `data: {"position":...,"checkoutToken":...,"status":...}` (bare DTO — good), or
- **(b)** `data: {"success":true,"message":"OK","data":{"position":...}}` (double-wrapped by the ResponseInterceptor), or
- **(c)** `data: {"data":{"position":...}}` (Nest MessageEvent wrapper only).

- [ ] **Step 2: If shape is (a) or (c), skip the fix**

Note the observed shape in the commit message / task notes. Task E2's parser handles (a) and (c) defensively. Proceed to Task E2. **Only** do Step 3 if you observed shape (b).

- [ ] **Step 3: (Only if shape (b)) Add response passthrough to the SSE route**

Create `services/api-gateway/src/common/decorators/response-passthrough.decorator.ts`:

```ts
import { SetMetadata } from '@nestjs/common';
import { RESPONSE_PASSTHROUGH_METADATA } from '@/shared/constants/system.constant';

export const ResponsePassthrough = () => SetMetadata(RESPONSE_PASSTHROUGH_METADATA, true);
```

In `services/api-gateway/src/modules/waitroom/waitroom.controller.ts`, import it and annotate only the SSE handler:

```ts
import { ResponsePassthrough } from '@/common/decorators/response-passthrough.decorator';
// ...
  @Get('position/:sessionId')
  @Sse()
  @ResponsePassthrough()
  streamPosition(@Param('sessionId') sessionId: string): Observable<any> {
    // unchanged body
```

Rebuild (`cd services/api-gateway && npm run build`) and re-run Step 1. Confirm frames are now bare DTOs (shape (a)). Commit in the `TicketEventPF` repo:
```bash
git add services/api-gateway/src/common/decorators/response-passthrough.decorator.ts services/api-gateway/src/modules/waitroom/waitroom.controller.ts
git commit -m "fix(gateway): bypass response envelope on waitroom SSE stream

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

- [ ] **Step 4: Record the confirmed shape**

Write the observed shape (a/b/c) into the buy-flow task notes so Task E2's `parsePositionFrame` default matches reality.

---

### Task E2: SSE subscribe helper + waitroom/orders API (with unit test)

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/lib/waitroom.ts`
- Test: `apps/console/src/lib/waitroom.test.ts`
- Create: `apps/console/src/api/waitroom.api.ts`
- Create: `apps/console/src/api/orders.api.ts`

**Interfaces:**
- Produces:
  - `lib/waitroom.ts`:
    - `parsePositionFrame(raw: string): PositionUpdate | null` — tolerant parser (handles bare DTO, `{data:...}`, and `{success,message,data:...}`).
    - `isAdmitted(u: PositionUpdate): boolean` — true when a non-empty `checkoutToken` is present (optionally also when `status === 'ACTIVE'`).
    - `subscribePosition(sessionId, handlers: { onUpdate; onError }): () => void` — opens an `EventSource` on `/api/waitroom/position/:sessionId`, returns an unsubscribe fn.
  - `api/waitroom.api.ts`: `waitroomApi.join(eventId): Promise<JoinQueueResult>`, `waitroomApi.leave(sessionId): Promise<{ sessionId: string; message: string }>`.
  - `api/orders.api.ts`: `ordersApi.create(body: CreateOrderBody): Promise<{ order: Order; paymentUrl: string }>`, `list`, `getByCode`, `cancel`; exported `CreateOrderBody`.

- [ ] **Step 1: Write the failing test for the parser + admit detection**

Create `apps/console/src/lib/waitroom.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { isAdmitted, parsePositionFrame } from './waitroom';

describe('parsePositionFrame', () => {
  it('parses a bare DTO frame', () => {
    const u = parsePositionFrame('{"position":3,"queueLength":10,"status":"QUEUED","checkoutUrl":"","checkoutToken":"","updatedAt":"2026-07-15T00:00:00Z"}');
    expect(u?.position).toBe(3);
    expect(u?.status).toBe('QUEUED');
  });

  it('parses a { data } wrapped frame', () => {
    const u = parsePositionFrame('{"data":{"position":1,"queueLength":2,"status":"ACTIVE","checkoutUrl":"u","checkoutToken":"tok","updatedAt":"x"}}');
    expect(u?.checkoutToken).toBe('tok');
  });

  it('parses a { success, message, data } envelope frame', () => {
    const u = parsePositionFrame('{"success":true,"message":"OK","data":{"position":0,"queueLength":0,"status":"ACTIVE","checkoutUrl":"u","checkoutToken":"tok2","updatedAt":"x"}}');
    expect(u?.checkoutToken).toBe('tok2');
  });

  it('returns null on garbage', () => {
    expect(parsePositionFrame('not json')).toBeNull();
  });
});

describe('isAdmitted', () => {
  it('is true when a checkoutToken is present', () => {
    expect(isAdmitted({ position: 0, queueLength: 0, status: 'ACTIVE', checkoutUrl: 'u', checkoutToken: 'tok', updatedAt: 'x' })).toBe(true);
  });
  it('is false while still queued without a token', () => {
    expect(isAdmitted({ position: 5, queueLength: 9, status: 'QUEUED', checkoutUrl: '', checkoutToken: '', updatedAt: 'x' })).toBe(false);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd apps/console && npx vitest run src/lib/waitroom.test.ts`
Expected: FAIL — module `./waitroom` not found.

- [ ] **Step 3: Implement the waitroom SSE helper**

Create `apps/console/src/lib/waitroom.ts`:

```ts
import type { PositionUpdate } from '@/types/domain';

// Tolerant to bare DTO, { data }, and { success, message, data } frames (see Task E1).
export function parsePositionFrame(raw: string): PositionUpdate | null {
  try {
    const obj = JSON.parse(raw) as Record<string, unknown>;
    const inner =
      obj && typeof obj === 'object' && 'data' in obj && obj.data && typeof obj.data === 'object'
        ? (obj.data as Record<string, unknown>)
        : obj;
    if (typeof inner.position === 'undefined' && typeof inner.status === 'undefined') return null;
    return {
      position: Number(inner.position ?? 0),
      queueLength: Number(inner.queueLength ?? 0),
      status: String(inner.status ?? ''),
      checkoutUrl: String(inner.checkoutUrl ?? ''),
      checkoutToken: String(inner.checkoutToken ?? ''),
      updatedAt: String(inner.updatedAt ?? ''),
    };
  } catch {
    return null;
  }
}

export function isAdmitted(u: PositionUpdate): boolean {
  return !!u.checkoutToken || u.status === 'ACTIVE';
}

export function subscribePosition(
  sessionId: string,
  handlers: { onUpdate: (u: PositionUpdate) => void; onError?: (e: Event) => void },
): () => void {
  const base = import.meta.env.VITE_API_URL || '/api';
  const es = new EventSource(`${base}/waitroom/position/${sessionId}`);
  es.onmessage = (evt) => {
    const u = parsePositionFrame(evt.data);
    if (u) handlers.onUpdate(u);
  };
  es.onerror = (e) => handlers.onError?.(e);
  return () => es.close();
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd apps/console && npx vitest run src/lib/waitroom.test.ts`
Expected: PASS (6 tests).

- [ ] **Step 5: Create the waitroom + orders API modules**

Create `apps/console/src/api/waitroom.api.ts`:

```ts
import { api, unwrap } from '@/lib/apiClient';
import type { JoinQueueResult } from '@/types/domain';

export const waitroomApi = {
  join: (eventId: string) => unwrap<JoinQueueResult>(api.post('/waitroom/join', { eventId })),
  leave: (sessionId: string) =>
    unwrap<{ sessionId: string; message: string }>(api.post('/waitroom/leave', { sessionId })),
};
```

Create `apps/console/src/api/orders.api.ts`:

```ts
import { api, unwrap } from '@/lib/apiClient';
import type { Paginated } from '@/types/api';
import type { Order } from '@/types/domain';

export interface CreateOrderBody {
  eventId: string;
  userFullname: string;
  userEmail: string;
  userPhone: string;
  paymentMethod: string;
  items: { ticketClassId: string; quantity: number }[];
  currency?: string;
  checkoutToken: string;
  redirectUrl: string;
}

export const ordersApi = {
  create: (body: CreateOrderBody) =>
    unwrap<{ order: Order; paymentUrl: string }>(api.post('/orders', body)),

  list: (params: { page?: number; limit?: number; eventId?: string; status?: string }) =>
    unwrap<Paginated<Order>>(api.get('/orders', { params })),

  getByCode: (code: string) => unwrap<Order>(api.get(`/orders/code/${code}`)),

  cancel: (id: string) => unwrap<void>(api.delete(`/orders/${id}`)),
};
```

- [ ] **Step 6: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src
git commit -m "feat(console): waitroom SSE helper (tested) + waitroom/orders API modules"
```

---

### Task E3: Buy screen — queue, live position, auto-create order, redirect to payment

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/routes/_app/buy/$eventId.tsx`

**Interfaces:**
- Consumes: `inventoryApi.listTicketClasses`, `waitroomApi.join`, `subscribePosition`, `isAdmitted`, `ordersApi.create`, `useAuthStore`.
- Produces: route `/buy/$eventId` implementing: select quantities + contact info → Join queue → live position via SSE → on admission auto-`POST /orders` → `window.location.href = paymentUrl`.

- [ ] **Step 1: Create the buy screen**

Create `apps/console/src/routes/_app/buy/$eventId.tsx`:

```tsx
import { inventoryApi } from '@/api/inventory.api';
import { ordersApi } from '@/api/orders.api';
import { waitroomApi } from '@/api/waitroom.api';
import { apiError } from '@/lib/apiClient';
import { isAdmitted, subscribePosition } from '@/lib/waitroom';
import { useAuthStore } from '@/store/authStore';
import { toaster } from '@/components/ui/toaster';
import { Box, Button, Heading, Input, Spinner, Stack, Table, Text } from '@chakra-ui/react';
import { useQuery } from '@tanstack/react-query';
import { createFileRoute } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';

export const Route = createFileRoute('/_app/buy/$eventId')({ component: Buy });

type Phase = 'select' | 'queuing' | 'placing' | 'done' | 'error';

function Buy() {
  const { eventId } = Route.useParams();
  const user = useAuthStore((s) => s.user);
  const tcQ = useQuery({ queryKey: ['ticket-classes', eventId], queryFn: () => inventoryApi.listTicketClasses(eventId) });

  const [qty, setQty] = useState<Record<string, number>>({});
  const [contact, setContact] = useState({ fullname: '', email: user?.email ?? '', phone: '' });
  const [paymentMethod, setPaymentMethod] = useState('stripe');
  const [phase, setPhase] = useState<Phase>('select');
  const [position, setPosition] = useState<number | null>(null);
  const [msg, setMsg] = useState('');
  const placingRef = useRef(false); // guard against multiple SSE frames triggering multiple orders

  const items = Object.entries(qty).filter(([, q]) => q > 0).map(([ticketClassId, quantity]) => ({ ticketClassId, quantity }));

  async function placeOrder(checkoutToken: string) {
    if (placingRef.current) return;
    placingRef.current = true;
    setPhase('placing');
    try {
      const res = await ordersApi.create({
        eventId,
        userFullname: contact.fullname,
        userEmail: contact.email,
        userPhone: contact.phone,
        paymentMethod,
        items,
        currency: 'USD',
        checkoutToken,
        redirectUrl: `${window.location.origin}/orders`,
      });
      setPhase('done');
      if (res.paymentUrl) {
        window.location.href = res.paymentUrl; // hand off to payment provider
      } else {
        setMsg(`Order ${res.order.code} created (no payment URL returned).`);
      }
    } catch (err) {
      setPhase('error');
      setMsg(apiError(err));
      placingRef.current = false;
    }
  }

  function startQueue() {
    if (items.length === 0) { toaster.create({ title: 'Pick at least one ticket', type: 'error' }); return; }
    if (!contact.fullname || !contact.email || !contact.phone) { toaster.create({ title: 'Fill in contact info', type: 'error' }); return; }
    setPhase('queuing');
    waitroomApi.join(eventId).then((jq) => {
      setPosition(jq.position);
      const unsub = subscribePosition(jq.sessionId, {
        onUpdate: (u) => {
          setPosition(u.position);
          if (isAdmitted(u) && u.checkoutToken) { unsub(); placeOrder(u.checkoutToken); }
        },
        onError: () => { /* EventSource auto-retries; leave as-is for the dev tool */ },
      });
      cleanupRef.current = unsub;
    }).catch((err) => { setPhase('error'); setMsg(apiError(err)); });
  }

  const cleanupRef = useRef<null | (() => void)>(null);
  useEffect(() => () => cleanupRef.current?.(), []);

  if (tcQ.isLoading) return <Spinner />;
  if (tcQ.isError) return <Text color="red.500">{apiError(tcQ.error)}</Text>;

  return (
    <Stack gap={5}>
      <Heading size="lg">Buy tickets</Heading>

      <Table.Root size="sm" variant="line">
        <Table.Header>
          <Table.Row>
            <Table.ColumnHeader>Ticket</Table.ColumnHeader>
            <Table.ColumnHeader>Price</Table.ColumnHeader>
            <Table.ColumnHeader>Qty</Table.ColumnHeader>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {tcQ.data!.map((t) => (
            <Table.Row key={t.id}>
              <Table.Cell>{t.name}</Table.Cell>
              <Table.Cell>{(t.priceCents / 100).toFixed(2)} {t.currency}</Table.Cell>
              <Table.Cell>
                <Input w="90px" type="number" min={0} disabled={phase !== 'select'}
                  value={qty[t.id] ?? 0}
                  onChange={(e) => setQty((s) => ({ ...s, [t.id]: Number(e.target.value) }))} />
              </Table.Cell>
            </Table.Row>
          ))}
        </Table.Body>
      </Table.Root>

      <Box borderWidth="1px" borderRadius="md" p={4}>
        <Heading size="sm" mb={3}>Contact</Heading>
        <Stack gap={2}>
          <Input placeholder="Full name" value={contact.fullname} disabled={phase !== 'select'} onChange={(e) => setContact((s) => ({ ...s, fullname: e.target.value }))} />
          <Input placeholder="Email" type="email" value={contact.email} disabled={phase !== 'select'} onChange={(e) => setContact((s) => ({ ...s, email: e.target.value }))} />
          <Input placeholder="Phone" value={contact.phone} disabled={phase !== 'select'} onChange={(e) => setContact((s) => ({ ...s, phone: e.target.value }))} />
          <Input placeholder="Payment method" value={paymentMethod} disabled={phase !== 'select'} onChange={(e) => setPaymentMethod(e.target.value)} />
        </Stack>
      </Box>

      {phase === 'select' && <Button colorPalette="green" onClick={startQueue}>Enter queue & buy</Button>}
      {phase === 'queuing' && <Text>In queue… position: {position ?? '…'} (waiting for checkout admission)</Text>}
      {phase === 'placing' && <Text>Admitted — placing order…</Text>}
      {phase === 'done' && <Text color="green.600">{msg || 'Redirecting to payment…'}</Text>}
      {phase === 'error' && <Text color="red.500">Error: {msg}</Text>}
    </Stack>
  );
}
```

- [ ] **Step 2: Verify the full buy flow end-to-end**

Precondition: an event with `allowWaitRoom: true` and at least one ticket class with availability (from D2/D3). Sign in → event detail → "Buy tickets".
1. Set a quantity (e.g. 1), fill full name/email/phone, leave payment method.
2. Click "Enter queue & buy".
3. Observe "In queue… position: N", then "Admitted — placing order…", then either a redirect to the returned `paymentUrl` or a green "Order … created" message.
4. If it errors, the red message shows the gateway/saga reason (e.g. availability, checkout token). Cross-check with `curl -N` from Task E1 that the SSE actually delivers a `checkoutToken`.

Expected: the flow reaches "placing" and produces an order (redirect or code). This is the headline saga (waitroom → Temporal CreateOrder → inventory reserve → payment intent) driven from the browser.

- [ ] **Step 3: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src/routes/_app/buy
git commit -m "feat(console): buy flow — queue, live SSE position, auto order, payment redirect"
```

---

## PHASE F — Orders & profile

### Task F1: My-orders list and order detail

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/routes/_app/orders/index.tsx`
- Create: `apps/console/src/routes/_app/orders/$code.tsx`

**Interfaces:**
- Consumes: `ordersApi.list`, `ordersApi.getByCode`, `ordersApi.cancel`.
- Produces: `/orders` (list of my orders, link to detail by code) and `/orders/$code` (order detail + cancel).

- [ ] **Step 1: Create the orders list screen**

Create `apps/console/src/routes/_app/orders/index.tsx`:

```tsx
import { ordersApi } from '@/api/orders.api';
import { apiError } from '@/lib/apiClient';
import { Heading, Spinner, Stack, Table, Text } from '@chakra-ui/react';
import { useQuery } from '@tanstack/react-query';
import { createFileRoute, Link } from '@tanstack/react-router';

export const Route = createFileRoute('/_app/orders/')({ component: Orders });

function Orders() {
  const q = useQuery({ queryKey: ['orders'], queryFn: () => ordersApi.list({ page: 1, limit: 50 }) });

  return (
    <Stack gap={4}>
      <Heading size="lg">My orders</Heading>
      {q.isLoading && <Spinner />}
      {q.isError && <Text color="red.500">{apiError(q.error)}</Text>}
      {q.data && q.data.data.length === 0 && <Text color="gray.500">No orders yet.</Text>}
      {q.data && q.data.data.length > 0 && (
        <Table.Root size="sm" variant="line">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeader>Code</Table.ColumnHeader>
              <Table.ColumnHeader>Status</Table.ColumnHeader>
              <Table.ColumnHeader>Total</Table.ColumnHeader>
              <Table.ColumnHeader>Created</Table.ColumnHeader>
              <Table.ColumnHeader></Table.ColumnHeader>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {q.data.data.map((o) => (
              <Table.Row key={o.id}>
                <Table.Cell>{o.code}</Table.Cell>
                <Table.Cell>{o.status}</Table.Cell>
                <Table.Cell>{(o.totalAmountCents / 100).toFixed(2)} {o.currency}</Table.Cell>
                <Table.Cell>{new Date(o.createdAt).toLocaleString()}</Table.Cell>
                <Table.Cell><Link to="/orders/$code" params={{ code: o.code }}>Open</Link></Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      )}
    </Stack>
  );
}
```

- [ ] **Step 2: Create the order detail screen**

Create `apps/console/src/routes/_app/orders/$code.tsx`:

```tsx
import { ordersApi } from '@/api/orders.api';
import { apiError } from '@/lib/apiClient';
import { toaster } from '@/components/ui/toaster';
import { Box, Button, Heading, Spinner, Stack, Table, Text } from '@chakra-ui/react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createFileRoute } from '@tanstack/react-router';

export const Route = createFileRoute('/_app/orders/$code')({ component: OrderDetail });

function OrderDetail() {
  const { code } = Route.useParams();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ['order', code], queryFn: () => ordersApi.getByCode(code) });

  const cancel = useMutation({
    mutationFn: (id: string) => ordersApi.cancel(id),
    onSuccess: () => {
      toaster.create({ title: 'Order cancelled', type: 'success' });
      qc.invalidateQueries({ queryKey: ['order', code] });
      qc.invalidateQueries({ queryKey: ['orders'] });
    },
    onError: (err) => toaster.create({ title: apiError(err), type: 'error' }),
  });

  if (q.isLoading) return <Spinner />;
  if (q.isError) return <Text color="red.500">{apiError(q.error)}</Text>;
  const o = q.data!;

  return (
    <Stack gap={4}>
      <Heading size="lg">Order {o.code}</Heading>
      <Text>Status: {o.status} · Total: {(o.totalAmountCents / 100).toFixed(2)} {o.currency}</Text>
      <Text fontSize="sm" color="gray.600">{o.userFullname} · {o.userEmail} · {o.userPhone}</Text>
      <Box>
        <Heading size="sm" mb={2}>Items</Heading>
        <Table.Root size="sm" variant="line">
          <Table.Header>
            <Table.Row>
              <Table.ColumnHeader>Ticket class</Table.ColumnHeader>
              <Table.ColumnHeader>Qty</Table.ColumnHeader>
              <Table.ColumnHeader>Price</Table.ColumnHeader>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {o.items.map((it, i) => (
              <Table.Row key={i}>
                <Table.Cell>{it.ticketClassId}</Table.Cell>
                <Table.Cell>{it.quantity}</Table.Cell>
                <Table.Cell>{(it.priceCents / 100).toFixed(2)}</Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table.Root>
      </Box>
      <Button w="fit-content" colorPalette="red" variant="outline" loading={cancel.isPending} onClick={() => cancel.mutate(o.id)}>
        Cancel order
      </Button>
    </Stack>
  );
}
```

- [ ] **Step 3: Verify**

Sign in → `/orders`:
- Any orders created via the buy flow appear.
- Click "Open" → detail shows items + status; "Cancel order" cancels (status changes / toast). Note: cancel may be rejected by the backend depending on order state — the toast surfaces the reason.

- [ ] **Step 4: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src/routes/_app/orders
git commit -m "feat(console): my-orders list and order detail with cancel"
```

---

### Task F2: Profile update

**Repo:** `ticketbottle-fe` (same branch).

**Files:**
- Create: `apps/console/src/routes/_app/profile.tsx`

**Interfaces:**
- Consumes: `useAuthStore`, `usersApi.update`.
- Produces: `/profile` showing the signed-in email (from `/auth/me`) and a write-only form to PATCH `firstName`/`lastName`/`avatar`.

- [ ] **Step 1: Create the profile screen**

Create `apps/console/src/routes/_app/profile.tsx`:

```tsx
import { usersApi } from '@/api/users.api';
import { apiError } from '@/lib/apiClient';
import { useAuthStore } from '@/store/authStore';
import { toaster } from '@/components/ui/toaster';
import { Box, Button, Heading, Input, Stack, Text } from '@chakra-ui/react';
import { createFileRoute } from '@tanstack/react-router';
import { useState } from 'react';

export const Route = createFileRoute('/_app/profile')({ component: Profile });

function Profile() {
  const user = useAuthStore((s) => s.user);
  const [form, setForm] = useState({ firstName: '', lastName: '', avatar: '' });
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!user) return;
    setBusy(true);
    try {
      const body: { firstName?: string; lastName?: string; avatar?: string } = {};
      if (form.firstName) body.firstName = form.firstName;
      if (form.lastName) body.lastName = form.lastName;
      if (form.avatar) body.avatar = form.avatar;
      await usersApi.update(user.id, body);
      toaster.create({ title: 'Profile updated', type: 'success' });
    } catch (err) {
      toaster.create({ title: apiError(err), type: 'error' });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Box maxW="480px">
      <Heading size="lg" mb={2}>Profile</Heading>
      <Text color="gray.600" mb={4}>Signed in as {user?.email} (id {user?.id})</Text>
      <Text fontSize="sm" color="gray.500" mb={3}>
        Note: the API has no get-profile endpoint, so name/avatar are write-only here.
      </Text>
      <form onSubmit={onSubmit}>
        <Stack gap={3}>
          <Input placeholder="First name" value={form.firstName} onChange={(e) => setForm((s) => ({ ...s, firstName: e.target.value }))} />
          <Input placeholder="Last name" value={form.lastName} onChange={(e) => setForm((s) => ({ ...s, lastName: e.target.value }))} />
          <Input placeholder="Avatar URL" value={form.avatar} onChange={(e) => setForm((s) => ({ ...s, avatar: e.target.value }))} />
          <Button type="submit" colorPalette="blue" loading={busy}>Save</Button>
        </Stack>
      </form>
    </Box>
  );
}
```

> The gateway's `UpdateUserDto.avatar` is validated with `@IsUrl()` — an invalid avatar URL will be rejected (shown as a toast). Leave it blank to only update names.

- [ ] **Step 2: Full-app verification (typecheck + build + smoke)**

Run:
```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe/apps/console
npx tsc -b
npx vitest run
npx vite build
```
Expected: typecheck clean, all unit tests pass, production build succeeds.

Then with backend up + `npx vite`: sign in → `/profile` → enter a first/last name → Save → success toast. Walk the full happy path once: signup → create event + config → add ticket class → buy (queue → order) → see it in `/orders` → open detail → profile update. No console errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/vogiaan/coding/projects/ticketbottle-fe
git add apps/console/src/routes/_app/profile.tsx
git commit -m "feat(console): profile update screen"
```

---

## Self-Review Notes (for the executor)

- **Chakra v3 API:** components use the v3 compound API already present in `apps/ticketbottle` (`Table.Root/Header/Body/Row/ColumnHeader/Cell`, `Checkbox.Root/Control/Label/HiddenInput`, `Button` `loading`/`colorPalette` props, `toaster.create`). If any prop name differs in the installed `@chakra-ui/react` version, cross-check against the copied `apps/ticketbottle/src/components/ui/*` and existing routes — do not upgrade/downgrade the package.
- **TanStack Router file routes:** the `_app` prefix is a *pathless* layout, so its children resolve at `/events`, `/orders`, etc. (no `/_app` in the URL). The router plugin regenerates `routeTree.gen.ts` on `vite` start; if types for `Route.useParams`/`Link params` look stale, restart the dev server.
- **SSE is the only source of `checkoutToken`** — `JoinQueueResponse` does not include it. If Task E1 shows the stream is broken/empty, the buy flow cannot complete; fix the gateway SSE (Task E1 Step 3) before blaming the frontend.
- **Order creation depends on the full saga** (Temporal + inventory reserve + payment intent). A failed order in Task E3 is often a backend/saga condition (no availability, payment provider not configured), surfaced via the toast — verify with `make status` and gateway logs, not by changing the frontend.
- **Spec coverage:** Auth (C1/C2) ✓ · Admin setup: create event+config (D2), create ticket class (D3), + backend inventory endpoints (A1/A2) ✓ · Browse + availability (D1/D3) ✓ · Buy flow waitroom→order→pay (E1/E2/E3) ✓ · My orders + detail (F1) · Profile (F2) ✓ · CORS handled via proxy (B1) ✓.
```
