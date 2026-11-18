# event-svc converges to the TS layout

**Status: COMPLETE 2026-11-18.** `93d4ffc`, `a6aa617`, `9f119dd`, docs in `c440c7d`; CI green; `k3s-gate2` passed at `sha-6ff19a0` → [0034](../decisions/0034-event-svc-converges-first-on-its-own.md#outcome). Item 3 of
[0031](../decisions/0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md); the
service and the standalone change are
[0034](../decisions/0034-event-svc-converges-first-on-its-own.md).

**Design:** [`docs/design/ts-layout.md`](../design/ts-layout.md). This plan follows its
*Converging a service* steps; it adds nothing to the rule.

**Goal:** event-svc matches the domain-service archetype, with no behaviour change, and
becomes the worked example payment-svc follows.

## Where event-svc is off the rule (checked 2026-11-14)

| Today | Target |
|---|---|
| `modules/events/controllers/grpc/events.controller.ts` (+ spec) | `modules/events/events.controller.ts` |
| `controllers/grpc/dtos/*` — transport, validated, `implements <Proto>Request` | `modules/events/dto/` |
| `controllers/grpc/mappers/*` | `modules/events/mappers/` |
| `modules/events/dtos/*` — 8 domain classes, no decorators, none built with `new` or tested with `instanceof` | interfaces in `modules/events/events.types.ts` |
| aliases `@interfaces/*` (0 uses), no `@protogen/*` | the design's six |

Already on the rule: `entities/`, `repository/` (with its `events.mapper.ts`),
`infra/database/prisma/`.

**Names stay.** Six domain shapes share a name with a transport DTO (`CreateEventDto`,
`CreateConfigDto`, `FilterEventDto`, `FindManyEventDto`, `UpdateEventDto`,
`UpdateConfigDto`). The transport files already import the domain one as
`Service<Name>`; that alias carries over. Renaming either side is a separate choice,
not this change.

## Global constraints

- No behaviour change; every symbol name kept.
- One commit per ticket, each green on `npm run build` and `npm test`.
- The repository suite needs Postgres: `docker compose -f docker-compose.dev.yml up -d`
  in `services/event-svc`, `down -v` when done.
- The `system-map` check fails CI on a gone path, so Ticket 5 lands in the same push
  as Tickets 1–4, not after.

## Ticket 0: green before the first move

- [x] `rm -rf node_modules && npm ci` (the committed `.bin` links can be stale).
- [x] `npm run build && npm test` green; note the suite count.

## Ticket 1: flat controller, `dto/`, `mappers/`

- [x] `git mv` the controller and its spec to the module root, the transport DTOs to
  `dto/`, the mappers to `mappers/`; delete `controllers/`.
- [x] Rewrite imports. Build and test green.

## Ticket 2: domain shapes to `events.types.ts`

- [x] The 8 classes in `dtos/` become exported interfaces in `events.types.ts`, same
  names and fields; delete `dtos/`.
- [x] Imports in `events.service.ts`, `repository/events.repository.ts` and `dto/*`
  point at `../events.types` (or `@modules/events/events.types`).
- [x] Build and test green. A class that `tsc` now refuses as an interface is one that
  was constructed after all: stop and report it rather than work around it.

## Ticket 3: aliases

- [x] `tsconfig.json` `paths` and jest `moduleNameMapper`: drop `@interfaces/*`, add
  `@protogen/*`. Build and test green.

## Ticket 4: boot the built service

`tsc` does not catch DI tokens, decorator metadata, module wiring or the
`nest-cli.json` copy of `src/protos/**`.

- [x] `npm run build`; `ls dist/protos/event.proto`.
- [x] Start it against the compose database; `grpcurl -plaintext -import-path
  ../../proto -proto event.proto localhost:50053 list` names the service, and a
  `FindManyEvents` and a `GetEventConfig` with a bad id answer (`NOT_FOUND`), not
  `UNIMPLEMENTED`.
- [x] After the push, CI's `ts-tests.yml` and `proto-copies.yml` green, and one
  `make -C deploy k3s-gate2` at the new `sha-` tag the next time the box is up.

## Ticket 5: docs in the same change

- [x] `docs/design/ts-layout.md`: event-svc leaves *Where each service stands*; the
  status line names it the worked example.
- [x] `services/event-svc/CLAUDE.md` *Layout* and its mapper path.
- [x] `.claude/skills/system-map/references/event.md`: the moved paths.
- [x] 0034 and 0014 gain their Outcome; the register row for the TS layout says one
  service converged.

## Out of scope

Renaming the shared DTO names; `common/interceptors/transfrom.interceptor.ts`'s
misspelt file name; whether `shared/swagger/` belongs in a gRPC-only service.

## Results

**2026-11-18.** Baseline after a clean `npm ci`: 6 suites, 37 tests, with the repository
suite against the compose Postgres. Each ticket ended at the same 6 and 37.

- Ticket 2: no domain class was constructed or tested with `instanceof`, so all 8 became
  interfaces. `FilterEventDto`'s `categoryIds = []` initializer never ran, since nothing
  called `new`; the interface keeps the field and drops the dead default.
- Ticket 4: the built service (`dist/src/main`, as the Dockerfile runs it) started on
  :50053 with `dist/src/protos/event.proto` in place. `FindMany` with a filter answered
  `OK`, and `FindOne` and `GetConfig` on an unknown id answered `NOT_FOUND`.
- Found, out of scope: `FindMany` with no `filter` answers `INTERNAL`, because the
  controller calls `dto.filter.toServiceDto()` on `undefined`. It predates this change
  (same line at `3eaa1aa`); the gateway always sends a filter. A business-free input that
  yields `INTERNAL` breaks the error taxonomy, so it needs a ticket of its own. Fixed the same day,
  after the convergence: the controller passes no filter through, and the repository
  reads a missing filter as an empty one. Each half has a test that failed first; the
  built service then answered a filterless `FindMany` `OK`.
