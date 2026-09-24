---
name: add-grpc-endpoint
description: Use when adding a new gRPC RPC end-to-end across TicketBottle — defining it in proto/, implementing the server in the owning service, and (usually) exposing it through the API Gateway as REST. Covers both Go-owned and NestJS-owned services.
---

# Adding a gRPC endpoint end-to-end

## 1. Contract first
Add the RPC + request/response messages to `proto/<owner>.proto`, then regenerate (see the `proto-change` skill: `make proto`). Commit stubs with the contract.

## 2. Implement the server (the service that owns the RPC)

**Go-owned service** (`order`, `inventory`, `waitroom`):
- Add the handler in `internal/delivery/grpc/` — it should be thin: unmarshal → call `internal/service(s)/` → map result to the proto response.
- Put business logic in `internal/service/`, data access in `internal/repository/`. Domain types live in `internal/models/`.
- Logging: zap wrapper, ctx-first `f`-methods (`l.Errorf(ctx, "pkg.type.Method: %v", err)`). In **order-svc only**, return declared error `var`s, never `fmt.Errorf`.

**NestJS-owned service** (`event`, `user`, `payment`):
- Add a `@GrpcMethod` controller method under `src/modules/<feature>/`, delegate to the feature service, data access via the repository.
- Use a `dto/` class with `class-validator` decorators (a global `ValidationPipe` throws `RpcValidationException`). Map domain ↔ proto in the module's mapper if one exists; prefer the generated proto types directly over redefining enums.

## 3. Expose via the API Gateway (if the endpoint is client-facing)
- In `services/api-gateway/src/modules/<service>/`: add a REST controller method, a request/response DTO, and call the downstream service through its **gRPC client** (registered with `ClientsModule` in that feature's `<feature>.module.ts`).
- gRPC client addresses come from config/env (one address per downstream service) — never hardcode ports.
- gRPC codes become HTTP statuses only through `GRPC_TO_HTTP` in `src/shared/metrics/code.ts`, applied by the global filter in `src/common/filters` — never in the controller.

## 4. Verify
- Rebuild the owner (`go build ./...` or `npm run build`) and the gateway (`npm run build`).
- The gateway is the only HTTP entry point (port 3000, Swagger in dev) — smoke-test through it.

> Match the **existing** module's conventions in the service you're editing — the TS services currently differ (see `docs/design/ts-layout.md`, *Where each service stands*). Don't introduce a new layout; a service converges whole.
