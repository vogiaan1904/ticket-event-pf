# TS service layout — design

**Status:** specified 2026-06-16 and re-adopted 2026-09-24
([0014](../decisions/0014-ts-structure-follows-the-june-layout-rule.md)). **No service is
converged yet**; none is the reference until one is. The event-svc convergence plan
of 2026-06-17 was written and never executed.
**Applies to:** `api-gateway`, `event-svc`, `payment-svc`, `user-svc`.

## The problem

The four NestJS services grew four ways of arranging a feature: `modules/<feature>/`
beside a bare `src/user/`, flat controllers beside `controllers/grpc/`, and five DTO
shapes (`dtos/req` + `dtos/resp`, flat `dtos/`, `dto/`, and a double layer of
`controllers/grpc/dtos` plus a second `dtos/`). A reader cannot predict where anything
is, and each new module copies whichever service its author last opened.

The double DTO layer looked redundant and is not. `controllers/grpc/dtos/*.dto.ts` are
**transport** DTOs: they implement the proto request, carry class-validator decorators,
hold wire types (`startDate: string`) and convert with `toServiceDto()`. The second
`dtos/` holds **domain** shapes (`startDate: Date`, no decorators). Merging them naively
loses the validation and the string→`Date` boundary.

## The rule

A shared skeleton plus additions by archetype, not one template for all four: forcing
uniform depth on a small service is the over-layering this replaces.

```
src/
  main.ts
  app.module.ts
  modules/<feature>/
    <feature>.controller.ts     flat, unless the module serves two transports
    <feature>.service.ts
    <feature>.module.ts
    dto/                        transport DTOs only, singular folder
  common/                       decorators, exceptions, filters, guards, interceptors, middlewares
  shared/                       constants, interfaces, services, utils
  protogen/                     generated, never hand-edited
  protos/                       runtime .proto copy synced from root proto/, never hand-edited
```

| Archetype | Adds | Never adds |
|---|---|---|
| HTTP edge (`api-gateway`) | `mappers/` for proto enums; request and response DTOs together in one `dto/` (`*.dto.ts`, `*.resp.dto.ts`) | `repository/`, `entities/`, `infra/`, a domain layer — it is a gRPC client |
| Domain service (`event-svc`, `payment-svc`) | `entities/`, `repository/`, `<feature>.types.ts`, `mappers/`, `infra/` | — |
| Small service (`user-svc`) | `infra/database/prisma/` only; `PrismaService` injected directly | a repository wrapper or any empty layer |

- **Controllers** are flat. `controllers/<transport>/` only where one module serves two
  transports — today only `payment-svc` (gRPC plus the HTTP webhook shell).
- **DTOs.** `dto/` is the module's public, validated contract. Domain shapes are plain
  interfaces in one `<feature>.types.ts`; a class stays a class only if something
  constructs it with `new` or tests it with `instanceof`.
- **`infra/`** holds datastore and messaging adapters: `infra/database/prisma/`, and
  `infra/messaging/kafka/` where a service has one. No empty placeholders.
- **Path aliases**, the same six everywhere: `@/*`, `@modules/*`, `@common/*`,
  `@shared/*`, `@infra/*`, `@protogen/*`.
- **Keep `mappers/`.** The proto enums are non-contiguous (`DRAFT=1, PUBLISHED=2,
  CONFIGURED=4`), so the status, role and currency mappers are translation, not
  boilerplate.
- A `common/` or `shared/` folder holding one file that will not grow becomes a flat file.

## Where each service stands

| Service | Off the rule |
|---|---|
| `event-svc` | `controllers/grpc/` wrapper around a gRPC-only controller; transport DTOs in `controllers/grpc/dtos/`, domain shapes in a second `dtos/` |
| `payment-svc` | transport DTOs under `controllers/grpc/dto/`; domain shapes in `dto/` |
| `api-gateway` | `dtos/req` + `dtos/resp`; 15 granular aliases |
| `user-svc` | `src/user/` outside `modules/`; Prisma under `shared/prisma/`; 13 aliases |

## Converging a service

One service per change, and only when that service is being worked on anyway: the
payoff is predictability, not capability, so it does not justify a change of its own.

1. `rm -rf node_modules && npm ci` — the committed toolchain links can be stale.
2. `npm run build` and `npm test` green **before** the first move.
3. Move files, rewrite imports, keep every symbol name. No behaviour change.
4. `npm run build` and `npm test` green again, then **boot** the built service (against
   its `docker-compose.dev.yml` datastore where it has one): `tsc` does not catch DI
   tokens, decorator metadata, module wiring or the `nest-cli.json` copy of
   `src/protos/**`.
5. Update this table and the service's `CLAUDE.md` in the same change. The first
   service converged becomes the worked example the others follow.
