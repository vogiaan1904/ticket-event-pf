# TicketBottle V2 — Architecture & Repo Review

*SA-level review, 2026-06-15. Scope: overall architecture, per-service structure, proto handling, and the Claude Code setup. Findings are grounded in the actual tree, not the docs (the docs are part of the problem — see §A).*

---

## Verdict up front

Your instinct ("over-engineered") is **half right, and the half it's right about is fixable cheaply.**

- **The macro-architecture is justified.** High-traffic ticketing genuinely needs the virtual queue → atomic inventory → saga-orchestrated order → multi-provider payment shape. Temporal for the saga, the outbox pattern, and Kafka for eventual consistency are the *correct* tools for this domain, and as a portfolio piece they demonstrate real distributed-systems competence. Do **not** collapse this to a monolith.
- **The over-engineering is almost entirely *accidental complexity*** — cruft left behind by half-finished migrations (multi-repo→monorepo, submodules→copies, mongo→dynamo, nest→lambda), uniform premature layering applied to tiny services, and documentation/tooling that no longer matches the code. The fix is **deletion and consolidation, not redesign.**

The single worst problem isn't any one service — it's that **the repo's own docs and tooling actively lie about the repo**, which wastes your time and every AI session's time.

---

## A. Repo-reality drift — *fix this first, highest impact*

The project was consolidated from sibling `ticketbottle-*` checkouts into one monorepo under `services/*-svc`, but the supporting machinery was never updated.

| # | Evidence | Impact |
|---|----------|--------|
| A1 | Root `CLAUDE.md` describes top-level `ticketbottle-*` dirs, git submodules, and per-service histories. Reality: one repo, one history, dirs are `services/*-svc`. | Misleads every agent session (incl. Claude) on the most-read file. |
| A2 | **All** `docker-compose.dev.yml` / `.aws.yml` build contexts are `../ticketbottle-*` → these paths **do not exist**. | `make up` / `make up-aws` **cannot build as committed.** The documented local-dev story is broken. |
| A3 | `.gitmodules` is gone, but every service still ships a `protos-submodule/` dir (now just plain file copies) and `make update-proto` still runs `git submodule update --remote`. | Dead/broken command; confusing vestige. |
| A4 | `order-svc/CLAUDE.md` describes a `legacy/mongodb` vs `main` branch split; repo is single-branch. `internal/infra/mongo/` contains **0 `.go` files** (empty leftover); DynamoDB is the only live driver. | Phantom complexity; the "two branches, build the matching one" gotcha no longer applies. |

**Action:** correct root `CLAUDE.md`; repoint compose contexts to `services/*-svc`; delete the dead submodule machinery and the empty mongo dir. Cheap, and it makes everything below trustworthy.

---

## B. Proto handling — *you're right, this is over-engineered* (user complaint #1)

- 6 source contracts in `proto/`. **72 `.proto` files are tracked** = 6 sources + **11 scattered copies**. Each service carries 1–2 copies (`protos/` + `src/protos/`, or `protos-submodule/`); **waitroom carries three** (`proto/`, `protogen/`, `protos-submodule/`).
- Codegen is **N hand-rolled per-service pipelines**: TS services run `proto:user … proto:all`; Go services run `make protoc-all`. There is no single command that regenerates everything, and the one that tried (`update-proto`) is broken (A3).
- The "single source of truth" exists but is fanned out by copy-paste — so it isn't a source of truth in practice.

**Recommendation (target state):** adopt **[buf](https://buf.build)**. One root `proto/` module (`buf.yaml`) + a `buf.gen.yaml` per language target → `buf generate` produces Go and TS stubs into each service's `*/protogen` dir. Delete all `protos/`, `src/protos/`, and `protos-submodule/` copies. One `make proto` regenerates all 7 consumers, and `buf lint` / `buf breaking` give you contract CI for free.

**Minimal interim step (if buf is too big a jump):** keep `protoc`, but point every script at the **root** `proto/` (`-I ../../proto`), delete the 11 copies and the dead `update-proto`, and add one root `Makefile` target that fans out generation. This kills the duplication without a new tool.

> ⚠️ Don't just delete the copies — repoint the existing `proto:*` npm scripts and the Go `make protoc` `-I` paths at root `proto/` in the same change, or you break every build.

---

## C. Layering — *you're right for the TS services* (user complaint #2)

The Go services (`order`, `inventory`, `waitroom`) are **proportionate**: `cmd/ → internal/{delivery,service,repository,models} → pkg/`. Keep that. The over-layering is a **TypeScript-services phenomenon**, and it's inconsistent, which is worse than uniform.

Concrete evidence (not just folder counts):

- **`user-svc`** — 715 LOC spread across ~16 directories. Its `BaseRepository<T>` (`src/shared/repositories/base.repository.ts`) is a generic Prisma wrapper typed with `any` everywhere (`data: any`, `options?: any`, `this.prisma[this.model]`). It adds an indirection layer **and throws away type safety** — the opposite of what a repository abstraction is supposed to buy you.
- **`api-gateway`** — per-module `dtos/req`, `dtos/resp`, `enums`, `mappers`. `StatusMapper` (`modules/events/mappers/status.mapper.ts`) hand-maintains a bidirectional enum↔proto-enum `Map`. That boilerplate exists **only because** domain enums were redefined separately from the generated proto enums. Use the proto enums directly and the mapper class evaporates.
- **`event-svc`** — *two* parallel DTO layers for a single module: `modules/events/controllers/grpc/dtos` (13 files) **and** `modules/events/dtos` (9 files), plus `shared`. Empty `controllers/http/` (it's a gRPC-only service) and empty `infra/database/redis/`.
- **Inconsistency is the real cost:** three TS services, three different module/DTO conventions (`user/dto` vs `modules/events/controllers/grpc/dtos` vs `modules/x/dtos/{req,resp}`). Nothing is shared or predictable.

**Recommendation:** pick **one** NestJS layout convention and apply it across all four TS services. Collapse single-file `common/*` and `shared/*` folders into flat files. Drop the `any`-typed `BaseRepository` (use Prisma's generated types directly, or a typed repo only where there's real shared logic). Eliminate the domain-enum/proto-enum redefinition so the mappers disappear. Right-size to the service: `user-svc` does not need the same scaffolding as `api-gateway`.

*(Minor, low priority: each Go service re-vendors its own `pkg/{logger,errors,grpc,response,util}`. Fine when they were separate repos; in a monorepo this is duplication that could become a shared Go module — but it's harmless, so leave it for later.)*

---

## D. payment-svc dual surface — *defensible, but stop duplicating*

Not a duplicate implementation (I checked). `AI/LAMBDA_MIGRATION_PLAN.md` describes an intentional **hybrid**: the gRPC core lives in `src/` (runs in compose as `payment-service`, ~2.3k LOC) and the event-driven work lives in `lambdas/` (`payment-webhook-handler`, `outbox-processor`, `outbox-cleanup`, ~3.5k LOC, deployed to LocalStack). That split is architecturally reasonable.

The real costs:
- `lambdas/common/{config,constants,database,kafka,logger,types,utils}` **duplicates** the concerns in `src/infra` + `src/shared`. You now maintain two Prisma clients, two Kafka configs, two loggers in sync.
- Heavy build machinery (`common-layer`, `dependencies-layer`) for **three** functions.

**Recommendation:** extract the shared payment domain + data-access code into **one** internal package imported by both `src/` and `lambdas/`. Keep the hybrid split; stop duplicating infra. And make an explicit decision on the gRPC core's end state (stay NestJS-on-EKS vs. also-Lambda) rather than leaving it implied in a plan doc.

---

## E. Cruft & placeholders — *safe deletes*

- `k8s/` — empty.
- `aws/DIAGRAMS.md` — 0 bytes.
- `services/order-svc/internal/infra/mongo/` — no code (A4).
- `protos-submodule/` in every service (B).
- `services/waitroom-svc/.claude/settings.local.json` — orphan per-service permission file.
- `.cursorignore` / `.cursorrules` — Cursor leftovers sitting next to Claude config (pick one assistant's config or `.gitignore` the other).
- ~3.0 GB on disk vs 686 tracked files → ~3 GB of local build artifacts / `vendor/` / `node_modules` / `mongo-data` (correctly gitignored — *good hygiene there* — but worth a `make clean` and a note).

---

## F. Documentation sprawl (user complaint #3)

33 tracked `.md` files (excl. vendor): **9× `CLAUDE.md`**, 7× `README.md`, 2× `SYSTEM.md`, waitroom's five extra guides (`QUEUE_PROCESSOR_GUIDE`, `REAL_TIME_POSITION`, `STREAMING_IMPLEMENTATION_GUIDE`, `SYSTEM_FLOW_README`), and payment's five `AI/` scratch docs — including `lucky-imagining-island.md` (an AI-generated throwaway). Much of it is stale or one-shot.

**Recommendation — one rule per file type:**
- **`README.md`** — one per service, for humans (run/build/test).
- **`CLAUDE.md`** — one per service, for agents (conventions, gotchas). Keep the hierarchy; just keep it *true*.
- **Design notes** — move to a single `docs/` per service, or delete once the code is the source of truth.
- **Delete the `AI/` scratch dirs** — migration plans belong in PRs/issues, not committed scratch.

---

## G. Claude Code setup — *outdated, confirmed* (user's explicit ask)

Current state: the *entire* setup is `.claude/settings.local.json` (a permission allow-list) plus the `CLAUDE.md` hierarchy — and the root `CLAUDE.md` is wrong (A1). No skills, no slash commands, no subagents, no output styles.

**Modernization plan (in order):**
1. **Fix root `CLAUDE.md` first.** It's read every session; correcting the layout, dropping the submodule/branch fiction, and fixing the stale port table is the single highest-leverage change in this whole review.
2. **Add repo-specific skills** under `.claude/skills/` (generic skills would waste the domain knowledge this repo has):
   - **`proto-change`** — "edit `proto/*.proto` → regenerate across all 7 consumers." Directly replaces the broken `update-proto`.
   - **`add-grpc-endpoint`** — scaffold a new RPC end-to-end (proto → server handler → gateway client → DTO/mapper) following the chosen conventions.
   - **`add-service`** — new service from the canonical layout + compose wiring + env file.
   - **`trace-purchase-flow`** — encode the canonical Waitroom → Order → Temporal → Payment → Kafka chain as a debugging aid.
3. Optional `.claude/commands/` for `make up` / regen, plus a `dev-doctor` that verifies compose build contexts exist (would have caught A2).
4. Remove the orphan `waitroom-svc/.claude` and consolidate/`.gitignore` the Cursor files.

---

## Prioritized roadmap (impact × effort × risk)

**P0 — ✅ DONE.** Root `CLAUDE.md` corrected (layout, ports, dropped submodule/branch fiction, MongoDB-mode-is-legacy note); all compose build contexts + LocalStack volume mounts repointed `../ticketbottle-*` → `../services/*-svc`; deleted `k8s/`, `aws/DIAGRAMS.md`, `order-svc/internal/infra/mongo` + empty `pkg/mongo`, orphan `waitroom-svc/.claude`, Cursor files; relocated payment `AI/` docs → `payment-svc/docs/` (preserved, not destroyed). *Outcome: the repo stops lying and the local stack resolves its build contexts.*

**P1 — ✅ DONE (interim, not buf yet).** Proto consolidated to a single source of truth (root `proto/`): repointed all TS `proto:*` scripts and Go `protoc`/`protoc-all` at `../../proto`, gutted the dead submodule logic in `update-protos.sh` / `update-proto`, fixed inventory's undefined `protoc-event`, deleted all **11** per-service copy dirs, removed the dead `@protos` tsconfig alias, added a root `make proto` orchestrator. **Validated:** Go regen is byte-identical to committed stubs; `protoc -I=../../proto` resolves cleanly (incl. `google/protobuf/empty`). Added four repo-specific skills under `.claude/skills/` (`proto-change`, `add-grpc-endpoint`, `add-service`, `trace-purchase-flow`). Documented the canonical TS layout in root `CLAUDE.md`.
  - *Still open in P1:* migrate from per-service `protoc` scripts to `buf generate` (one root module + `buf lint`/`buf breaking` in CI). TS stub regen needs a working `npm install` per service (pre-existing).

**P2 — refined after investigation (2026-06-15).** The original P2 framing was partly wrong; the code review for planning revealed:
  - The `any`-typed `BaseRepository` is **dead code** — nothing extends it and `user.service.ts` injects `PrismaService` directly. So this is a *delete*, not a refactor.
  - The enum mappers are **legitimate, not boilerplate** — proto enums are non-contiguous (`DRAFT=1, PUBLISHED=2, CONFIGURED=4`), so the status/role/currency mappers are necessary translation. **Keep them.** (Earlier "drop the mappers" recommendation retracted.)
  - The TS services have **near-zero test coverage** (user-svc 0, event-svc 0 specs), so a full layout-convergence refactor would need a test harness bigger than the refactor itself, for the lowest-ROI item. **Deferred — do not plan now.**

  Resulting prioritized P2 tiers (verification is golden-master/build-passes/docs-match, not red-green — there are no tests to red-green against):
  - **Tier 1 (safe, mechanical):** delete dead `user-svc/src/shared/repositories/`; (`development/` retired 2026-07-16 — kind is the sole local path, so its stale README is gone with it). → planned in `docs/superpowers/plans/2026-06-15-p2-tier1-quick-wins.md`.
  - **Tier 2:** migrate proto codegen to `buf` (single root module; removes the TS `src/protos` runtime copy; verify via golden stub-diff).
  - **Tier 3:** de-duplicate payment `lambdas/common` vs `src` (real overlap: prisma/kafka/logger/config; tricky because NestJS-DI vs plain-Node — scope tight; lambda `__tests__` exist as a partial net).
  - **Deferred:** TS layout convergence; (optional) shared Go `pkg` module.

---

*Bottom line: this is a well-conceived system carrying the scar tissue of several mid-flight migrations. You don't have an over-architecture problem; you have a* **cleanup-and-consolidation** *backlog plus stale guidance. Most of the wins are deletions.*
