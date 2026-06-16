# P2 Tier 1: Safe Cleanups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the dead `BaseRepository` scaffolding from `user-svc` and rewrite `development/README.md` so it matches the DynamoDB-only monorepo reality.

**Architecture:** Two independent, mechanically-verifiable cleanups from the P2 backlog (see `REVIEW.md`). No behavior changes. Task 1 deletes provably-unused code (verified: nothing imports it; `user.service.ts` uses `PrismaService` directly). Task 2 replaces a stale doc that describes a non-existent `legacy/mongodb` branch and `make up-legacy` target.

**Tech Stack:** TypeScript / NestJS (user-svc), Markdown, Docker Compose, GNU Make.

> **Verification approach (read this — it is NOT standard TDD):** The TS services have ~zero test coverage and these are a dead-code deletion and a docs rewrite, so there is no behavior to red-green. Verification here is **golden-master / build-passes / docs-match-reality**: prove no references exist (grep returns empty), prove the build still compiles, and prove the doc contains no stale claims (grep returns empty). Keep the rest of the discipline: exact paths, exact commands with expected output, one action per step, commit after each task.

---

## File Structure

- `services/user-svc/src/shared/repositories/base.repository.ts` — **delete** (dead: an `any`-typed generic Prisma wrapper, never extended).
- `services/user-svc/src/shared/repositories/interfaces/base.interface.ts` — **delete** (dead interface for the above).
- `services/user-svc/src/shared/repositories/` (and `interfaces/`) — **delete** the now-empty directories.
- `development/README.md` — **replace** entirely with content matching current reality.

No other files are touched. `user-svc/src/user/user.service.ts` already injects `PrismaService` directly and does not reference the repositories dir — confirmed by grep.

---

### Task 1: Delete dead `BaseRepository` scaffolding from user-svc

**Files:**
- Delete: `services/user-svc/src/shared/repositories/base.repository.ts`
- Delete: `services/user-svc/src/shared/repositories/interfaces/base.interface.ts`
- Delete (empty after): `services/user-svc/src/shared/repositories/`

- [ ] **Step 1: Prove nothing references the code to be deleted**

Run:
```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
grep -rn "repositories\|BaseRepository\|base.repository\|base.interface\|BaseRepositoryInterface\|PaginationQuery" \
  services/user-svc/src --include='*.ts' | grep -v 'src/shared/repositories/'
```
Expected: **no output** (empty). This proves the only references to these symbols live inside the directory being deleted. If any line prints, STOP — there is a live consumer; do not delete.

- [ ] **Step 2: Delete the dead files (git rm)**

Run:
```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git rm services/user-svc/src/shared/repositories/base.repository.ts \
       services/user-svc/src/shared/repositories/interfaces/base.interface.ts
```
Expected: `rm 'services/user-svc/src/shared/repositories/base.repository.ts'` and the interface line. The now-empty `repositories/` and `interfaces/` dirs are removed automatically by `git rm`.

- [ ] **Step 3: Verify the directory is gone**

Run:
```bash
ls services/user-svc/src/shared/repositories 2>&1 || echo "GONE"
```
Expected: `GONE` (or "No such file or directory").

- [ ] **Step 4: Verify the service still compiles (build-passes verification)**

Run:
```bash
cd /Users/vogiaan/coding/projects/TicketEventPF/services/user-svc
npm install >/dev/null 2>&1; npm run build
```
Expected: build completes with no TypeScript error referencing `base.repository`, `base.interface`, or `repositories`. (If `npm run build` fails for a pre-existing/unrelated reason in this incomplete local env, the Step 1 grep is the authoritative proof that the deletion introduced no dangling reference — note the build output and proceed.)

- [ ] **Step 5: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add -A services/user-svc
git commit -m "refactor(user-svc): remove dead BaseRepository scaffolding

Unused: nothing extended BaseRepository and user.service.ts injects
PrismaService directly. Drops an any-typed generic wrapper that added
indirection while discarding type safety.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Rewrite `development/README.md` to match reality

**Files:**
- Modify (full replace): `development/README.md`

Context for the worker: the current README documents a `legacy/mongodb` git branch, "legacy MongoDB images", and a `make up-legacy` command — **none of which exist**. The real Makefile targets are `up`, `down`, `up-aws`, `down-aws`, `logs`, `logs-aws`, `status`, `clean` (run `grep -nE '^[a-z-]+:' development/Makefile` to confirm). `order-svc` is DynamoDB-only; `make up-aws` is the supported path; `make up` (MongoDB) is legacy and non-functional. Compose build contexts point at `../services/<name>-svc`.

- [ ] **Step 1: Replace the file with the content below**

Write the following as the complete new `development/README.md`:

````markdown
# TicketBottle Development Environment

Local orchestration for all TicketBottle V2 services. Compose files and the
Makefile live here; service code lives under `../services/<name>-svc`.

## Supported mode: AWS / DynamoDB (LocalStack)

`order-svc` is **DynamoDB-only**, so the supported local stack is the AWS mode,
which runs DynamoDB (and Lambda, etc.) inside LocalStack.

```bash
make up-aws      # build + start everything (DynamoDB via LocalStack)
make down-aws    # stop it
make status      # container status
make logs-aws    # tail all logs   (make logs-order / logs-consumer / logs-localstack for one)
make clean       # remove containers + volumes for BOTH modes
```

> **Legacy MongoDB mode (`make up` / `make down`) is non-functional.** It predates
> the DynamoDB migration; `order-svc` no longer ships a MongoDB driver, so the
> `mongo-order` container has nothing to talk to. It is kept only for history and
> should be removed or deliberately revived. Do not use it.

## Build

`make up-aws` builds each service image from its source directory — the compose
build contexts are `../services/<name>-svc` (e.g. `../services/order-svc`,
`../services/api-gateway`). There is **no** manual branch checkout step: this is a
single monorepo on `main`, not separate sibling repos. (`docker-compose.aws.yml`
expects the prebuilt `ticketbottle-order-*:aws` / `ticketbottle-payment-service:aws`
images for the Order and Payment services — build those from their service dirs if
they are not already present.)

## Service ports

| Service       | Port            | Protocol            |
|---------------|-----------------|---------------------|
| API Gateway   | 3000            | HTTP/REST + Swagger |
| User          | 50052           | gRPC                |
| Event         | 50053           | gRPC                |
| Order         | 50054           | gRPC                |
| Payment       | 50055 (+ 8085)  | gRPC                |
| Waitroom      | 50056           | gRPC                |
| Inventory     | 50057           | gRPC                |

Infrastructure: Kafka 9092 (UI 8090), Temporal 7233 (UI 8080), Redis 6379
(waitroom) / 6380 (auth), LocalStack 4566. Postgres: Payment 5433, Event 5434,
Inventory 5435, User 5436.

Per-service env files live in `envs/.env.*`.

## How it works

The API Gateway (HTTP) is the only entry point; everything behind it is gRPC. The
Order service drives a Temporal saga (Event → Inventory → Payment) and reacts to
Kafka events. See the root `README.md` and `CLAUDE.md` for the full architecture
and the end-to-end purchase flow.

## Troubleshooting

- **A service image is missing / out of date:** rebuild from its dir, e.g.
  `docker compose -f docker-compose.aws.yml build order-service`.
- **Order can't reach DynamoDB:** confirm LocalStack is healthy
  (`make logs-localstack`) and that `envs/.env.order.aws` points
  `DYNAMODB_ENDPOINT` at LocalStack.
- **Check DynamoDB table:** `make test-dynamodb`.
- **Shell into LocalStack:** `make shell-localstack`.
````

- [ ] **Step 2: Verify no stale claims remain (docs-match-reality verification)**

Run:
```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
grep -nE 'ticketbottle-order/|ticketbottle-payment/|\.\./ticketbottle-|up-legacy|legacy/mongodb|Legacy Mode|Legacy Images|Checkout legacy' development/README.md
```
Expected: **no output** (empty). Every stale path, the non-existent `make up-legacy`, the `legacy/mongodb` branch, and the legacy-image sections are gone.

- [ ] **Step 3: Verify every Makefile target the README mentions actually exists**

Run:
```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
for t in up up-aws down-aws status logs-aws logs-order logs-consumer logs-localstack clean test-dynamodb shell-localstack; do
  grep -qE "^$t:" development/Makefile && echo "OK   $t" || echo "MISSING $t"
done
```
Expected: every line prints `OK` (no `MISSING`).

- [ ] **Step 4: Commit**

```bash
cd /Users/vogiaan/coding/projects/TicketEventPF
git add development/README.md
git commit -m "docs(development): rewrite README for DynamoDB-only monorepo reality

Removes the defunct legacy/mongodb two-branch model, the non-existent
make up-legacy target, and the ../ticketbottle-* paths. Documents the
supported make up-aws flow and the services/*-svc build contexts.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** Tier 1 of the refined P2 (REVIEW.md) = (a) delete dead `BaseRepository` → Task 1; (b) rewrite stale `development/README.md` → Task 2. Both covered. (Mappers are intentionally NOT touched — confirmed legitimate due to non-contiguous proto enums. Layout convergence is intentionally deferred. buf migration and payment dedup are Tier 2/3, separate plans.)
- **Placeholder scan:** No TBDs. Task 1 shows exact files + exact grep/commands + expected output. Task 2 embeds the complete replacement README, not a description of it.
- **Consistency:** Symbol names (`BaseRepository`, `BaseRepositoryInterface`, `PaginationQuery`) match the grep in Step 1 of Task 1. Makefile targets named in the README (Task 2) are verified to exist by Task 2 Step 3.
