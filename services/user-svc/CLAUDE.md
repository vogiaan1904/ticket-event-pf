# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is the **User** service for TicketBottle V2. For the system-wide picture, ports, and dev workflow, see the umbrella `../CLAUDE.md`.

## Role

gRPC microservice (port **50052**) owning user registration, authentication, profile management, and email verification. Data lives in PostgreSQL via Prisma. Called by the API Gateway; has no HTTP server of its own.

## Commands

```bash
npm install
npm run start:dev          # watch mode
npm run start:prod         # node dist/main
npm run build
npm run lint               # eslint --fix
npm run test               # jest  (npm run test -- <path> for one file)
npm run prisma-migrate:dev # apply a Prisma migration in dev
npm run proto:all          # regenerate gRPC stubs into src/protogen
```

## Layout

- `src/user/` — the gRPC controller, service, and `dto/` for user operations.
- `src/shared/{prisma,repositories,services}` — Prisma client, repository layer (data access is abstracted behind repositories), config/logger.
- `src/common/{filters,exceptions,decorators}` — `GlobalGrpcExceptionFilter` and `RpcValidationException` turn validation/business errors into gRPC statuses.
- `prisma/` — schema and migrations. `src/protogen/` is generated from `protos/user.proto` — don't hand-edit.

## Notes

- Passwords are hashed with bcrypt; this service is the source of truth for credentials and JWT subject claims consumed elsewhere.
- Bootstrap registers a global `ValidationPipe` (`whitelist: true, transform: true`) that throws `RpcValidationException`; keep DTO validation decorators authoritative.
