# event-svc Layout Convergence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Converge `event-svc` onto the canonical archetype-aware NestJS layout from the design spec, making it the reference implementation the other three TS services will follow.

**Architecture:** Pure structural refactor — relocate the gRPC controller, transport DTOs, and mappers from `modules/events/controllers/grpc/` up to the module root; consolidate the second (domain) DTO layer into one `events.types.ts`; delete empty scaffolding; align the tsconfig alias set. No behavioural, dependency, or proto change.

**Tech Stack:** NestJS (gRPC microservice), TypeScript, Prisma, `ts-proto`-generated stubs, docker-compose (LocalStack/AWS mode).

> **Verification model (read first):** This service has **no tests** — verification is **golden-master**, not red-green (per the spec). The per-task gate is `npm run build` (nest build / tsc) staying **green**, plus targeted `grep` checks that no stale import paths remain. Final acceptance adds a **boot gate** (the service starts and serves gRPC under docker-compose), because tsc does not catch DI/asset-glob/module-wiring breakage.
>
> **Toolchain precondition:** the committed `node_modules` has broken `.bin` symlinks. Every session that builds must first run a **clean reinstall** (Task 0). A plain `npm install` does **not** repair it.
>
> **Commits:** the repo convention is Conventional Commits with a trailing `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` line. Work on the current `cleanup/repo-review` branch.

---

## File Structure (end state)

```
services/event-svc/src/modules/events/
  events.controller.ts        # moved up from controllers/grpc/ (class stays GrpcEventsController)
  events.service.ts           # unchanged code; import paths repointed
  events.module.ts            # controller import path repointed
  events.types.ts             # NEW — 8 domain interfaces (consolidated from dtos/)
  dto/                        # moved up from controllers/grpc/dtos/ (13 transport DTO files)
  mappers/                    # moved up from controllers/grpc/mappers/ (unchanged internally)
  entities/                   # unchanged
  repository/                 # unchanged code; import paths repointed
  # DELETED: controllers/ (grpc + empty http), dtos/ (old domain layer)
services/event-svc/src/infra/database/
  prisma/                     # unchanged
  # DELETED: redis/ (empty)
services/event-svc/tsconfig.json   # alias set aligned (+@protogen, -@interfaces)
CLAUDE.md (repo root)              # canonical layout convention updated
```

**Import-path changes caused by the moves (the whole risk surface):**
- Files relocate from `controllers/grpc/{events.controller.ts,dtos,mappers}` → module root. `./mappers` / `../mappers` strings stay valid (mappers also move to module root).
- The controller's `../../events.service` → `./events.service`; its `./dtos` → `./dto`.
- The 5 transport DTOs that reference the domain layer (`create-event`, `update-event`, `filter-event`, `create-config`, `update-config`) change `../../../dtos` → `../dtos` (Task 1) → `../events.types` (Task 2).
- `events.service.ts` and `repository/events.repository.ts` change their domain-DTO imports (`./dtos`, `../dtos`, and per-file paths) → `events.types` (Task 2).

---

## Task 0: Baseline — clean install + green build

**Files:** none (verification only).

- [ ] **Step 1: Clean reinstall the toolchain**

```bash
cd services/event-svc
rm -rf node_modules
npm install
```
Expected: completes with `added N packages` (audit warnings are fine).

- [ ] **Step 2: Confirm baseline build is green BEFORE any change**

```bash
cd services/event-svc
npm run build
```
Expected: PASS — no errors; `dist/` is produced. (If this fails, stop: the refactor cannot be verified until the build is green.)

- [ ] **Step 3: Snapshot the current import topology (for later diff)**

```bash
cd services/event-svc/src/modules/events
grep -rn "controllers/grpc\|'\.\./\.\./\.\./dtos'\|'\./dtos'\|'\.\./dtos'" . | sort
```
Expected: shows the controller import of `./dtos`, the 5 transport DTOs importing `../../../dtos`, and service/repository importing `./dtos` / `../dtos`. Keep this output to confirm it is empty by Task 2.

---

## Task 1: Relocate controller, transport `dto/`, and `mappers/` to the module root

**Files:**
- Move: `services/event-svc/src/modules/events/controllers/grpc/events.controller.ts` → `events.controller.ts`
- Move: `services/event-svc/src/modules/events/controllers/grpc/dtos` → `dto`
- Move: `services/event-svc/src/modules/events/controllers/grpc/mappers` → `mappers`
- Modify: `events.controller.ts`, `events.module.ts`, and 5 transport DTO files

- [ ] **Step 1: Move the files with git (preserves history)**

```bash
cd services/event-svc/src/modules/events
git mv controllers/grpc/events.controller.ts events.controller.ts
git mv controllers/grpc/dtos dto
git mv controllers/grpc/mappers mappers
rmdir controllers/grpc controllers/http controllers 2>/dev/null
```
Expected: `controllers/` directory is gone; `dto/`, `mappers/`, and `events.controller.ts` now sit at the module root.

- [ ] **Step 2: Fix imports in `events.controller.ts`**

In `services/event-svc/src/modules/events/events.controller.ts`, make these exact replacements:

Replace:
```ts
import { EventsService } from '../../events.service';
```
with:
```ts
import { EventsService } from './events.service';
```

Replace:
```ts
} from './dtos';
import { CreateConfigDto } from './dtos/create-config.dto';
```
with:
```ts
} from './dto';
import { CreateConfigDto } from './dto/create-config.dto';
```

(Leave `import { EventResponseMapper } from './mappers';` unchanged — it is still correct.)

- [ ] **Step 3: Fix the controller import in `events.module.ts`**

In `services/event-svc/src/modules/events/events.module.ts`, replace:
```ts
import { GrpcEventsController } from './controllers/grpc/events.controller';
```
with:
```ts
import { GrpcEventsController } from './events.controller';
```

- [ ] **Step 4: Fix the domain-DTO path in the 5 transport DTOs (still pointing at the old domain dir)**

In each of these files under `services/event-svc/src/modules/events/dto/`:
`create-event.dto.ts`, `update-event.dto.ts`, `filter-event.dto.ts`, `create-config.dto.ts`, `update-config.dto.ts`

replace the substring `'../../../dtos'` with `'../dtos'`. For example, in `create-event.dto.ts`:
```ts
import { CreateEventDto as ServiceCreateEventDto } from '../../../dtos';
```
becomes:
```ts
import { CreateEventDto as ServiceCreateEventDto } from '../dtos';
```
(`filter-event.dto.ts` also imports `from '../mappers'` — leave that line unchanged, it is still correct.)

- [ ] **Step 5: Verify no stale `controllers/grpc` references remain**

```bash
cd services/event-svc
grep -rn "controllers/grpc" src/ || echo "CLEAN"
```
Expected: `CLEAN`.

- [ ] **Step 6: Build must stay green**

```bash
cd services/event-svc
npm run build
```
Expected: PASS, no errors.

- [ ] **Step 7: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add -A services/event-svc/src/modules/events
git commit -m "$(printf 'refactor(event-svc): flatten controller/dto/mappers to module root\n\nCo-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>')"
```

---

## Task 2: Consolidate the domain DTO layer into `events.types.ts`

**Files:**
- Create: `services/event-svc/src/modules/events/events.types.ts`
- Modify: `events.service.ts`, `repository/events.repository.ts`, the 5 transport DTOs
- Delete: `services/event-svc/src/modules/events/dtos/` (old domain layer)

> Safety: a grep confirmed **no `new <Dto>()` and no `instanceof`** on these domain classes, so converting them to interfaces is behaviour-neutral. The only class with a field initializer is `FilterEventDto.categoryIds: string[] = []`; the default is dead (never instantiated), so dropping it on the interface is safe.

- [ ] **Step 1: Create `events.types.ts` with the 8 consolidated domain interfaces**

Create `services/event-svc/src/modules/events/events.types.ts`:
```ts
import { EventRoleType, EventStatus } from '@prisma/client';

export interface CreateCategoryDto {
  name: string;
}

export interface CreateConfigDto {
  eventId: string;
  ticketSaleStartDate: Date;
  ticketSaleEndDate: Date;
  isFree: boolean;
  maxAttendees: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;
}

export interface CreateEventDto {
  createdBy: string;
  name: string;
  description: string;
  startDate: Date;
  endDate: Date;
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

export interface CreateEventRoleDto {
  userId: string;
  eventId: string;
  role: EventRoleType;
}

export interface FilterEventDto {
  searchQuery?: string;
  categoryIds: string[];
  status?: EventStatus;
  organizerId?: string;
  userId?: string;
  startDateFrom?: Date;
  startDateTo?: Date;
  city?: string;
  country?: string;
  isPublic?: boolean;
  isFree?: boolean;
}

export interface FindManyEventDto {
  page: number;
  pageSize: number;
  filter: FilterEventDto;
}

export interface UpdateConfigDto {
  eventId: string;
  ticketSaleStartDate: Date;
  ticketSaleEndDate: Date;
  isFree: boolean;
  maxAttendees: number;
  isPublic: boolean;
  requiresApproval: boolean;
  allowWaitRoom: boolean;
  isNewTrending: boolean;
}

export interface UpdateEventDto {
  name?: string;
  description?: string;
  startDate?: Date;
  endDate?: Date;
  status?: EventStatus;
  thumbnailUrl?: string;
  venue?: string;
  street?: string;
  city?: string;
  country?: string;
  ward?: string;
  district?: string;
  categoryIds?: string[];
  organizerName?: string;
  organizerDescription?: string;
  organizerLogoUrl?: string;
}
```

- [ ] **Step 2: Repoint `events.service.ts` imports**

In `services/event-svc/src/modules/events/events.service.ts`:

Replace:
```ts
import { CreateEventDto, FilterEventDto, UpdateConfigDto } from './dtos';
```
with:
```ts
import {
  CreateConfigDto,
  CreateEventDto,
  FilterEventDto,
  UpdateConfigDto,
  UpdateEventDto,
} from './events.types';
```

Delete this line:
```ts
import { UpdateEventDto } from './dtos/update-event.dto';
```

Delete this line:
```ts
import { CreateConfigDto } from './dtos/create-config.dto';
```

- [ ] **Step 3: Repoint `repository/events.repository.ts` imports**

In `services/event-svc/src/modules/events/repository/events.repository.ts`:

Replace:
```ts
import { CreateCategoryDto, CreateEventDto, FilterEventDto, UpdateConfigDto } from '../dtos';
```
with:
```ts
import {
  CreateCategoryDto,
  CreateConfigDto,
  CreateEventDto,
  CreateEventRoleDto,
  FilterEventDto,
  UpdateConfigDto,
  UpdateEventDto,
} from '../events.types';
```

Delete these three lines:
```ts
import { CreateConfigDto } from '../dtos/create-config.dto';
import { CreateEventRoleDto } from '../dtos/create-role.dto';
import { UpdateEventDto } from '../dtos/update-event.dto';
```

- [ ] **Step 4: Repoint the 5 transport DTOs to `events.types`**

In each of `services/event-svc/src/modules/events/dto/`: `create-event.dto.ts`, `update-event.dto.ts`, `filter-event.dto.ts`, `create-config.dto.ts`, `update-config.dto.ts`, replace the substring `'../dtos'` with `'../events.types'`. For example:
```ts
import { CreateEventDto as ServiceCreateEventDto } from '../dtos';
```
becomes:
```ts
import { CreateEventDto as ServiceCreateEventDto } from '../events.types';
```

- [ ] **Step 5: Delete the old domain DTO directory**

```bash
cd services/event-svc/src/modules/events
git rm -r dtos
```
Expected: removes the 8 domain `*.dto.ts` files + `index.ts`.

- [ ] **Step 6: Verify no domain-DTO references remain**

```bash
cd services/event-svc
grep -rn "'\./dtos'\|'\.\./dtos'\|/dtos/\|modules/events/dtos" src/ || echo "CLEAN"
```
Expected: `CLEAN` (the transport `dto/` singular dir must NOT match — confirm any hits are not the old plural `dtos`).

- [ ] **Step 7: Build must stay green**

```bash
cd services/event-svc
npm run build
```
Expected: PASS, no errors.

- [ ] **Step 8: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add -A services/event-svc/src/modules/events
git commit -m "$(printf 'refactor(event-svc): consolidate domain DTOs into events.types.ts\n\nCo-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>')"
```

---

## Task 3: Delete empty `redis/` scaffolding + align tsconfig aliases

**Files:**
- Delete: `services/event-svc/src/infra/database/redis/` (empty)
- Modify: `services/event-svc/tsconfig.json`

- [ ] **Step 1: Remove the empty redis dir**

```bash
cd services/event-svc
rmdir src/infra/database/redis 2>/dev/null; echo "done"
```
(Empty dirs aren't tracked by git, so there is nothing to commit for this alone — it just cleans the working tree.)

- [ ] **Step 2: Align the tsconfig alias set to the canonical six**

In `services/event-svc/tsconfig.json`, replace the `paths` block:
```json
    "paths": {
      "@/*": ["src/*"],
      "@modules/*": ["src/modules/*"],
      "@common/*": ["src/common/*"],
      "@infra/*": ["src/infra/*"],
      "@shared/*": ["src/shared/*"],
      "@interfaces/*": ["src/shared/interfaces/*"]
    }
```
with:
```json
    "paths": {
      "@/*": ["src/*"],
      "@modules/*": ["src/modules/*"],
      "@common/*": ["src/common/*"],
      "@shared/*": ["src/shared/*"],
      "@infra/*": ["src/infra/*"],
      "@protogen/*": ["src/protogen/*"]
    }
```
(`@interfaces/*` has zero usages — verified — so dropping it is safe. `@protogen/*` is added for cross-service consistency; existing `@/protogen` imports keep working.)

- [ ] **Step 3: Build must stay green**

```bash
cd services/event-svc
npm run build
```
Expected: PASS, no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add -A services/event-svc/tsconfig.json
git commit -m "$(printf 'refactor(event-svc): align tsconfig alias set; drop empty redis dir\n\nCo-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>')"
```

---

## Task 4: Acceptance verification — lint + boot gate

**Files:** none (verification only). No commit.

- [ ] **Step 1: Lint check (read-only — do NOT use the `--fix` script)**

```bash
cd services/event-svc
npx eslint "src/**/*.ts"
```
Expected: no **new** errors versus baseline. (Pre-existing warnings are acceptable; a new error caused by the refactor is not — fix it before proceeding.)

- [ ] **Step 2: Boot gate — service starts and serves gRPC under docker-compose**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/development
docker compose -f docker-compose.aws.yml up -d --build postgres-event event-service
sleep 20
docker compose -f docker-compose.aws.yml ps event-service
docker compose -f docker-compose.aws.yml logs --tail=40 event-service
```
Expected: `event-service` is `Up`; logs show a successful Nest bootstrap with the gRPC server listening on `50053` and **no** module-resolution / DI / crash errors.

- [ ] **Step 3: Tear down**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/development
docker compose -f docker-compose.aws.yml down
```

---

## Task 5: Record the canonical convention in root `CLAUDE.md`

**Files:**
- Modify: `CLAUDE.md` (repo root) — the "Canonical TS layout" bullet under "Conventions that span services".

- [ ] **Step 1: Replace the canonical-layout bullet block (exact edit)**

In `CLAUDE.md` (repo root), replace this exact block (lines ~69–74):

```markdown
  - **Canonical TS layout (target convention — apply when touching a service):**
    `src/main.ts` → `src/modules/<feature>/` (controller + service + module + `dto/` + `repository.ts`) → `src/common/*` (filters, guards, interceptors, decorators) → `src/shared/*` (cross-cutting helpers) → `src/protogen/*` (generated, never hand-edited).
    - One `dto/` per module — do **not** split into parallel `dtos/req`+`dtos/resp`+`controllers/grpc/dtos` trees.
    - Prefer the generated proto types/enums directly; avoid redefining domain enums and hand-writing enum↔proto mappers.
    - Don't scaffold empty layers. A small service (e.g. `user-svc`) should not carry the same folder depth as the gateway. Collapse single-file `common/`/`shared/` folders into flat files.
  - The three TS services currently use **three different** module/DTO conventions — converging them on the above is tracked in `REVIEW.md` (P2).
```

with:

```markdown
  - **Canonical TS layout (archetype-aware — apply when touching a service):**
    `src/main.ts` → `src/modules/<feature>/` → `src/common/*` (filters, guards, interceptors, decorators) → `src/shared/*` (cross-cutting helpers) → `src/infra/*` (datastore/messaging adapters: `database/prisma`, `messaging/kafka`) → `src/protogen/*` (generated, never hand-edited). The four TS services are different archetypes (HTTP proxy `api-gateway`; domain gRPC `event-svc`/`payment-svc`; tiny gRPC `user-svc`) — converge on a **shared skeleton + archetype-specific additions**, do not force one uniform template.
    - A module is flat (`<feature>.controller.ts`, `.service.ts`, `.module.ts`) unless it genuinely serves two transports (only `payment-svc`: keep `controllers/{grpc,http}/`).
    - **DTOs:** one singular `dto/` per module = the validated **transport** contract (classes implementing the proto request, class-validator, `toServiceDto()`). Internal **domain** shapes (no decorators, e.g. `startDate: Date`) go in a single `<feature>.types.ts` as interfaces — not a second `dtos/` folder, and not `dtos/req`+`dtos/resp` trees.
    - **Keep `mappers/`** — proto enums are non-contiguous (`DRAFT=1, PUBLISHED=2, CONFIGURED=4`), so enum↔proto mappers are necessary translation, not boilerplate.
    - One tsconfig alias set everywhere: `@/ @modules/ @common/ @shared/ @infra/ @protogen/`. No hyper-granular aliases.
    - Don't scaffold empty layers; a small service does not carry the gateway's depth. Collapse single-file `common/`/`shared/` folders into flat files.
  - **Reference implementation: `event-svc`** (converged 2026-06-16; spec `docs/superpowers/specs/2026-06-16-ts-layout-convergence-design.md`). `payment-svc`, `api-gateway`, `user-svc` converge onto it in follow-on plans (REVIEW.md P2).
```

- [ ] **Step 2: Sanity-check the doc still reads correctly**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
grep -n "archetype-aware\|events.types.ts\|@protogen" CLAUDE.md
```
Expected: the new lines are present.

- [ ] **Step 3: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add CLAUDE.md
git commit -m "$(printf 'docs(claude): record archetype-aware TS layout convention\n\nCo-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>')"
```

---

## Final acceptance checklist (maps to spec §8)

- [ ] event-svc matches the canonical layout: flat `events.controller.ts`, singular `dto/`, `events.types.ts`, `mappers/`, `infra/`, the six-alias set; `controllers/` and empty `redis/` gone.
- [ ] `rm -rf node_modules && npm install && npm run build` is **green** (Task 0 + Task 3 Step 3).
- [ ] `npx eslint "src/**/*.ts"` introduces **no new** errors (Task 4 Step 1).
- [ ] event-service **boots** under docker-compose and serves gRPC on `50053` (Task 4 Step 2).
- [ ] No change under `src/protos/` or `src/protogen/`; no behavioural/dependency change (review the final diff).
- [ ] Root `CLAUDE.md` records the convention, including `infra/` and the transport-`dto/` vs domain-`types.ts` rule (Task 5).
```
