# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **API Gateway** for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

The only HTTP entry point to the platform. It exposes a REST API (global prefix `api`, Swagger docs in `development`/`staging`), handles JWT auth, validation and CORS, then fans out to the backend services as a **gRPC client**. It owns no database.

`helmet` sets security headers on every response. Sign-in and sign-up are throttled per client IP, and nothing else is; the proxy hop count that makes the client IP trustworthy is set per target (`APP_TRUST_PROXY_HOPS`). Why, and what it costs: `docs/decisions/0013-the-gateway-throttles-sign-in-not-purchase.md`.

## Commands

```bash
npm install
npm run start:dev      # watch mode (NODE_ENV=development)
npm run start:prod     # node dist/main
npm run build          # nest build
npm run lint           # eslint --fix
npm run test           # jest
npm run test -- <path> # single test file
npm run proto:all      # regenerate gRPC client stubs into src/protogen (see proto:<svc> scripts)
```

Runs on port **3000**. Swagger is served at `/<globalPrefix>/<swaggerPath>` only in dev/staging.

## Layout

- `src/modules/{auth,users,events,inventory,orders,waitroom}` — one feature module per downstream service; each holds REST controllers + DTOs and a gRPC client that calls the matching service.
- `src/common/{guards,filters,interceptors,middlewares,decorators,exceptions}` — auth guards, exception mapping (gRPC status → HTTP), request plumbing.
- `src/shared/{microservices,services,constants,swagger,interfaces,types,utils}` — config/logger services, Swagger setup, downstream error-code constants. Each feature module registers its own gRPC client (`ClientsModule` in `<feature>.module.ts`).
- `src/protogen/` — generated gRPC stubs (do not hand-edit). The single source of truth is the root `../../proto/` directory; `npm run update:proto` syncs it into `src/protos/` and regenerates `src/protogen/`. `src/protos/` is a generated, synced-from-root copy that the NestJS gRPC transport loads at runtime (`protoPath`) and `nest-cli.json` ships into `dist` — don't hand-edit it either.

## Notes

- gRPC client targets come from config/env (one address per downstream service) — update those, not hardcoded ports, when wiring a new service.
- gRPC errors become HTTP responses only in `common/filters/global-exception.filter.ts`, from the `GRPC_TO_HTTP` table in `src/shared/metrics/code.ts`. A new code gets a row in that table, never a mapping in a controller.
