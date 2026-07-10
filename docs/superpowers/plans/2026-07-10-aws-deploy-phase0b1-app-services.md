# AWS Deploy — Phase 0B-1: App Services, Config & Migrations — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy all 8 TicketBottle application services (user, event, inventory, waitroom, payment gRPC core, order-api, order-consumer, api-gateway) onto the Phase 0A `kind` infra tier via the same Helm chart, with DB migrations applied, and prove the gateway → user-svc → Postgres path works end-to-end (signup returns tokens).

**Architecture:** Reuse the `deploy/helm/ticketbottle` chart from Phase 0A. Add an `apps/` template group: one reusable named-template (`tb.appService`) that renders a Deployment (+ optional Service) per app service, per-service ConfigMaps holding the compose env remapped for the K8s infra, and Prisma migration Jobs (run from the builder-stage image, which has the `prisma` CLI). Images are built locally and `kind load`ed (no registry on Rung 1). App services keep their compose Service names (`event-service`, etc.) so the existing inter-service address env values resolve unchanged.

**Tech Stack:** Docker multi-stage builds (existing per-service Dockerfiles), kind, Helm 3/4, kubectl; NestJS gRPC (user/event/payment/gateway), Go gRPC (order/inventory/waitroom), Prisma, GORM.

## Plan series (where this fits)

Plan **2 of 5** in the affordable-AWS-deployment ladder (spec: `docs/superpowers/specs/2026-07-09-aws-affordable-deployment-ladder-design.md`). Phase 0B was split into 0B-1 (this doc) and 0B-2 for size.

| Plan | Scope | Gate |
|------|-------|------|
| 0A ✅ done | kind + Helm chart + infra tier | `make infra-up` + smoke green |
| **0B-1 (this doc)** | 8 app images, config, migrations, all app Deployments + gateway | **all app pods Ready + `POST /api/auth/signup` returns tokens** |
| 0B-2 | payment lambda-replacements (outbox-processor CronJob + webhook Deployment), full purchase-flow harness | **Gate 1** — full flow green on kind |
| 1 | Terraform + stoppable k3s EC2, same chart | Gate 2 — flow green on k3s + stop/start |
| 2 (optional) | ephemeral EKS | Gate 3 — flow green on EKS, clean destroy |

0B-2 is written after 0B-1's gate passes (it needs these Deployments running, plus a short investigation pass over `webhook.handler.ts`, the ZaloPay signature scheme, the lambda Docker build + `@/common` path-alias resolution, and the event/order create request bodies).

## Global Constraints

Copied verbatim from Phase 0A + the codebase — every task implicitly includes these:

- **Prerequisite:** Phase 0A is deployed and green (`cd deploy && make cluster-up && make infra-up && make smoke`). Infra Services exist: `postgres:5432`, `redis:6379`, `redpanda:9093`, `dynamodb:8000`, `temporal:7233`.
- **Namespace:** `ticketbottle`. Chart: `deploy/helm/ticketbottle`. Release: `tb`. Overlay: `values-local.yaml`.
- **K8s Service names MUST equal the compose service names** so existing inter-service env addresses resolve unchanged: `user-service`, `event-service`, `inventory-service`, `payment-service`, `order-service`, `waitroom-service`, `app-gateway`.
- **gRPC ports:** user `50052`, event `50053`, order-api `50054`, payment `50055`, waitroom `50056`, inventory `50057`. Gateway HTTP `3000`.
- **Config remap (compose → K8s infra), applied in every ConfigMap:**
  - `kafka:29092` → `redpanda:9093`
  - `postgres-user|event|payment|inventory:5432` → `postgres:5432` (keep the DB name in the URL)
  - `redis-waitroom:6379` / `redis-auth:6379` → `redis:6379`
  - `localstack:4566` → `dynamodb:8000` (order `DYNAMODB_ENDPOINT`)
  - `temporal:7233` → unchanged (same Service name)
- **Bind address:** all services bind `0.0.0.0` already (Go listens `:PORT`; NestJS `HOST=process.env.HOST||'0.0.0.0'`; payment hardcodes `0.0.0.0`). For user/event, set `HOST=0.0.0.0` in the ConfigMap explicitly; do **not** pass `.env.payment`'s ngrok `HOST` value anywhere.
- **Readiness probes:** gRPC services expose **no** health endpoint → use a **TCP** readiness probe on the gRPC port. Gateway → HTTP GET `/api` (or `/`).
- **Images:** built locally, tagged `ticketbottle/<svc>:local`, loaded with `kind load docker-image` (no registry on Rung 1). Prisma **migration** images use the `builder` stage: `ticketbottle/<svc>-migrate:local`.
- **Image pull policy:** `IfNotPresent` (images are side-loaded into kind, never pulled).
- **No app-source changes:** touch only `deploy/**`. Do not edit `services/**`.

---

### Task 1: Build all app images and load them into kind

**Files:**
- Create: `deploy/scripts/build-images.sh`

**Interfaces:**
- Produces (in the kind node's containerd): `ticketbottle/{user,event,payment,inventory,waitroom,order-api,order-consumer,gateway}:local` and `ticketbottle/{user,event,payment}-migrate:local`.

- [ ] **Step 1: Write the build/load script**

Create `deploy/scripts/build-images.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
CLUSTER=ticketbottle
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"   # repo root
cd "$ROOT"

build() { # <tag> <context> <dockerfile> [target]
  local tag=$1 ctx=$2 df=$3 target=${4:-}
  echo "==> build $tag"
  if [ -n "$target" ]; then
    docker build -t "$tag" -f "$df" --target "$target" "$ctx"
  else
    docker build -t "$tag" -f "$df" "$ctx"
  fi
  kind load docker-image "$tag" --name "$CLUSTER"
}

# Runtime images
build ticketbottle/user:local            services/user-svc           services/user-svc/Dockerfile
build ticketbottle/event:local           services/event-svc          services/event-svc/Dockerfile
build ticketbottle/payment:local         services/payment-svc        services/payment-svc/Dockerfile
build ticketbottle/inventory:local       services/inventory-svc      services/inventory-svc/Dockerfile
build ticketbottle/waitroom:local        services/waitroom-svc       services/waitroom-svc/Dockerfile
build ticketbottle/order-api:local       services/order-svc          services/order-svc/cmd/api/Dockerfile
build ticketbottle/order-consumer:local  services/order-svc          services/order-svc/cmd/consumer/Dockerfile
build ticketbottle/gateway:local         services/api-gateway        services/api-gateway/Dockerfile

# Prisma migration images (builder stage: has the prisma CLI + migrations)
build ticketbottle/user-migrate:local    services/user-svc           services/user-svc/Dockerfile    builder
build ticketbottle/event-migrate:local   services/event-svc          services/event-svc/Dockerfile   builder
build ticketbottle/payment-migrate:local services/payment-svc        services/payment-svc/Dockerfile builder

echo "ALL IMAGES BUILT AND LOADED"
```
Make executable: `chmod +x deploy/scripts/build-images.sh`

- [ ] **Step 2: Run it (first build is slow — npm ci + go build per service)**

Run:
```bash
cd deploy && ./scripts/build-images.sh
```
Expected: each `==> build …` completes, ends with `ALL IMAGES BUILT AND LOADED`. If a TS build fails on `npm ci`, that is expected to still work because the Dockerfile does a clean install inside the image (the committed host `node_modules` is irrelevant).

- [ ] **Step 3: Verify the images are present in the kind node**

Run:
```bash
docker exec ticketbottle-control-plane crictl images | grep ticketbottle
```
Expected: 11 `ticketbottle/*` image lines (8 runtime + 3 migrate).

- [ ] **Step 4: Commit**

```bash
git add deploy/scripts/build-images.sh
git commit -m "feat(deploy): build + kind-load all app images (Phase 0B-1)"
```

---

### Task 2: Per-service ConfigMaps (compose env remapped for K8s)

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/config.yaml`

**Interfaces:**
- Produces: ConfigMaps `user-config`, `event-config`, `inventory-config`, `waitroom-config`, `payment-config`, `order-config`, `gateway-config` — consumed via `envFrom` by Tasks 3–8.

- [ ] **Step 1: Write all app ConfigMaps**

Create `deploy/helm/ticketbottle/templates/apps/config.yaml` (values are the `.env.*` files with the Global-Constraints remap applied):
```yaml
apiVersion: v1
kind: ConfigMap
metadata: { name: user-config, namespace: {{ include "tb.namespace" . }} }
data:
  ENV: development
  HOST: "0.0.0.0"
  PORT: "50052"
  DATABASE_URL: postgresql://root:root@postgres:5432/ticketbottle_user
  DATABASE_HOST: postgres
  DATABASE_PORT: "5432"
  DATABASE_USERNAME: root
  DATABASE_PASSWORD: root
  DATABASE_NAME: ticketbottle_user
---
apiVersion: v1
kind: ConfigMap
metadata: { name: event-config, namespace: {{ include "tb.namespace" . }} }
data:
  HOST: "0.0.0.0"
  GRPC_PORT: "50053"
  NAME: TicketBottle_Event
  APP_GLOBAL_PREFIX: api
  CORS_ORIGINS: "[]"
  ENCRYPT_KEY: 3723723ry2h398rhfby824yrf032ihrbfiuewrt782u3yrh0d23hr9ghuwhfskdbmsdnc
  DATABASE_URL: postgresql://root:root@postgres:5432/ticketbottle_event
  DATABASE_HOST: postgres
  DATABASE_PORT: "5432"
  DATABASE_USERNAME: root
  DATABASE_PASSWORD: root
  DATABASE_NAME: ticketbottle_event
---
apiVersion: v1
kind: ConfigMap
metadata: { name: inventory-config, namespace: {{ include "tb.namespace" . }} }
data:
  ENV: development
  SERVER_GRPC_PORT: "50057"
  SERVER_READ_TIMEOUT: 30s
  SERVER_WRITE_TIMEOUT: 30s
  SERVER_IDLE_TIMEOUT: 60s
  LOG_LEVEL: info
  LOG_MODE: development
  LOG_ENCODING: console
  POSTGRES_URL: postgresql://root:root@postgres:5432/ticketbottle_inventory?sslmode=disable
  POSTGRES_MAX_OPEN_CONNS: "25"
  POSTGRES_MAX_IDLE_CONNS: "10"
  POSTGRES_CONN_MAX_LIFETIME: 5m
  POSTGRES_CONN_MAX_IDLE_TIME: 10m
---
apiVersion: v1
kind: ConfigMap
metadata: { name: waitroom-config, namespace: {{ include "tb.namespace" . }} }
data:
  ENV: development
  SERVER_GRPC_PORT: "50056"
  SERVER_READ_TIMEOUT: 30s
  SERVER_WRITE_TIMEOUT: 30s
  SERVER_IDLE_TIMEOUT: 60s
  REDIS_ADDR: redis:6379
  REDIS_PASSWORD: ""
  REDIS_DB: "0"
  REDIS_MAX_RETRIES: "3"
  REDIS_POOL_SIZE: "10"
  REDIS_MIN_IDLE_CONNS: "5"
  KAFKA_ENABLED: "true"
  KAFKA_BROKERS: redpanda:9093
  KAFKA_CONSUMER_GROUP_ID: waitroom-service
  KAFKA_PRODUCER_RETRY_MAX: "3"
  KAFKA_PRODUCER_REQUIRED_ACKS: "1"
  KAFKA_CONSUMER_SESSION_TIMEOUT: "10000"
  QUEUE_DEFAULT_MAX_CONCURRENT: "100"
  QUEUE_DEFAULT_RELEASE_RATE: "10"
  QUEUE_PROCESS_INTERVAL: 1s
  QUEUE_SESSION_TTL: 7200s
  QUEUE_POSITION_UPDATE_INTERVAL: 5s
  EVENT_SERVICE_ADDR: event-service:50053
  JWT_SECRET: 3723723ry2h398rhfby824yrf032ihrbfiuewrt782u3yrh0d23hr9ghuwhfskdbmsdnc
  JWT_EXPIRY: 15m
  LOGGER_LEVEL: debug
  LOGGER_MODE: development
  LOGGER_ENCODING: console
---
apiVersion: v1
kind: ConfigMap
metadata: { name: payment-config, namespace: {{ include "tb.namespace" . }} }
data:
  # HOST is hardcoded 0.0.0.0 in payment main.ts; do NOT pass the ngrok HOST from .env.payment.
  PORT: "8085"
  GRPC_PORT: "50055"
  NAME: TicketBottle_Payment
  APP_GLOBAL_PREFIX: api
  NODE_ENV: development
  LOG_LEVEL: debug
  ZALOPAY_APP_ID: "2554"
  ZALOPAY_KEY1: sdngKKJmqEMzvh5QQcdD2A9XBSKUNaYn
  ZALOPAY_KEY2: trMrHtvjo6myautxDUiAcYsVtaeQ8nhf
  DATABASE_URL: postgresql://root:root@postgres:5432/ticketbottle_payment
  DATABASE_HOST: postgres
  DATABASE_PORT: "5432"
  DATABASE_USERNAME: root
  DATABASE_PASSWORD: root
  DATABASE_NAME: ticketbottle_payment
  KAFKA_CLIENT_ID: payment-service
  KAFKA_BROKERS: redpanda:9093
  KAFKA_CONSUMER_GROUP_ID: payment-consumer-group
  KAFKA_SSL: "false"
  OUTBOX_POLL_INTERVAL_MS: "5000"
  OUTBOX_BATCH_SIZE: "100"
  OUTBOX_MAX_RETRIES: "5"
  OUTBOX_CLEANUP_DAYS: "7"
---
apiVersion: v1
kind: ConfigMap
metadata: { name: order-config, namespace: {{ include "tb.namespace" . }} }
data:
  ENV: development
  SERVER_GRPC_PORT: "50054"
  SERVER_READ_TIMEOUT: 30s
  SERVER_WRITE_TIMEOUT: 30s
  SERVER_IDLE_TIMEOUT: 60s
  PAYMENT_TIMEOUT_SECONDS: "600"
  DYNAMODB_TABLE_NAME: ticketbottle-orders
  AWS_REGION: us-east-1
  DYNAMODB_ENDPOINT: http://dynamodb:8000
  AWS_ACCESS_KEY_ID: local
  AWS_SECRET_ACCESS_KEY: local
  TEMPORAL_HOST_PORT: temporal:7233
  TEMPORAL_NAMESPACE: default
  JWT_SECRET: 3723723ry2h398rhfby824yrf032ihrbfiuewrt782u3yrh0d23hr9ghuwhfskdbmsdnc
  JWT_EXPIRY: 15m
  LOG_LEVEL: debug
  LOG_MODE: development
  LOG_ENCODING: console
  KAFKA_BROKERS: redpanda:9093
  KAFKA_PRODUCER_RETRY_MAX: "3"
  KAFKA_PRODUCER_REQUIRED_ACKS: "1"
  KAFKA_ENABLED: "true"
  KAFKA_CONSUMER_GROUP_ID: order-service
  EVENT_SERVICE_ADDR: event-service:50053
  INVENTORY_SERVICE_ADDR: inventory-service:50057
  PAYMENT_SERVICE_ADDR: payment-service:50055
---
apiVersion: v1
kind: ConfigMap
metadata: { name: gateway-config, namespace: {{ include "tb.namespace" . }} }
data:
  PORT: "3000"
  NAME: TicketBottle_Event
  APP_GLOBAL_PREFIX: api
  CORS_ORIGINS: '["*"]'
  APP_INIT_ADMIN_PASSWORD: initadminpassword
  ENCRYPT_KEY: 3723723ry2h398rhfby824yrf032ihrbfiuewrt782u3yrh0d23hr9ghuwhfskdbmsdnc
  JWT_ACCESS_SECRET: 92387r0293uhrfio2y39r78iudjeorf2o83eryhdip23ur09
  JWT_REFRESH_SECRET: 23789ryfh29eturfiyu2h3oqeiprdh2379erydh2ou3eiqr
  JWT_ACCESS_EXPIRATION: 1d
  JWT_REFRESH_EXPIRATION: 7d
  JWT_REFRESH_SLIDING_WINDOW: 24h
  SALT_ROUND: "10"
  INTERNAL_KEY: 2iytufy2u3eorfyg297ry8dhiqks23ryuegriuu2yehrbdlkhp
  USER_SERVICE: user-service:50052
  EVENT_SERVICE: event-service:50053
  INVENTORY_SERVICE: inventory-service:50057
  ORDER_SERVICE: order-service:50054
  WAITROOM_SERVICE: waitroom-service:50056
  REDIS_URL: redis://redis:6379
  REDIS_HOST: redis
  REDIS_PORT: "6379"
  REDIS_PASSWORD: ""
```

> Note: these are learning-grade secrets copied from the repo's committed `.env.*`; Phase 1 (k3s) moves the sensitive keys into a Secret. Redis is shared: waitroom and gateway both use `redis:6379` (different key prefixes; no collision at learning scale).

- [ ] **Step 2: Render and apply**

Run:
```bash
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml \
  --show-only templates/apps/config.yaml | head -5
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml >/dev/null
kubectl -n ticketbottle get configmaps | grep -E "user-config|event-config|inventory-config|waitroom-config|payment-config|order-config|gateway-config"
```
Expected: the render prints a ConfigMap header; the get lists all 7 config maps.

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/config.yaml
git commit -m "feat(deploy): per-service app ConfigMaps, env remapped for K8s (Phase 0B-1)"
```

---

### Task 3: Prisma migration Jobs (user, event, payment)

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/migrations.yaml`

**Interfaces:**
- Consumes: `*-migrate:local` images (Task 1), `*-config` ConfigMaps (Task 2), `postgres` (0A).
- Produces: applied schemas in `ticketbottle_user|event|payment`. Runs as pre-install/pre-upgrade Helm hooks so schemas exist before the Deployments start.

- [ ] **Step 1: Write the three migration Jobs**

Create `deploy/helm/ticketbottle/templates/apps/migrations.yaml`:
```yaml
{{- range $svc := list "user" "event" "payment" }}
---
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ $svc }}-migrate
  namespace: {{ include "tb.namespace" $ }}
  labels: {{- include "tb.labels" $ | nindent 4 }}
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "5"
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  backoffLimit: 10
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: migrate
          image: ticketbottle/{{ $svc }}-migrate:local
          imagePullPolicy: IfNotPresent
          command: ["sh","-c","npx prisma migrate deploy"]
          envFrom:
            - configMapRef: { name: {{ $svc }}-config }
      # wait for postgres before running (hook order doesn't guarantee DB readiness)
      initContainers:
        - name: wait-postgres
          image: postgres:15-alpine
          command: ["sh","-c","until pg_isready -h postgres -U root; do echo waiting for postgres; sleep 2; done"]
{{- end }}
```

> Note: uses the **builder-stage** image (`*-migrate:local`), which contains the `prisma` CLI + `prisma/migrations`. The runtime images omit devDependencies, so `npx prisma` would not resolve there. `migrate deploy` applies committed migrations idempotently (safe to re-run each upgrade). The migration Job reads `DATABASE_URL` from the service ConfigMap.

- [ ] **Step 2: Apply and verify migrations succeed**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml --timeout 5m >/dev/null && echo "upgraded (migration hooks ran)"
for s in user event payment; do
  echo "== $s tables =="
  kubectl -n ticketbottle exec statefulset/postgres -- \
    psql -U root -d ticketbottle_$s -tAc "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';"
done
```
Expected: `upgraded (migration hooks ran)`, then a nonzero table count for each of the three databases (proves each migration applied).

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/migrations.yaml
git commit -m "feat(deploy): Prisma migration Jobs for user/event/payment (Phase 0B-1)"
```

---

### Task 4: Reusable app-service template + deploy user-svc & event-svc

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/_appservice.tpl`
- Create: `deploy/helm/ticketbottle/templates/apps/user.yaml`
- Create: `deploy/helm/ticketbottle/templates/apps/event.yaml`

**Interfaces:**
- Produces: named template `tb.appService` (used by Tasks 4–8); Services `user-service:50052`, `event-service:50053`.
- `tb.appService` params (dict): `ctx` (root `.`), `name`, `image`, `port` (0 = no Service/port), `config` (ConfigMap name), `svcName` (Service name; omit to skip Service), `probe` ("tcp"|"http"|"none"), `nodePort` (optional).

- [ ] **Step 1: Write the reusable template**

Create `deploy/helm/ticketbottle/templates/apps/_appservice.tpl`:
```yaml
{{- define "tb.appService" -}}
{{- $ := .ctx -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .name }}
  namespace: {{ include "tb.namespace" $ }}
  labels: {{- include "tb.labels" $ | nindent 4 }}
spec:
  replicas: 1
  selector:
    matchLabels: { app: {{ .name }} }
  template:
    metadata:
      labels: { app: {{ .name }} }
    spec:
      containers:
        - name: {{ .name }}
          image: {{ .image }}
          imagePullPolicy: IfNotPresent
          envFrom:
            - configMapRef: { name: {{ .config }} }
          {{- if gt (int .port) 0 }}
          ports: [{ containerPort: {{ .port }} }]
          {{- end }}
          {{- if eq .probe "tcp" }}
          readinessProbe:
            tcpSocket: { port: {{ .port }} }
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 12
          {{- else if eq .probe "http" }}
          readinessProbe:
            httpGet: { path: /api, port: {{ .port }} }
            initialDelaySeconds: 5
            periodSeconds: 5
            failureThreshold: 24
          {{- end }}
{{- if .svcName }}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ .svcName }}
  namespace: {{ include "tb.namespace" $ }}
  labels: {{- include "tb.labels" $ | nindent 4 }}
spec:
  {{- if .nodePort }}
  type: NodePort
  {{- end }}
  selector: { app: {{ .name }} }
  ports:
    - port: {{ .port }}
      targetPort: {{ .port }}
      {{- if .nodePort }}
      nodePort: {{ .nodePort }}
      {{- end }}
{{- end }}
{{- end -}}
```

- [ ] **Step 2: Write user + event manifests using the template**

Create `deploy/helm/ticketbottle/templates/apps/user.yaml`:
```yaml
{{ include "tb.appService" (dict "ctx" . "name" "user-service" "image" "ticketbottle/user:local" "port" 50052 "config" "user-config" "svcName" "user-service" "probe" "tcp") }}
```

Create `deploy/helm/ticketbottle/templates/apps/event.yaml`:
```yaml
{{ include "tb.appService" (dict "ctx" . "name" "event-service" "image" "ticketbottle/event:local" "port" 50053 "config" "event-config" "svcName" "event-service" "probe" "tcp") }}
```

- [ ] **Step 3: Apply and verify both pods become Ready**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml --timeout 5m >/dev/null
kubectl -n ticketbottle rollout status deployment/user-service --timeout=120s
kubectl -n ticketbottle rollout status deployment/event-service --timeout=120s
kubectl -n ticketbottle get pods -l 'app in (user-service,event-service)'
```
Expected: both rollouts complete; both pods `1/1 Running`.

- [ ] **Step 4: Confirm they are actually serving gRPC (logs)**

Run:
```bash
kubectl -n ticketbottle logs deployment/user-service --tail=5
kubectl -n ticketbottle logs deployment/event-service --tail=5
```
Expected: startup logs with no crash; a "listening"/"started" style line (NestJS gRPC microservice boot). If a pod is `CrashLoopBackOff`, read full logs and fix before continuing (common causes: DB URL, missing env).

- [ ] **Step 5: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/_appservice.tpl deploy/helm/ticketbottle/templates/apps/user.yaml deploy/helm/ticketbottle/templates/apps/event.yaml
git commit -m "feat(deploy): reusable app-service template + user & event services (Phase 0B-1)"
```

---

### Task 5: Deploy inventory-svc & waitroom-svc

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/inventory.yaml`
- Create: `deploy/helm/ticketbottle/templates/apps/waitroom.yaml`

**Interfaces:**
- Consumes: `tb.appService`, `inventory-config`/`waitroom-config`, `postgres`/`redis`/`redpanda`/`event-service`.
- Produces: Services `inventory-service:50057`, `waitroom-service:50056`. inventory GORM-automigrates `ticketbottle_inventory` on boot.

- [ ] **Step 1: Write inventory + waitroom manifests**

Create `deploy/helm/ticketbottle/templates/apps/inventory.yaml`:
```yaml
{{ include "tb.appService" (dict "ctx" . "name" "inventory-service" "image" "ticketbottle/inventory:local" "port" 50057 "config" "inventory-config" "svcName" "inventory-service" "probe" "tcp") }}
```

Create `deploy/helm/ticketbottle/templates/apps/waitroom.yaml`:
```yaml
{{ include "tb.appService" (dict "ctx" . "name" "waitroom-service" "image" "ticketbottle/waitroom:local" "port" 50056 "config" "waitroom-config" "svcName" "waitroom-service" "probe" "tcp") }}
```

- [ ] **Step 2: Apply and verify**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml --timeout 5m >/dev/null
kubectl -n ticketbottle rollout status deployment/inventory-service --timeout=120s
kubectl -n ticketbottle rollout status deployment/waitroom-service --timeout=120s
echo "== inventory auto-migrated tables =="
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema='public';"
```
Expected: both rollouts complete; inventory table count is nonzero (GORM AutoMigrate ran on boot).

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/inventory.yaml deploy/helm/ticketbottle/templates/apps/waitroom.yaml
git commit -m "feat(deploy): inventory & waitroom services (Phase 0B-1)"
```

---

### Task 6: Deploy payment-svc (gRPC core)

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/payment.yaml`

**Interfaces:**
- Consumes: `tb.appService`, `payment-config`, `postgres`/`redpanda`.
- Produces: Service `payment-service:50055`. (The outbox-processor + webhook workloads are Phase 0B-2 — without them, payments write to the outbox but are not yet published to Kafka.)

- [ ] **Step 1: Write payment manifest**

Create `deploy/helm/ticketbottle/templates/apps/payment.yaml`:
```yaml
{{ include "tb.appService" (dict "ctx" . "name" "payment-service" "image" "ticketbottle/payment:local" "port" 50055 "config" "payment-config" "svcName" "payment-service" "probe" "tcp") }}
```

- [ ] **Step 2: Apply and verify**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml --timeout 5m >/dev/null
kubectl -n ticketbottle rollout status deployment/payment-service --timeout=120s
kubectl -n ticketbottle logs deployment/payment-service --tail=5
```
Expected: rollout completes; logs show a clean gRPC boot (no DB connection error).

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/payment.yaml
git commit -m "feat(deploy): payment gRPC core service (Phase 0B-1)"
```

---

### Task 7: Deploy order-api & order-consumer

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/order.yaml`

**Interfaces:**
- Consumes: `tb.appService`, `order-config`, `dynamodb`/`temporal`/`redpanda` + `event-service`/`inventory-service`/`payment-service`.
- Produces: Service `order-service:50054` (order-api). `order-consumer` runs as a Deployment with **no Service/port** (Kafka consumer only).

- [ ] **Step 1: Write order-api + order-consumer manifests**

Create `deploy/helm/ticketbottle/templates/apps/order.yaml`:
```yaml
{{ include "tb.appService" (dict "ctx" . "name" "order-service" "image" "ticketbottle/order-api:local" "port" 50054 "config" "order-config" "svcName" "order-service" "probe" "tcp") }}
---
{{ include "tb.appService" (dict "ctx" . "name" "order-consumer" "image" "ticketbottle/order-consumer:local" "port" 0 "config" "order-config" "probe" "none") }}
```

- [ ] **Step 2: Apply and verify**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml --timeout 5m >/dev/null
kubectl -n ticketbottle rollout status deployment/order-service --timeout=120s
kubectl -n ticketbottle rollout status deployment/order-consumer --timeout=120s
kubectl -n ticketbottle logs deployment/order-service --tail=8
kubectl -n ticketbottle logs deployment/order-consumer --tail=8
```
Expected: both rollouts complete. order-service logs show gRPC listening + a Temporal client connection; order-consumer logs show it connected to Redpanda + Temporal (no crash). If order-service can't reach Temporal/DynamoDB, fix env before continuing.

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/order.yaml
git commit -m "feat(deploy): order-api & order-consumer (Phase 0B-1)"
```

---

### Task 8: Deploy api-gateway + NodePort, and the 0B-1 gate (signup works)

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/gateway.yaml`
- Modify: `deploy/scripts/smoke-infra.sh` → add app-tier checks (or create `deploy/scripts/smoke-apps.sh`)
- Modify: `deploy/Makefile` (add `apps-up`, `smoke-apps`)

**Interfaces:**
- Consumes: everything above.
- Produces: Service `app-gateway` (NodePort 30000 → kind hostPort 3000). Gateway reachable at `http://localhost:3000/api`.

- [ ] **Step 1: Write the gateway manifest (NodePort on 30000, mapped to host 3000 by the kind config)**

Create `deploy/helm/ticketbottle/templates/apps/gateway.yaml`:
```yaml
{{ include "tb.appService" (dict "ctx" . "name" "app-gateway" "image" "ticketbottle/gateway:local" "port" 3000 "config" "gateway-config" "svcName" "app-gateway" "probe" "http" "nodePort" 30000) }}
```

- [ ] **Step 2: Write the app smoke script**

Create `deploy/scripts/smoke-apps.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
NS=ticketbottle
echo "== all app pods Ready =="
kubectl -n $NS wait --for=condition=Ready pod \
  -l 'app in (user-service,event-service,inventory-service,waitroom-service,payment-service,order-service,order-consumer,app-gateway)' \
  --timeout=180s
echo "== gateway signup (gateway -> user-svc -> postgres) =="
EMAIL="smoke+$(date +%s)@example.com"
CODE=$(curl -s -o /tmp/signup.json -w '%{http_code}' -X POST http://localhost:3000/api/auth/signup \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"Password123!\",\"firstName\":\"Smoke\",\"lastName\":\"Test\"}")
echo "  HTTP $CODE"
grep -qiE "accessToken|access_token" /tmp/signup.json && echo "  OK signup returned tokens" || { echo "  FAIL: $(cat /tmp/signup.json)"; exit 1; }
echo "ALL APP SMOKE CHECKS PASSED"
```
Make executable: `chmod +x deploy/scripts/smoke-apps.sh`

> Note: verified against `services/api-gateway/src/modules/auth/dtos/signup.dto.ts` — the fields are `firstName`, `lastName`, `email`, `password`, and `password` must satisfy `@IsStrongPassword` (8+ chars with upper/lower/number/symbol; `Password123!` qualifies). The body above matches exactly.

- [ ] **Step 3: Add Makefile targets**

Append to `deploy/Makefile`:
```makefile
.PHONY: apps-up smoke-apps

apps-up:               ## Build+load images and deploy the app tier
	./scripts/build-images.sh
	helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace -f $(LOCAL_VALUES) --wait --timeout 8m

smoke-apps:            ## Verify the app tier (pods Ready + signup)
	./scripts/smoke-apps.sh
```

- [ ] **Step 4: Apply, then run the gate**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml --wait --timeout 8m >/dev/null
kubectl -n ticketbottle get pods
cd deploy && make smoke-apps
```
Expected: all app pods `1/1 Running`; `make smoke-apps` ends with `ALL APP SMOKE CHECKS PASSED` (signup returned tokens). The Swagger UI is browsable at `http://localhost:3000/api`.

- [ ] **Step 5: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/gateway.yaml deploy/scripts/smoke-apps.sh deploy/Makefile
git commit -m "feat(deploy): api-gateway + NodePort + app smoke gate (Phase 0B-1)"
```

---

## Phase 0B-1 completion criteria

- `make cluster-up && make infra-up && make apps-up && make smoke && make smoke-apps` from clean is green.
- All 8 app pods (`user/event/inventory/waitroom/payment/order-service/order-consumer/app-gateway`) `1/1 Running`.
- `POST http://localhost:3000/api/auth/signup` returns access/refresh tokens (proves gateway → user-svc → Postgres).
- No `services/**` file modified.

**Next:** Phase 0B-2 — deploy the payment **outbox-processor** (CronJob) and **payment-webhook** (Deployment+Service) by building the `services/payment-svc/lambdas/` code into images with a small runner/HTTP shim, then a full purchase-flow harness (register → create+publish event → join waitroom → get admitted → create order → POST a signed simulated ZaloPay webhook → assert order COMPLETED + inventory sold + waitroom slot freed) = **Gate 1**.

## Self-review notes (author)

- **Spec coverage:** implements the app-tier half of spec §5/§6 (config via ConfigMaps, IAM deferred to k3s/EKS, DynamoDB via endpoint, no app-code change) and the "Deployments/probes" learning goal. Payment event-loop (outbox-processor/webhook) and the Gate-1 purchase flow (§7) are explicitly deferred to 0B-2 and named in the plan-series table.
- **Placeholder scan:** none — all manifests/commands are concrete; the signup smoke body was verified against `signup.dto.ts` inline.
- **Name/type consistency:** Service names match the inter-service env addresses (`*-service`), ports match the Global Constraints table, ConfigMap names (`<svc>-config`) are consistent between Task 2 and Tasks 4–8, `tb.appService` param names are consistent across all call sites, and infra hostnames match the 0A Services (`postgres`/`redis`/`redpanda`/`dynamodb`/`temporal`).
