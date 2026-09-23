# User — reading guide

*Trust labels checked against the code on 2026-09-23.*

For accounts, profiles, stored credentials and email verification. Authentication
itself — hashing, verifying, issuing tokens — is the gateway's, not this service's.

## Read in this order

1. `services/user-svc/CLAUDE.md` — **current**: role, commands, layout (no
   repository layer), and where passwords are hashed.
2. [api-gateway.md](api-gateway.md), *Sign-up, sign-in, tokens* — the other half of
   every credential question.
3. `docs/plans/2026-09-21-user-svc-test-coverage.md` — what the tests pin, and the
   defects they found. A record of that day.

## By question

| Question | Owner | Verify in |
|---|---|---|
| Where a password is hashed and checked | service `CLAUDE.md`, *Notes* | `services/api-gateway/src/modules/auth/auth.service.ts` |
| What a user row holds | `services/user-svc/prisma/schema.prisma` | same |
| How a validation or business error leaves the service | root `CLAUDE.md`, *Error taxonomy* | `services/user-svc/src/common/filters/global-grpc-exception.filter.ts`, `services/user-svc/src/shared/constants/error-code.constant.ts` |

## Code entry points

| To see | Open |
|---|---|
| The gRPC surface | `services/user-svc/src/user/user.controller.ts` |
| Logic and data access, together | `services/user-svc/src/user/user.service.ts` |
| Bootstrap and the global validation pipe | `services/user-svc/src/main.ts` |

## Edges

- **Serves** `proto/user.proto`, to the gateway only.
- **Calls** nothing; no Kafka.
- **Store** — Postgres via Prisma; migrations in `services/user-svc/prisma/`.
- **Deployed by** `deploy/helm/ticketbottle/templates/apps/user.yaml`.
