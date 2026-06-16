# TS Service Layout Convergence — Design

*Date: 2026-06-16. Status: approved for planning. Scope: the four NestJS/TypeScript services (`api-gateway`, `event-svc`, `user-svc`, `payment-svc`). This is the deferral-reversal of REVIEW.md §C / P2 "TS layout convergence."*

---

## 1. Context & motivation

REVIEW.md diagnosed the TS services as carrying **inconsistent, accidental over-layering** — three services, three different module/DTO conventions, nothing predictable. P2 **deferred** this item on the grounds that "near-zero test coverage means a layout refactor needs a test harness bigger than the refactor."

That deferral is being reversed deliberately, because the objection is weaker than it looked for a *move-and-rename* refactor:

- The change is **mechanical** (move files, collapse folders, rewrite imports), not behavioural.
- There is a real **golden-master safety net**: each service has `nest build` (tsc) + `eslint`, and a `Dockerfile` for a boot check. tsc + a clean boot catch the failure modes this refactor can introduce.

**Honest framing (keep this visible):** this is the SA-deferred, self-described *lowest-ROI* item. The payoff is **consolidation, predictability, and deletion** — not new capability. The dominant cost is not the moves; it is the per-service clean-install + boot verification. We proceed with eyes open.

### Confirmed current-state drift (evidence)

- **Feature placement:** `modules/<feature>/` (gateway, event, payment) vs bare `src/user/` (user).
- **Controller nesting:** flat `<f>.controller.ts` (gateway, user) vs `controllers/grpc/<f>.controller.ts` (event, payment).
- **DTO convention (5 different shapes):** `dtos/req`+`dtos/resp` (gateway events/orders/waitroom), flat `dtos/` (gateway users/auth), `dto/` (user), **double layer** `controllers/grpc/dtos` + a second `dtos/` (event, payment).
- **The double DTO layer is NOT redundant.** `controllers/grpc/dtos/*.dto.ts` are **transport DTOs** (`implements <Proto>Request`, class-validator decorators, `startDate: string`, a `toServiceDto()` converter). `modules/events/dtos/*.dto.ts` are **domain shapes** (`startDate: Date`, no decorators). Merging naively would destroy the validation + string→Date boundary.
- **Prisma location:** `infra/database/prisma/` (event, payment) vs `shared/prisma/` (user).
- **tsconfig path aliases:** `api-gateway` has **none** (all relative); `user-svc` has ~13 hyper-granular (`@filters/`, `@guards/`, `@utils/`, …); event/payment have a middle ~6.
- **Empty scaffolding:** `event-svc/src/modules/events/controllers/http/` and `event-svc/src/infra/database/redis/` are empty dirs.

---

## 2. Goals / non-goals

**Goals**
1. Define **one canonical NestJS layout convention**, archetype-aware, and record it in root `CLAUDE.md`.
2. Converge **event-svc** fully onto it as the **reference implementation** (the worst offender; exercises every feature).
3. Establish a repeatable **verification recipe** (clean install → build → lint → boot) usable by the follow-on services.

**Non-goals (this spec)**
- Converging payment-svc, api-gateway, user-svc — each gets its **own** follow-on plan using event-svc as the template.
- Adding test coverage. Verification is golden-master (build/lint/boot), not red-green.
- Behavioural change, dependency upgrades, proto/buf work (that is P2 Tier 2), or payment lambda dedup (Tier 3).
- Fixing the broken committed `node_modules` permanently (we work around it with clean installs; a real fix is out of scope).

---

## 3. The canonical TS layout convention

The four services are **different archetypes**, so the convention is a **shared skeleton + archetype-specific additions** — not one uniform template (forcing uniform layering is the exact sin REVIEW.md diagnosed).

### 3.1 Shared skeleton (every TS service)

```
src/
  main.ts
  app.module.ts                 # (+ app.controller/service only if a real health endpoint)
  modules/<feature>/            # feature modules ALWAYS under modules/
    <feature>.controller.ts     # flat; see 3.3 for the 2-transport exception
    <feature>.service.ts
    <feature>.module.ts
    dto/                        # TRANSPORT DTOs only (singular folder)
  common/                       # decorators, exceptions, filters, guards, interceptors, middlewares
  shared/                       # constants, interfaces, services, swagger, utils
  protogen/                     # generated proto stubs — never hand-edited
  protos/                       # runtime .proto copy (synced from root proto/) — never hand-edited
```

### 3.2 Archetype-specific additions

- **HTTP edge / proxy (`api-gateway`):** has a single `dto/` (request + response DTOs collapsed together; today's `dtos/req` + `dtos/resp` merge here) and `mappers/` (proto-enum translation). **No** `repository/`, `entities/`, `infra/database`, or domain layer — it is a pure gRPC client. Do not add them.
- **Domain gRPC service (`event-svc`, `payment-svc`):** adds `entities/`, `repository/` (`<feature>.repository.ts` + `<feature>.mapper.ts`), `<feature>.types.ts` (domain shapes — see 3.4), `mappers/`, and an `infra/` layer (3.5).
- **Tiny gRPC service (`user-svc`):** shared skeleton only. Injects `PrismaService` directly (no repository wrapper — the dead `BaseRepository` was already removed). Adopts `infra/database/prisma` (moved from today's `shared/prisma`) and adds nothing else. Do not re-scaffold layers it doesn't need.

### 3.3 Controllers

- **Flat by default:** `<feature>.controller.ts` at the module root.
- **`controllers/<transport>/` only when a module genuinely serves two transports.** Only `payment-svc` qualifies (gRPC core + HTTP webhook), so it keeps `controllers/grpc/` + `controllers/http/`. Every other module is flat. event-svc is gRPC-only → its `controllers/grpc/` collapses up one level and the empty `controllers/http/` is deleted.

### 3.4 DTO model (the reconciliation)

The "one `dto/` per module" rule in CLAUDE.md predates the discovery that the second layer is legitimate. Reconciled rule:

- **`dto/` = the module's public contract (transport).** Validated classes that `implements <Proto>Request`, hold class-validator decorators, and own the over-the-wire→service conversion (`toServiceDto()`). One singular `dto/` folder. Gateway's `dtos/req` + `dtos/resp` collapse here (filenames already disambiguate: `*.dto.ts` vs `*.resp.dto.ts`).
- **`<feature>.types.ts` = internal domain/command shapes.** Plain interfaces (e.g. `{ startDate: Date }`), no decorators, an implementation detail of the service. The previously-separate second `dtos/` folder is **consolidated into a single `<feature>.types.ts`** co-located in the module. Classes that are pure data holders become interfaces unless something constructs them (`new`) or uses `instanceof` — the plan verifies this before converting.

### 3.5 `infra/` layer (adopted officially)

Datastore and messaging adapters live under `src/infra/`:

```
infra/
  database/prisma/   # prisma.module.ts + prisma.service.ts
  messaging/kafka/   # payment-svc only
```

This is now an **official layer** and will be documented in CLAUDE.md alongside `common/shared/modules/protogen`. user-svc's `shared/prisma/` moves to `infra/database/prisma/` in its follow-on. Empty infra subdirs (e.g. event's `infra/database/redis/`) are deleted, not kept as placeholders.

### 3.6 tsconfig path aliases (one scheme)

All four services adopt exactly this set; nothing more granular:

```
@/*         -> src/*
@modules/*  -> src/modules/*
@common/*   -> src/common/*
@shared/*   -> src/shared/*
@infra/*    -> src/infra/*
@protogen/* -> src/protogen/*
```

- Drop user-svc's ~13 hyper-granular aliases.
- Add the scheme to api-gateway (currently none); converting its relative imports is the bulk of the gateway follow-on.

### 3.7 Other standing rules

- **Keep `mappers/`** — proto enums are non-contiguous (`DRAFT=1, PUBLISHED=2, CONFIGURED=4`), so the status/role/currency mappers are necessary translation, not boilerplate (REVIEW.md P2 retraction).
- **Collapse single-file `common/<kind>/` or `shared/<kind>/` folders into flat files** where a folder holds exactly one file and won't plausibly grow — judiciously, low priority within a plan.
- **`dto/` is singular** everywhere (not `dtos/`).

---

## 4. Verification model

The committed `node_modules` for the TS services has **broken `.bin` symlinks** (restored from a copy/cache; `.bin/tsc` is a 45-byte stub, `lib/tsc.js` missing). A plain `npm install` does **not** repair it; a **clean reinstall does** (verified on event-svc: `rm -rf node_modules && npm install && npm run build` → green, `dist/` produced).

**Per-service verification recipe (run before and after the refactor — green→green):**

1. `rm -rf node_modules && npm install` — establish a working toolchain.
2. `npm run build` (nest build / tsc) — **must compile.** Catches import-path breakage from moves.
3. `npm run lint` (eslint) — **must pass.**
4. **Boot gate:** `docker compose -f development/docker-compose.aws.yml up --build <service>` (or `npm run start` against a local DB) — **service bootstraps and serves on its port.** tsc does **not** catch DI-token resolution, decorator-metadata shifts, module wiring/load-order, or `nest-cli.json` asset-glob breakage — only a boot does. This is the real acceptance gate.

**Asset-glob safety:** all four `nest-cli.json` files copy `src/protos/**` → `dist`. This refactor never moves `src/protos/`, so the runtime proto copy is unaffected. (Confirmed.)

**Baseline first:** the plan's first step is to capture baseline-green (steps 1–4) on event-svc *before* any move, so a regression is unambiguous.

---

## 5. Scope & sequencing (reference-first)

- **This spec + first plan:** the canonical convention (§3) + full convergence of **event-svc** + a CLAUDE.md update recording the convention.
- **Follow-on plans (one each, later, in this order):** `payment-svc` (next-most-complex; validates the 2-transport `controllers/` exception and the kafka `infra/`), then `api-gateway` (alias introduction + req/resp collapse), then `user-svc` (smallest; `src/user/`→`modules/user/`, prisma→infra, alias trim).

Each follow-on reuses the §4 recipe and points at event-svc as the worked example.

---

## 6. event-svc migration map (the reference)

Current → target. All moves are within `src/`; `protogen/` and `protos/` are untouched.

| Current | Target | Notes |
|---|---|---|
| `modules/events/controllers/grpc/events.controller.ts` | `modules/events/events.controller.ts` | flatten (gRPC-only) |
| `modules/events/controllers/grpc/dtos/*.ts` (13) | `modules/events/dto/*.ts` | transport DTOs; singular `dto/` |
| `modules/events/controllers/grpc/mappers/*.ts` (3+index) | `modules/events/mappers/*.ts` | keep mappers |
| `modules/events/dtos/*.ts` (8 classes + index) | `modules/events/events.types.ts` | consolidate domain shapes into one file; class→interface where safe |
| `modules/events/controllers/` (wrapper) | *deleted* | incl. empty `controllers/http/` |
| `infra/database/redis/` (empty) | *deleted* | |
| `entities/`, `repository/`, `infra/database/prisma/` | unchanged | already canonical |

**Import rewrites:** every consumer of the moved files (the controller, `events.service.ts`, `events.module.ts`, the transport DTOs' `import … from '../../../dtos'`, mappers' barrels) must be repointed. tsc is the catch-all for misses. Update barrels (`index.ts`) accordingly.

**Domain-shape consolidation:** the 8 domain classes (`CreateCategoryDto`, `CreateConfigDto`, `CreateEventDto`, `CreateEventRoleDto`, `FilterEventDto`, `FindManyEventDto`, `UpdateConfigDto`, `UpdateEventDto`) move into `events.types.ts`. Verify none are constructed with `new` or used with `instanceof` before converting class→interface; if any are, keep them as classes in the same file.

**Optional, flagged (not core):** `app.controller.ts`/`app.service.ts` expose an HTTP `@Get()` "getHello" in a gRPC-only microservice that serves no HTTP — likely dead. Note for deletion but treat as a separate decision, not part of the layout move.

---

## 7. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Broken `node_modules` blocks verification | Clean reinstall recipe (§4), verified working on event-svc. |
| tsc green but service fails at boot (DI/asset/wiring) | Mandatory boot gate (§4 step 4) as the real acceptance check. |
| class→interface conversion changes runtime behaviour | Grep for `new <Dto>` / `instanceof` first; keep as class if found. |
| Moves break the `nest-cli.json` proto asset copy | Refactor never touches `src/protos/`; globs confirmed safe. |
| Scope creep into the other 3 services | Hard scope: event-svc only this plan; follow-ons are separate. |
| Convention drifts from docs again | CLAUDE.md updated in the same plan (the §3 convention is the durable artifact). |

---

## 8. Acceptance criteria

1. event-svc matches the §3 canonical layout: flat controller, singular `dto/`, `events.types.ts`, `mappers/`, `infra/`, the standard alias set; `controllers/` wrapper and empty `redis/` gone.
2. `rm -rf node_modules && npm install && npm run build && npm run lint` on event-svc is **green**.
3. event-svc **boots** via docker compose and serves gRPC on `50053`.
4. No `src/protos/` or `src/protogen/` change; no behavioural/dependency change.
5. Root `CLAUDE.md` records the canonical convention (§3), including the `infra/` layer and the transport-`dto/` vs domain-`types.ts` rule (replacing the now-too-simple "one dto/ per module" note).
