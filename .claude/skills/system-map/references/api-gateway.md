# API Gateway — reading guide

*Trust labels checked against the code on 2026-09-23.*

For the HTTP edge: REST routes, auth, validation, the gRPC-to-HTTP error mapping,
the gateway's gRPC clients, and its request metrics.

## Read in this order

1. `services/api-gateway/CLAUDE.md` — **current**: role, commands, layout, where
   error mappings and client addresses belong.
2. Root `CLAUDE.md`, *Error taxonomy* and *Canonical TS layout* — binding: the only
   place a gRPC code becomes an HTTP status, and the target shape for any new module.
3. `services/api-gateway/src/CLAUDE.md` — the style guide for the module shape the
   gateway has today (`dtos/req` + `dtos/resp`, mappers, enums). It loads by itself
   whenever you open gateway source. It predates the *Canonical TS layout*, which
   forbids that split in new structure; where they conflict, the root file is
   binding. Whether to keep it is open — see the root register.
4. `docs/plans/2026-09-20-api-gateway-test-coverage.md` — what the tests pin, and the
   defects they found. A record of that day.

## By question

| Question | Owner | Verify in |
|---|---|---|
| Which HTTP status a failure becomes | root `CLAUDE.md`, *Error taxonomy* | `services/api-gateway/src/shared/metrics/code.ts`, applied by `services/api-gateway/src/common/filters/global-exception.filter.ts` |
| Sign-up, sign-in, tokens, password hashing | service `CLAUDE.md` | `services/api-gateway/src/modules/auth/auth.service.ts`, `services/api-gateway/src/modules/auth/strategies/jwt.strategy.ts` |
| Which routes need which role | service `CLAUDE.md` | `services/api-gateway/src/common/guards/access.guard.ts` |
| Rate limiting, CORS, security headers | service `CLAUDE.md`, *Role* | `services/api-gateway/src/main.ts` |
| Why the gateway sends order fields the contract lacks | root `CLAUDE.md`, register — **open** | `services/api-gateway/src/protogen/order.pb.ts` against `services/api-gateway/src/protos/order.proto` |
| Whose waitroom session a request may touch | `docs/decisions/0008-the-waitroom-enforces-session-ownership.md` | `services/api-gateway/src/modules/waitroom/waitroom.service.ts` |
| What it records per request | `docs/METRICS.md` | `services/api-gateway/src/common/middlewares/metrics.middleware.ts` |

## Code entry points

| To see | Open |
|---|---|
| Bootstrap: prefix, validation, CORS, Swagger | `services/api-gateway/src/main.ts` |
| Every feature module | `services/api-gateway/src/app.module.ts` |
| A downstream client: address and proto | that feature's module, e.g. `services/api-gateway/src/modules/orders/orders.module.ts` |
| Response shaping | `services/api-gateway/src/common/interceptors/response.interceptor.ts` |

## Edges

- **Calls** user, event, order, waitroom and inventory over gRPC, one feature module
  each under `services/api-gateway/src/modules/`. Inventory's is a stub.
- **Loads protos at runtime** from `services/api-gateway/src/protos/`, synced from
  `proto/` — `.claude/skills/proto-change/SKILL.md`.
- **Store** — none of its own; Redis for auth state (`deploy/README.md`).
- **Deployed by** `deploy/helm/ticketbottle/templates/apps/gateway.yaml`; NodePort
  30000 on k3s, ALB on EKS.

## Don't trust for current behaviour

- `services/api-gateway/src/protogen/order.pb.ts` — stale against the runtime proto.
- `services/api-gateway/src/CLAUDE.md` as the layout for a **new** module.
