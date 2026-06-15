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
- In `services/api-gateway/src/modules/<service>/`: add a REST controller method, a request/response DTO, and call the downstream service through its **gRPC client** (registered in `src/shared/microservices`).
- gRPC client addresses come from config/env (one address per downstream service) — never hardcode ports.
- Map gRPC errors to HTTP in `src/common/filters` (add a mapping there, not in the controller).

## 4. Verify
- Rebuild the owner (`go build ./...` or `npm run build`) and the gateway (`npm run build`).
- The gateway is the only HTTP entry point (port 3000, Swagger in dev) — smoke-test through it.

> Match the **existing** module's conventions in the service you're editing — the three TS services currently differ (see root `CLAUDE.md` "Canonical TS layout"). Don't introduce a new layout.
