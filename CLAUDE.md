# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

TicketBottle V2 is a polyglot microservices platform for high-traffic ticket sales (virtual queue → atomic inventory → saga-orchestrated order → multi-provider payment). It is a **single monorepo with one git history** — every service lives under `services/<name>-svc/` (the `api-gateway` dir has no `-svc` suffix). The `.proto` contracts are shared from the root `proto/` directory.

Each service has its own `CLAUDE.md` with service-specific detail — read that file when working inside a service.

See `README.md` for the architecture overview and the end-to-end purchase data flow, and `docs/ARCHITECTURE.md` for the longer design walkthrough of each decision. `deploy/README.md` covers the Helm chart, its per-target values overlays, and the Terraform under `deploy/terraform/`.

## Decision register

Before proposing an architectural change, find the question here. If a row owns
it, read that document and work from its decision instead of deriving a new one.
If the question is open, it is yours to decide.

**A fact has exactly one home; everywhere else points at it.** `35e864f` removed
`SELECT … FOR UPDATE` from the reserve path, and three documents went on
describing it for a week — because the fact had been copied rather than linked.

### Decided

| Question | Owned by | State |
|---|---|---|
| How is the stateful tier split, and onto what? | `docs/design/eks-stateful-tier.md` | (a), (a2) built; (b)–(f) specified, unbuilt |
| What bounds `Reserve` throughput on one hot ticket class? | `services/inventory-svc/CLAUDE.md` | Measured 2026-09-22 |
| In what order are queued buyers admitted? | `services/waitroom-svc/CLAUDE.md` | Draw before sale open, arrival after; nothing admitted before the sale opens; 2026-09-23 |
| Do the waitroom's stampede changes survive an end-to-end run? | `docs/plans/2026-09-23-waitroom-admission-correctness.md` | Yes, on k3s 2026-09-23, after five defects were fixed |
| How does a waiting client learn its position and token? | `services/waitroom-svc/CLAUDE.md` | Polling; the push stream was removed 2026-09-23 |
| Who decides what, and what is the agent's role? | `docs/decisions/0002` | Accepted 2026-09-22 |
| What runs where, and how does it authenticate? | `.claude/skills/deployment-architecture` | Built through the k3s and ephemeral-EKS targets |
| How does a failure become a gRPC code, then an HTTP status? | This file, *Error taxonomy* | Binding |
| What does every workload publish, and how is it queried? | `docs/METRICS.md` | Binding |
| Which failures page, and which never do? | This file, *Alerting policy* | Binding |
| Where does rationale live — comment, design, or plan? | This file, *Comment conventions*; `docs/README.md` | Binding |
| Where is a decision's why recorded, and how is it reviewed? | `docs/decisions/README.md` | One record per decision, drafted when made; reviewed from the generated index |
| Where does an agent start to learn a part of the system, and which document wins? | `.claude/skills/system-map` | A map of documents, not a copy of them; CI fails on a gone path or an uncited document; 2026-09-23 |
| Does the gateway rate-limit and set security headers? | `docs/decisions/0013` | Helmet everywhere; sign-in and sign-up throttled per client IP, purchase never; 2026-09-24 |
| Which TS layout is the target for new structure? | `docs/design/ts-layout.md` | The 2026-06-16 rule, re-adopted 2026-09-24; no service converged yet |

### Open

| Question | Why it is open |
|---|---|
| How many buyers may hold inventory at once, and can it vary per event? | `QUEUE_DEFAULT_MAX_CONCURRENT: 100` is global. `queue_processor.go` reads it once at construction, so `MaxConcurrentPerEvent` is per-event in name only — a 100k on-sale and a 500-seat show cannot be tuned apart. The per-event config path now exists (`internal/service/event_gate.go`), so this is a field and a knob, not a new mechanism. |
| What fails first as concurrent checkouts rise into the thousands? | Unmeasured. Temporal's persistence shares the app's Postgres (`templates/infra/temporal.yaml:28`) against a stock `max_connections=100`, and its load scales with in-flight orders — so it is the suspect, but that is a reading, not a measurement. |
| Are the deferred stateful-tier phases (b)–(f) the next work? | The ranking that deferred them dissolved on 2026-09-23: the ceiling they were postponed for is not reachable. Nothing has replaced the ranking. |
| Does `Confirm` contend on the hot row enough to matter? | `confirmReservationTx` holds the same `ticket_class` row to `COMMIT`, and every completed purchase pays it. Only `Reserve` has been measured. |
| Why does the gateway send order fields the contract no longer has? | `src/protogen/order.pb.ts` is stale: it describes orders keyed by `id` with offset pagination, while the runtime `src/protos/order.proto` is cursor-based. Regenerating breaks `src/modules/orders/`, which is written against the old shape. |

## Services & ports

Ports below are the **authoritative** values (from each service's config/`main`); the root `README.md` repeats them for human readers.

| Service | Dir | Lang/Framework | Port | Protocol | Datastore |
|---------|-----|----------------|------|----------|-----------|
| API Gateway | `services/api-gateway` | TS / NestJS | 3000 | HTTP/REST + Swagger | — (gRPC client to all services) |
| User | `services/user-svc` | TS / NestJS | 50052 | gRPC | PostgreSQL (Prisma) |
| Event | `services/event-svc` | TS / NestJS | 50053 | gRPC | PostgreSQL (Prisma) |
| Order | `services/order-svc` | Go | 50054 | gRPC | DynamoDB |
| Payment | `services/payment-svc` | TS / NestJS | 50055 | gRPC | PostgreSQL (Prisma) |
| Waitroom | `services/waitroom-svc` | Go | 50056 | gRPC | Redis |
| Inventory | `services/inventory-svc` | Go | 50057 | gRPC | PostgreSQL (GORM) |

`proto/` is **not a service** — it is the shared `.proto` contract source (see "Proto contracts & generation" below).

## Architecture in one paragraph

The **API Gateway** is the only HTTP entry point; everything behind it is gRPC. The **Order** service is the saga orchestrator: it drives a **Temporal** workflow that calls Event → Inventory → Payment synchronously over gRPC, and compensates on failure. Cross-service eventual consistency flows over **Kafka** (dotted topic names: `queue.ready`, `payment.completed`, `checkout.completed`, `order.refund_required`, and their failure counterparts). Canonical chain: Waitroom admits a user → Gateway calls Order → Temporal `CreateOrder` reserves inventory, writes the order, then creates a payment intent → payment webhook → Payment writes an outbox row → the relay publishes it to Kafka → Order's `ConfirmOrder` workflow confirms inventory and completes the order → Waitroom frees the checkout slot.

## Communication patterns (where to look when tracing a flow)

- **Synchronous gRPC** — request/response needing an immediate answer (Order→Inventory reserve, Order→Payment intent, Gateway→everything). Contracts live in `proto/`.
- **Asynchronous Kafka** — event notifications / eventual consistency (Payment→Order, Order→Waitroom).
- **Temporal workflows** (Order service only) — long-running, stateful, auto-compensating saga steps.

## Development and deployment

**Single-service** work runs the service natively against its own
`docker-compose.dev.yml`, which starts only that service's datastore (`docker compose
down -v` when done). There is no local full-stack cluster: kind was removed for disk,
and the Docker Compose stack under `development/` before it.

The **full stack** runs on **k3s on one EC2 instance**, from the same Helm chart that
deploys to EKS; the target is a values overlay (`values-k3s.yaml`, `values-eks.yaml`)
plus the Terraform under `deploy/terraform/`. Images are built only by CI and pushed to
ECR. Operations live in `deploy/Makefile`:

```bash
make -C deploy start-ec2-k3s   # start the box; prints the SSH tunnel to open
make -C deploy k3s-kubeconfig
make -C deploy k3s-deploy      # helm upgrade from the :dev images in ECR
make -C deploy k3s-gate2       # full purchase-flow acceptance test
make -C deploy stop-ec2-k3s    # the cost switch
```

Per-service config is baked into the chart's ConfigMaps (`deploy/helm/ticketbottle/templates/apps/config.yaml`), **not** env files. The API Gateway is reachable at `localhost:3000` through the tunnel (NodePort 30000).

## Branches

Two branches: **`main`** and **`dev`**. Start platform work from `dev`; integrate
`dev` → `main`. `main` is the branch a reader lands on and is kept presentable.

CI (`.github/workflows/build-push-ecr.yml`) builds and pushes to ECR on a push to
either one.

Do **not** reintroduce a `staging` branch. Nothing builds or deploys from one, so
it is a promotion step that only ever goes stale.

## Proto contracts & generation

There is **one source of truth: the root `proto/` directory.** Edit contracts there, then regenerate in every consumer. Generated code is committed (TS under `src/protogen/`, Go under `pkg/grpc/` or `protogen/`), so a fresh checkout builds without regenerating.

- **TS services** (`api-gateway`, `event`, `user`, `payment`): `npm run update:proto` — syncs the contracts into the service's `src/protos/` and runs `proto:all` (`protoc` + `ts-proto`, `-I=../../proto`) into `src/protogen/`. **Why `src/protos/` exists:** the NestJS gRPC transport loads `.proto` at *runtime* (`protoPath`) and `nest-cli.json` copies `src/protos/**` into `dist` as build assets — so each TS service needs a local copy. It is a **generated, synced-from-root** artifact: never hand-edit it, edit `proto/` and re-run `update:proto`.
- **Go services** (`order`, `inventory`, `waitroom`): `make protoc-all` — runs `protoc` with the Go plugins against `../../proto` into `pkg/grpc/<svc>/` (waitroom: `protogen/`). Go uses **compiled stubs only** (no runtime `.proto`), so Go services keep **no** local `.proto` copy.

Do **not** reintroduce the old redundant copies (`protos/`, `protos-submodule/`) — those were dead submodule remnants. The only legitimate local copy is each TS service's `src/protos/`, kept in sync by `update:proto`. (Target state is `buf generate` from a single root module, which removes even the TS copy.)

## Error taxonomy (binding for every service)

A failure crosses two boundaries: domain error → gRPC code → HTTP status. The
gRPC code is the contract; the API Gateway maps it to HTTP in
`common/filters/global-exception.filter.ts`, from the one table in
`shared/metrics/code.ts`, and nowhere else.

| gRPC code | HTTP | Means | Client should |
|---|---|---|---|
| `INVALID_ARGUMENT` | 400 | The request is malformed | Fix the request |
| `UNAUTHENTICATED` | 401 | Missing or invalid credentials | Re-authenticate |
| `PERMISSION_DENIED` | 403 | Authenticated but not allowed | Stop |
| `NOT_FOUND` | 404 | The entity does not exist | Stop or refetch |
| `ALREADY_EXISTS` | 409 | Duplicate create | Treat as success, or refetch |
| `FAILED_PRECONDITION` | 409 | Valid request, world is in the wrong state | Refetch; do not retry identically |
| `DEADLINE_EXCEEDED` | 504 | We did not answer in time | Retry |
| `UNAVAILABLE` | 503 | A dependency is down | Retry with backoff |
| `INTERNAL` | 500 | **We have a bug** | Retry later; page someone |

**The governing rule: `INTERNAL` means we have a bug.** If a business outcome can
produce it, the mapping is wrong. Sold out, sale closed, queue full and wrong-state
are all `FAILED_PRECONDITION` — a buyer losing a race is not a server fault.

`RESOURCE_EXHAUSTED` means only that the gateway throttled this client — its own
429 on sign-in and sign-up. No service returns it, and no business outcome maps to
it: a sold-out buyer told they were rate-limited would retry a show that is gone.
The gateway labels a downstream `RESOURCE_EXHAUSTED` `INTERNAL`.

**The code is required, not defaulted.** Go services take it as the first argument
of `pkgErrors.NewGRPCError(codes.X, "ID", "message")`, so omitting it does not
compile; `response.GrpcError` has no fallback. TS services carry it as the third
element of the `ErrorCode` tuple — `[message, httpStatus, grpcCode]` — so a missing
one fails `tsc`. Neither side has a default: a silent fallback is how an error
ends up with the wrong class.

## Metric contract (binding for every service)

Every workload publishes `tb_grpc_requests_total`, `tb_grpc_request_duration_seconds`
and `tb_grpc_in_flight` on port 2112, labelled `service` / `method` / `code`.
`service` is the workload's own name; `code` is the gRPC code from the taxonomy
above, which the gateway records too even though it serves HTTP.

**Histogram buckets are tuned per workload** — a boundary is only worth a series
where that workload's latency lands — and that forces one rule:

> **Every query over `tb_grpc_request_duration_seconds` names its workload:** a
> `service` matcher, or `service` kept in the `by` list.

`sum by (le)` across mismatched boundaries builds a curve `histogram_quantile`
silently clamps, so the query returns a plausible wrong number rather than an
error. Other histograms have a single publisher and need no matcher.

The checkout SLO — **99% of `POST /api/orders` under 2s** — is measured at the
gateway, not on the saga's `CreateOrder`, so `2` must stay a real boundary
in the gateway's array. Full contract, bucket tables and the cardinality
conditions: `docs/METRICS.md`.

**Metrics are absent, not zero, until traffic creates them.** A `*Vec` registers
no series until a label combination is used, so after any rollout every counter
and histogram in the contract is missing until the first request. Assert on
metrics *after* driving load, never before, and never read an empty query result
as a fault before checking whether traffic has happened.

## Alerting policy (binding for every service)

**The error taxonomy is the alerting policy.** `INTERNAL` pages on any sustained
rate above zero; `FAILED_PRECONDITION` never pages, because a buyer losing a race
is not a fault and paging on it turns a successful on-sale into an incident. That
absence is asserted, not merely intended — no rule in
`templates/apps/prometheusrule.yaml` may reference `FAILED_PRECONDITION`.

The one alert that fires on that code, `OrdersNeedingRefund`, decides on the
**ledger rather than the code**: a sold-out buyer was never charged, while a
`REFUND_REQUIRED` order was. Same code, opposite obligations.

Working on metrics, dashboards, alerts or PromQL: read the `observability` skill
first — it carries the query failure modes, the scrape wiring, and the
Helm-versus-Prometheus templating collision that breaks the chart render.


## Comment conventions (binding for every service)

A comment earns its place only by saying what the code cannot. It is read at a
glance or not at all, so it is budgeted like code, not written like prose.

**Budget — hard limits.**

| Where | Limit |
|---|---|
| Inline, inside a function body | **3 lines** |
| Doc comment on a symbol | **5 lines**, first line one sentence: `// X does Y.` |
| Package doc | 8 lines |

The budget counts **prose** lines. An indented case table or step list does not
count against it — that form is the point — but the whole block stays under 10.

**No paragraphs.** A block of running prose explaining a design decision is not
a comment — it is documentation in the wrong file. If the rationale does not fit
the budget, put it in the service's `docs/` and leave a one-line pointer:

```go
// Sized so an in-flight create is never mistaken for an abandoned one.
// See docs/PURCHASE_SLOT.md#settle-window.
```

**Compress with structure, not sentences.** Branching or multi-case reasoning
goes in a form the eye can scan:

```go
// pending | completed  -> resume, return the same checkout
// cancelled | failed   -> release the slot, let the retry take it
// unknown              -> refuse; guessing double-sells or strands
```

Single-line prefixes carry the rest: `// Why:`, `// Invariant:`,
`// Trade-off:`, `// Fails when:`.

**Never write.** Restatements of the line below; narrative history ("this used
to...", "changed because..."); walkthroughs of what a *different* function
does; justification aimed at a reviewer rather than the next reader.

A comment that only makes sense to someone who saw the bug is the worst case:
it reads as noise once the bug is forgotten. State the invariant instead.
`.claude/hooks/check-comment-budget.py` flags both on every write.

## Conventions that span services

- **Go services** (`order`, `inventory`, `waitroom`) share a layout: `cmd/<binary>/main.go` → `internal/{delivery,service(s),repository,models}` → shared `pkg/` (logger, errors, grpc, response, util). Logging uses the custom zap wrapper with `f`-suffixed, ctx-first methods: `l.Errorf(ctx, "...", err)`, `l.Infof(ctx, "...")`. (The "use error vars, never `fmt.Errorf`" rule is **Order-specific** — see `services/order-svc/CLAUDE.md`; the other Go services use `fmt.Errorf` freely.)
- **TS services** (`api-gateway`, `event`, `user`, `payment`) are NestJS. The gateway boots an HTTP app (`NestFactory.create`); the others boot gRPC microservices (`NestFactory.createMicroservice`, `Transport.GRPC`). Prisma services use `prisma migrate dev` / migrations under `prisma/`.
  - **Layout** — the target is `docs/design/ts-layout.md`: a flat controller, one
    transport `dto/`, domain shapes in `<feature>.types.ts`, `infra/` for adapters,
    and additions by archetype so a small service stays small. **No service follows
    it yet**, so none is a template: a new module follows the design, not its
    neighbours, and an existing service converges whole, never one module at a time.

## Documentation layout

`docs/` separates two kinds of document, because they have different lifespans:
**`docs/design/<name>.md`** is a target state, named and undated, read
repeatedly. **`docs/plans/YYYY-MM-DD-<name>.md`** is a work order derived from a
design, dated because it records a decision made on a day, and read once. A
completed plan says so in its first lines. Plans address whoever does the work,
never a tool. Full rules: `docs/README.md`.

To find which document owns a question and how far to trust it, load the
`system-map` skill. A design document added without a place on its map fails CI.

When you change a system-wide rule or invariant, update this file. When a
question above is settled, move its row from *Open* to *Decided* and name the
document that now owns it; when a new design doc is written, give it a row. A
register that lags is worse than none, because its silence reads as "nobody has
decided this."
