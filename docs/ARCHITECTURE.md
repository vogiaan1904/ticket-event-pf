# TicketBottle V2 — Architecture Field Guide

*A walkthrough of **how this system is built and why** — each decision with its trade-off and the concepts behind it. For the architecture summary, service table, and ports, see [`README.md`](../README.md).*

---

## The 30-second mental model

A buyer never touches the internal services directly. They hit **one HTTP gateway**, which fans out to seven small services over fast internal calls. The dangerous moment — thousands of people buying the same tickets at once — is tamed in stages: a **waiting room** lets only a bounded number into checkout, **inventory** locks rows so nothing oversells, an **orchestrator** drives the multi-step purchase and undoes it cleanly if any step fails, and **events** flow between services afterwards so nobody blocks waiting on anybody else.

Everything below is a variation on one theme: **the domain is genuinely hard (concurrency + money + no single database), so the architecture spends complexity buying correctness and scale.** The skill is knowing which complexity is essential and which is just habit.

---

## The canonical data flow — a purchase, start to finish

Order matters: this is the real sequence a ticket purchase follows across the services.

| # | Step | What happens | Where |
|---|------|--------------|-------|
| 1 | Join the queue | User enters the virtual waiting room; gets a fair position | Waitroom · Redis ZSET |
| 2 | Get admitted | A background loop admits N users at a time and issues a checkout token | Waitroom → Kafka |
| 3 | Create order | Gateway calls Order; a durable saga workflow begins | Order · Temporal |
| 4 | Reserve tickets | Inventory locks rows and holds the quantity for ~15 min | Inventory · `SELECT … FOR UPDATE` |
| 5 | Payment intent | Payment creates the intent and returns a pay URL | Payment · gRPC |
| 6 | Pay & callback | Provider webhook marks paid; an outbox row is written atomically | Payment · Outbox |
| 7 | Confirm | Outbox → Kafka → Order confirms inventory; order is `COMPLETED` | Kafka → Temporal |
| 8 | Free the slot | Order tells Waitroom to release the checkout slot for the next person | Order → Waitroom |

---

## The design decisions

Each decision below lists **why** it exists, its **trade-off**, and the **concepts to learn** to understand it.

### 1 · System shape — many small services, not one big app

**Seven services in two languages.** The *hot path* (queue, inventory) has very different scaling and latency needs than *business CRUD* (users, events). Go handles the concurrency-heavy, performance-critical pieces; NestJS handles the richer business logic. Each service scales, deploys, and fails independently.
- **Trade-off:** network hops, distributed debugging, and ops overhead you don't have in a monolith. Justified by this domain — overkill for a low-traffic app.

**One HTTP front door (the API Gateway).** The gateway is the *only* service exposed to the internet. It centralizes auth, rate limiting, validation, and CORS, then translates REST into internal gRPC. Internal services stay private and never speak HTTP to the outside.
- **Trade-off:** a single choke point you must keep available and scale — but far simpler than every service doing its own auth.

### 2 · How services talk — sync when you need an answer, async when you don't

**gRPC for "I need a reply now."** Reserving inventory or creating a payment intent needs an *immediate answer*. gRPC gives typed contracts (Protocol Buffers), fast binary transport over HTTP/2, and generated stubs that keep every service in lock-step with the contract.
- **Trade-off:** not browser-native, needs a codegen toolchain, harder to poke at with curl than plain REST.

**Kafka for "tell others, don't wait."** Once a payment succeeds, the order confirms and the waiting room frees a slot — but none of those should *block* on each other. Publishing events to Kafka decouples producers from consumers, survives restarts, and can be replayed.
- **Trade-off:** eventual consistency, plus you must handle message ordering and duplicates. More infrastructure to run.

### 3 · Surviving the stampede — traffic control & no oversell

**Virtual waiting room — admit a bounded few at a time.** A hot on-sale is a *thundering herd*: everyone arrives in the same second. The waiting room queues them fairly (Redis sorted set) and admits only a capped number into checkout, so inventory and payment never get flooded.
- **Trade-off:** users wait, and you carry queue state plus a background loop that releases slots as they free up.

**Atomic inventory — lock the row, never oversell.** Two buyers must never claim the last seat. Every quantity change happens inside a transaction that *locks the row* (`SELECT … FOR UPDATE`). A three-step Reserve → Confirm / Release with a timed hold lets abandoned carts free themselves.
- **Trade-off:** locks cut concurrency versus an optimistic approach, and you need a sweeper to expire stale holds.

### 4 · Consistency without one big transaction

This is the hardest part: a purchase spans services **and** databases, so a single ACID transaction is impossible.

**Saga orchestrated by Temporal — a workflow that undoes itself on failure.** A purchase touches inventory, payment, and order in *separate databases*. A *saga* runs the steps and *compensates* (releases tickets, deletes the order) if a later step fails. Temporal makes that workflow durable, auto-retried, and crash-safe.
- **Trade-off:** a whole workflow engine to run and learn; every step must be idempotent; the result is eventual, not instantaneous.

**Transactional outbox — never lose an event mid-crash.** Payment must update its database *and* emit an event — but a crash between those two loses the event (the "dual-write problem"). The fix: write the event into an *outbox table in the same transaction*, then a relay publishes it to Kafka afterwards.
- **Trade-off:** delivery is at-least-once, so consumers must be idempotent, and you run a relay process to drain the outbox.

### 5 · Data — a different database for each job

**Polyglot persistence.** *Postgres* (relational, transactional) for users, events, payments, and inventory where locks and ACID matter. *DynamoDB* (single-table NoSQL) for orders, queried by known keys at scale. *Redis* (in-memory) for the queue and sessions where microsecond latency wins.
- **Trade-off:** several engines to operate, no cross-database joins, and you must model each store around its access patterns.

**CQRS in the Event service.** The event service separates its *write model* from its *read model* so the two can be optimized and scaled independently — reading an event catalog behaves very differently from writing during setup.
- **Trade-off:** more moving parts and eventual consistency between the two sides; only worth it where read/write shapes truly diverge.

### 6 · Running it — containers, Kubernetes, and a cost-aware cloud ladder

*(The chart, the values overlays, and the Terraform behind this live in [`deploy/`](../deploy/README.md).)*

**Every service is a container Kubernetes runs.** Each service ships as an *image*; Kubernetes runs, heals, and scales them the same way regardless of language. Stateful stores use *StatefulSets* with persistent volumes; stateless services use *Deployments*; *Services* handle discovery; config comes from ConfigMaps/Secrets.
- **Trade-off:** Kubernetes is a large surface to learn and operate — but it's the industry lingua franca for exactly this.

**One chart, several targets, all of it in code.** The same Helm chart deploys to a local *kind* cluster, to *k3s* on a single EC2 instance, and to *Amazon EKS* — the target is chosen by a values overlay plus a Terraform delta, never by forking a manifest. Terraform makes every environment reproducible and destroyable, and no workload ever holds a long-lived AWS key: CI authenticates by GitHub OIDC, nodes by instance profile, pods by IRSA.
- **Trade-off:** several targets to keep working, and each identity mechanism is its own thing to understand.

---

## An honest read on the current state

Knowing where this system is real and where it is stubbed is part of reading it.

- **Sound — the macro-architecture.** The queue → inventory → saga → payment shape is the right tool for high-traffic ticketing.
- **Gap — inventory isn't seeded by the app.** Nothing calls `CreateTicketClass` yet: the gateway's inventory module is an empty stub and the event service never links to inventory (its Kafka publish is a `// TODO`). End-to-end today needs tickets seeded directly — a real feature to finish.
- **Gap — payment events originated as AWS Lambdas.** Webhook handling and outbox publishing were split into SAM-coupled Lambdas. The Kubernetes deployment re-homes that logic as in-cluster workloads so the flow runs off AWS too.

---

## Where to read more, in the repo

| File | What's in it |
|------|--------------|
| [`README.md`](../README.md) | Architecture summary, services, ports, data flow |
| [`deploy/README.md`](../deploy/README.md) | Helm chart, values overlays, Terraform |
| `CLAUDE.md` (root + per service) | Conventions & gotchas |
