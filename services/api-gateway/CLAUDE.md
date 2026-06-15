# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **API Gateway** for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

The only HTTP entry point to the platform. It exposes a REST API (global prefix `api`, Swagger docs in `development`/`staging`), handles JWT auth, validation, CORS, and rate limiting, then fans out to the backend services as a **gRPC client**. It owns no database.

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
- `src/shared/{microservices,services,constants,swagger,interfaces,types,utils}` — gRPC client registration, config/logger services, Swagger setup.
- `src/protogen/` — generated gRPC stubs (do not hand-edit); `protos/` / `src/protos` — the `.proto` copies they are generated from.

## Notes

- gRPC client targets come from config/env (one address per downstream service) — update those, not hardcoded ports, when wiring a new service.
- This service translates gRPC errors into HTTP responses in `common/filters`; add new error mappings there rather than in controllers.
