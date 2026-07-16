# Rung 1.5 — Local AWS Simulation: kind + LocalStack (AWS services only) — Design

*Date: 2026-07-15 (revised 2026-07-16). Status: approved for planning. Scope: add a **$0 "Rung 1.5"** to the [AWS affordable deployment ladder](2026-07-09-aws-affordable-deployment-ladder-design.md) that keeps the **existing kind cluster running everything it already runs** and swaps only the **AWS-native pieces** (order's datastore and the payment async path) to a **host-side LocalStack**, exercising real AWS APIs (DynamoDB, Lambda, API Gateway, IAM, EventBridge) locally. Delivered as a **Helm values overlay** on the existing chart — not a new stack.*

> **Supersedes an earlier draft of this file** that proposed re-hosting the whole stack in a standalone Docker Compose file. That approach was wrong: it **duplicated infra kind already runs** (Kafka/Temporal/Postgres/Redis) and created a parallel stack to maintain. This revision keeps kind as the single home for apps + infra and adds LocalStack only for the AWS surface.

---

## 1. Context & motivation

Phase 0 already delivered a **kind cluster** (Helm chart `deploy/helm/ticketbottle/`) running the full system green at **Gate 1**: 8 app services + **Redpanda** + **Temporal (no ES)** + **one Postgres/4 DBs** + **Redis** + **DynamoDB-local** + the Phase-0 **`payment-events` adapter** (a Node stand-in for the payment webhook + outbox→Kafka relay).

Two of those pieces are **local stand-ins for AWS services**:
- **DynamoDB-local** stands in for real DynamoDB.
- The **`payment-events` adapter** stands in for the real payment **Lambdas** (`services/payment-svc/lambdas/`: `payment-webhook-handler` behind API Gateway, `outbox-processor` on EventBridge, `outbox-cleanup`).

Rung 1.5 replaces **only those two** with **LocalStack-backed AWS services**, leaving everything else in kind untouched. This exercises the real AWS surface — **DynamoDB SDK + IAM, Lambda, API Gateway, EventBridge** — for **$0**, before any real-AWS spend, and revives the system's own already-built serverless design (the Lambda README documents exactly this hybrid: gRPC-on-Kubernetes + Lambda-for-async).

**Why this is faithful:** in real AWS, DynamoDB/Lambda/API Gateway are managed services **outside** your EKS cluster. Modeling them as a **LocalStack process outside kind** — with the cluster reaching *out* to them and the Lambdas reaching *back in* — mirrors that topology exactly.

---

## 2. Goals / non-goals

**Goals**
1. Run the full purchase flow on the **existing kind cluster** with order's datastore on **LocalStack DynamoDB** and the payment async path on the **real Lambdas** (API Gateway + EventBridge), replacing DynamoDB-local + the adapter.
2. Deliver it as a **`values-localstack.yaml` overlay** on the existing chart — on-thesis with the ladder's "one chart, values overlays."
3. Gain real, hands-on **AWS-API + cross-boundary-networking** learning: DynamoDB/IAM, Lambda, API Gateway, EventBridge, NodePorts, Kafka external listeners.
4. Keep the plain kind mode (DynamoDB-local + adapter, Gate 1) intact as the **default** — Rung 1.5 is purely additive.

**Non-goals**
- **No re-hosting of kind's infra.** Kafka/Redpanda, Temporal, Postgres, Redis stay as kind workloads. No standalone Compose stack duplicating them. (This is the abandoned approach — see the banner above.)
- No changes to app business logic, the saga, or the gRPC services. Config + operations + reviving the existing Lambda code only.
- No deploying these Lambdas to **real** AWS in this rung (that's a later Rung-2/3 serverless extension).
- No unit tests — verification is the end-to-end purchase flow (Gate 1.5).
- LocalStack **Pro** as a standing dependency — **community** is the target; Pro only if community can't attach Lambda layers.

---

## 3. Architecture — kind unchanged + host LocalStack for AWS only

```
        ┌────────────────────── kind cluster (unchanged) ───────────────────────┐
        │  api-gateway · user · event · payment(gRPC) · inventory · waitroom     │
        │  order-api · order-consumer   +   Redpanda · Temporal · Postgres · Redis│
        └──────────▲───────────────────────────────────────────────▲────────────┘
                   │ (1) DynamoDB SDK                                │ (2)(3) Lambdas reach
                   │     order-svc → LocalStack                      │     back into kind
                   ▼                                                 │     (NodePorts)
        ┌───────────────────────── LocalStack (host container, on the `kind` docker net) ─────────────┐
        │  DynamoDB · Lambda · API Gateway · EventBridge · IAM · STS · Logs                            │
        │  Lambdas: payment-webhook-handler (API GW) · outbox-processor (EventBridge) · outbox-cleanup │
        └─────────────────────────────────────────────────────────────────────────────────────────────┘
```

**LocalStack placement:** a **host-side Docker container** (a tiny `deploy/localstack/docker-compose.yml` running *only* LocalStack) attached to the **`kind` docker network** with a **static IP**, so (a) the Docker socket it needs for Lambda execution is natively available, and (b) the Lambda containers it spawns (`LAMBDA_DOCKER_NETWORK=kind`) share a network with the kind node and can reach kind's NodePorts. **No kind cluster recreate is required** — the Lambdas reach kind via the node container on the shared network, not via host port-mappings.

**The overlay:** `values-localstack.yaml` on the existing chart flips two switches:
| Switch | Effect |
|---|---|
| `dynamodb.enabled: false` | Don't deploy the DynamoDB-local pod/Service/init-Job; order-svc points at **LocalStack DynamoDB** instead. |
| `paymentEvents.enabled: false` | Don't deploy the `outbox-publisher` + `payment-webhook` adapter Deployments; the **real Lambdas** take over. |
| `localstack.expose: true` (new) | Add NodePort Services for **payment-Postgres** and **Redpanda** (external listener) so the Lambdas can reach them. |

Both `dynamodb.yaml` and `payment-events.yaml` gain `{{- if }}` guards (they have none / only `apps.enabled` today).

---

## 4. Networking — the core work of this rung

Three cross-boundary hops, each a deliberate learning beat:

1. **order-svc (kind pod) → LocalStack DynamoDB.** LocalStack sits on the `kind` docker network at a **static IP**. A headless k8s `Service` + `Endpoints` named `localstack` (in `ticketbottle` ns) targets that IP; order-svc uses `DYNAMODB_ENDPOINT=http://localstack.ticketbottle.svc.cluster.local:4566`, `AWS_REGION=us-east-1`, dummy creds (or IAM). Pod egress routes through the node to the LocalStack container.
2. **payment-webhook-handler Lambda → payment Postgres (kind).** Postgres is exposed via a **NodePort**; the Lambda (on the `kind` net) connects at `ticketbottle-control-plane:<nodePort>`. `DATABASE_URL` in the Lambda env points there.
3. **outbox-processor Lambda → Redpanda (kind).** Redpanda gets a **second (external) advertised listener** on a NodePort advertising `ticketbottle-control-plane:<nodePort>`; the Lambda's `KAFKA_BROKERS` points there. Internal clients (order-consumer, payment-svc) keep using the existing internal listener `redpanda:9093` unchanged.

The gate harness (on the host) reaches **API Gateway** at `localhost:4566` (LocalStack publishes 4566 to the host) and invokes `outbox-processor` via host `awslocal`.

**Connectivity is validated incrementally before wiring the full flow** (a "order-svc writes to LocalStack DynamoDB" smoke and a "Lambda reads payment Postgres" smoke), so each hop is proven in isolation.

---

## 5. Data flow (only the changed touchpoints)

```
create order → order-svc writes order to  LocalStack DynamoDB   (was: dynamodb-local)   [hop 1]
  → order-svc → payment-svc createPaymentIntent (kind gRPC, unchanged)
  → payment-svc writes outbox row to payment Postgres (kind, unchanged)
  → [gate fires ZaloPay-signed webhook] → API Gateway (LocalStack)
        → payment-webhook-handler Lambda → payment COMPLETED + PAYMENT_COMPLETED outbox row
              (Lambda → payment Postgres in kind)                                        [hop 2]
  → outbox-processor Lambda (EventBridge ~1min, or invoked by the gate)
        → publishes payment.completed to Redpanda in kind                               [hop 3]
  → order-consumer (kind) consumes → Temporal ConfirmOrder
        → confirms inventory (Postgres, kind) + updates order in LocalStack DynamoDB
  → order COMPLETED, waitroom slot freed
```

Everything except the three hops is **identical to the current kind Gate-1 flow**.

---

## 6. Components & files

**New — `deploy/localstack/` (host side):**
- `docker-compose.yml` — runs **only** LocalStack (community), on the `kind` network, static IP, `LAMBDA_DOCKER_NETWORK=kind`, publishes 4566, mounts the Docker socket + the lambda `build/` dir + order-svc's `init-dynamodb.sh`.
- `deploy-lambdas.sh` — thin wrapper over the existing `services/payment-svc/lambdas/scripts/deploy-to-dev-compose.sh` (publishes layers, creates the 3 functions, wires API Gateway) pointed at the LocalStack-for-kind env.
- `env.lambdas.json` — Lambda env for this rung: `DATABASE_URL` → payment-Postgres NodePort, `KAFKA_BROKERS` → Redpanda external NodePort, ZaloPay keys.
- `gate.sh` — the purchase-flow gate, kubectl-based (like `deploy/scripts/gate1-purchase-flow.sh`) with the payment step swapped to a ZaloPay-signed webhook → API Gateway + a direct `outbox-processor` invoke.
- `README.md`, `Makefile` — runbook + `up`/`lambdas`/`gate`/`down` targets.

**Modified — Helm chart:**
- `templates/infra/dynamodb.yaml` — add `{{- if .Values.dynamodb.enabled }}` guard.
- `templates/apps/payment-events.yaml` — add `{{- if .Values.paymentEvents.enabled }}` guard.
- New `templates/infra/localstack-bridge.yaml` (guarded) — the `localstack` Service+Endpoints (to the static IP), the payment-Postgres NodePort, and the Redpanda external listener/NodePort.
- `values.yaml` — add `dynamodb.enabled: true`, `paymentEvents.enabled: true`, `localstack.expose: false` defaults.
- `values-localstack.yaml` — the overlay: the two `false` switches, `localstack.expose: true`, order-svc `DYNAMODB_ENDPOINT` → the `localstack` Service.
- Redpanda template — add the external listener (advertised on the NodePort) alongside the internal one.

**Reused as-is:** the three Lambdas + build tooling under `services/payment-svc/lambdas/` (rebuilt clean — layers + zips, with the Linux Prisma engine).

---

## 7. Acceptance — Gate 1.5 (single gate)

Full purchase flow green on the **kind cluster with the `values-localstack` overlay**: order persisted in **LocalStack DynamoDB**, payment completed via the **`payment-webhook-handler` Lambda through API Gateway**, and `payment.completed` published by the **`outbox-processor` Lambda** into kind's Redpanda → order reaches `COMPLETED`, inventory decremented, waitroom slot freed.

- Gate is on **DynamoDB + webhook-handler + outbox-processor** (the critical path). `outbox-cleanup` is deployed for completeness but not gated.
- No "untrimmed vs trimmed" two-step — kind is already the trimmed tier; there is exactly **one** gate.
- $0. This milestone de-risks the serverless payment ingress + real DynamoDB before any AWS spend.

---

## 8. Cost, edition, risks

- **Cost: $0.** LocalStack **community** (Lambda + API Gateway + DynamoDB + EventBridge all supported); fall back to **Pro** only if community can't publish/attach layers.
- **The 4 GB VPS is not used here** — it remains reserved as the future Rung-2 remote box.

| Risk | Mitigation |
|---|---|
| Pod → LocalStack (hop 1) reachability | LocalStack static IP on the `kind` net + k8s `Service`/`Endpoints`; validated by an isolated DynamoDB smoke before the full flow. |
| Lambda → kind Postgres/Redpanda (hops 2–3) | NodePorts + Lambdas on the `kind` net (reach the node container); Redpanda external advertised listener; each hop smoke-tested in isolation. |
| Redpanda dual-listener misconfig | Add the external listener without touching the working internal one; internal clients unchanged; verified by `rpk` + the existing flow. |
| Community LocalStack can't attach Lambda layers | Fall back to Pro, or bundle deps into each function zip. Isolated to lambda-deploy. |
| Signing a valid ZaloPay webhook in the gate | `mac = HMAC_SHA256(data, ZALOPAY_KEY2)` from `env.localstack.json`; minted in the harness like the checkout JWT already is. |
| Committed Lambda `build/` zips stale | Rebuilt clean (`npm run build:layers`, Node 20) with the `rhel-openssl-3.0.x` Prisma engine before deploy. |

---

## 9. Open decisions / deferred

- **Confirmed:** LocalStack = host-side container (not a kind pod); community edition first; deploy all 3 Lambdas, gate on webhook + processor; keep plain kind mode as default.
- **Deferred to the plan:** exact NodePort numbers + the Redpanda external-listener stanza; whether order-svc reaches LocalStack via the `Service`/`Endpoints` name or a direct static IP (default: Service/Endpoints); PayOS path (default: ZaloPay only for the gate).
- **Later / not built here:** deploying these Lambdas to real AWS (Rung-2/3 serverless extension); cleaning up the legacy `development/` compose dir (separate housekeeping).

---

*Bottom line: keep the kind cluster exactly as it is; stand up a host-side LocalStack for the AWS-native services only; flip two switches via a `values-localstack` overlay so order uses real DynamoDB and the real payment Lambdas replace the adapter. Same chart, one overlay, three cross-boundary network hops — real AWS + K8s-networking learning, $0, no duplicated infra.*
