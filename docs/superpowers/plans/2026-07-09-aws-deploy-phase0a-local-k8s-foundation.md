# AWS Deploy — Phase 0A: Local Kubernetes Foundation & Infra Tier — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the full TicketBottle *infrastructure tier* (Postgres, Redis, Redpanda, DynamoDB-local, Temporal) on a local `kind` Kubernetes cluster via one portable Helm chart, with each component individually verified — the free substrate that Phase 0B (app services) deploys onto.

**Architecture:** A single Helm umbrella chart (`deploy/helm/ticketbottle`) rendered against a `values-local.yaml` overlay, installed into a `kind` cluster. The heavyweight tier is trimmed per the spec to fit one box: **Redpanda** replaces Kafka+Zookeeper (Kafka-API compatible, clients unchanged), **Temporal runs without Elasticsearch** (Postgres/SQL visibility), **one Postgres instance hosts all databases** (four app DBs + Temporal's two), and **DynamoDB-local** replaces LocalStack. All stateful components run as StatefulSets with PVCs.

**Tech Stack:** kind, kubectl, Helm 3, Docker; postgres:15-alpine, redis:7-alpine, redpandadata/redpanda, amazon/dynamodb-local, temporalio/auto-setup:1.27.2.

## Plan series (where this fits)

This is plan **1 of 4** in the affordable-AWS-deployment ladder (spec: `docs/superpowers/specs/2026-07-09-aws-affordable-deployment-ladder-design.md`):

| Plan | Scope | Deliverable / Gate |
|------|-------|--------------------|
| **0A (this doc)** | Local `kind` + Helm chart skeleton + **infra tier** (Postgres/Redis/Redpanda/DynamoDB-local/Temporal) | `make infra-up` → all infra pods Ready + each component smoke-verified |
| **0B** | ConfigMaps/Secrets, DB migrations, **8 app services** + **2 payment lambda-replacements** (outbox-processor CronJob, webhook Deployment), gateway ingress, **end-to-end purchase-flow harness** | **Gate 1** — full purchase flow green on `kind` (no AWS spend before this) |
| **1** | Terraform (budget alarm first, VPC, stoppable EC2, DynamoDB, ECR, IAM), install k3s, deploy same chart via `values-k3s.yaml`, `make stop/start` | **Gate 2** — flow green on k3s EC2 + stop/start preserves data |
| **2** *(optional)* | Terraform minimal EKS (spot), deploy same chart via `values-eks.yaml` (ALB + IRSA), verify, `terraform destroy` | **Gate 3** — flow green on EKS, clean destroy |

**0B/1/2 are separate plan documents** written after their predecessor's gate passes (each needs its predecessor's artifacts to exist before its steps can be made concrete and non-placeholder). This document fully covers 0A only.

## Global Constraints

Copied verbatim from the spec + the codebase reality — every task implicitly includes these:

- **Region / naming:** DynamoDB table is `ticketbottle-orders` (single-table: PK/SK + GSI1 (GSI1PK/GSI1SK) + GSI2 (GSI2PK/GSI2SK)). `AWS_REGION=us-east-1`.
- **Postgres:** one instance, superuser `root` / password `root`. App databases: `ticketbottle_user`, `ticketbottle_event`, `ticketbottle_payment`, `ticketbottle_inventory`. Temporal databases: `temporal`, `temporal_visibility` (created by Temporal auto-setup, which needs the `root` superuser).
- **Kafka API endpoint (Redpanda):** in-cluster brokers address is `redpanda:9093` (the value app configs will use in 0B in place of the compose `kafka:29092`).
- **Temporal:** address `temporal:7233`, namespace `default`, **`ENABLE_ES=false`** (Postgres visibility). Image pinned `temporalio/auto-setup:1.27.2` (matches compose).
- **DynamoDB-local endpoint:** `http://dynamodb:8000` (the value `order-svc`'s `DYNAMODB_ENDPOINT` will use in 0B in place of the compose `http://localstack:4566`).
- **Namespace:** everything installs into the `ticketbottle` Kubernetes namespace.
- **No app changes:** this plan touches only new files under `deploy/`. Do not edit any `services/**` source.
- **Chart release name:** `tb`. Helm chart path: `deploy/helm/ticketbottle`.
- **Image pins (use exactly):** `postgres:15-alpine`, `redis:7-alpine`, `redpandadata/redpanda:v24.2.7`, `amazon/dynamodb-local:2.5.2`, `amazon/aws-cli:2.17.0`, `temporalio/auto-setup:1.27.2`.

---

### Task 1: Local tooling check + `kind` cluster

**Files:**
- Create: `deploy/kind/cluster.yaml`
- Create: `deploy/Makefile`
- Create: `deploy/.gitignore`

**Interfaces:**
- Produces: a running `kind` cluster named `ticketbottle` with kubectl context `kind-ticketbottle`; `make` targets `cluster-up`, `cluster-down` in `deploy/Makefile`.

- [ ] **Step 1: Verify required tools exist (this is the "test")**

Run:
```bash
for t in docker kubectl helm kind; do printf "%s: " "$t"; command -v "$t" >/dev/null && "$t" version --short 2>/dev/null | head -1 || echo "MISSING"; done
```
Expected: a version line for each. If any prints `MISSING`, install it first:
- macOS: `brew install kind kubernetes-cli helm` (Docker Desktop provides `docker`).
Do not proceed until all four resolve.

- [ ] **Step 2: Write the kind cluster config**

Create `deploy/kind/cluster.yaml`:
```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ticketbottle
nodes:
  - role: control-plane
    # extraPortMappings let us reach a NodePort Service (api-gateway, added in 0B)
    # and Temporal/Redpanda dashboards from the host without port-forward.
    extraPortMappings:
      - containerPort: 30000   # api-gateway NodePort (0B)
        hostPort: 3000
        protocol: TCP
```

- [ ] **Step 3: Write `deploy/Makefile` (cluster targets only for now)**

Create `deploy/Makefile`:
```makefile
# TicketBottle — local Kubernetes (Rung 1) operations.
CLUSTER := ticketbottle
CTX     := kind-$(CLUSTER)
NS      := ticketbottle

.PHONY: cluster-up cluster-down cluster-status

cluster-up:            ## Create the local kind cluster
	kind create cluster --config kind/cluster.yaml
	kubectl --context $(CTX) create namespace $(NS) --dry-run=client -o yaml | kubectl --context $(CTX) apply -f -

cluster-down:          ## Delete the local kind cluster
	kind delete cluster --name $(CLUSTER)

cluster-status:        ## Show nodes and namespaces
	kubectl --context $(CTX) get nodes
	kubectl --context $(CTX) get ns $(NS)
```

Create `deploy/.gitignore`:
```gitignore
# Rendered/temp artifacts
*.tmp
charts/*.tgz
```

- [ ] **Step 4: Create the cluster and verify it is Ready**

Run:
```bash
cd deploy && make cluster-up && kubectl --context kind-ticketbottle get nodes
```
Expected: `kind create cluster` finishes, then a node line:
```
NAME                       STATUS   ROLES           AGE   VERSION
ticketbottle-control-plane   Ready    control-plane   ...   v1.x
```
Status must be `Ready` and namespace `ticketbottle` created.

- [ ] **Step 5: Commit**

```bash
git add deploy/kind/cluster.yaml deploy/Makefile deploy/.gitignore
git commit -m "feat(deploy): local kind cluster + deploy Makefile (Phase 0A)"
```

---

### Task 2: Helm umbrella chart skeleton

**Files:**
- Create: `deploy/helm/ticketbottle/Chart.yaml`
- Create: `deploy/helm/ticketbottle/values.yaml`
- Create: `deploy/helm/ticketbottle/values-local.yaml`
- Create: `deploy/helm/ticketbottle/templates/_helpers.tpl`
- Create: `deploy/helm/ticketbottle/.helmignore`

**Interfaces:**
- Consumes: nothing.
- Produces: an installable (empty) chart; the `tb.labels` / `tb.namespace` helpers and the `postgres`/`redis`/`redpanda`/`dynamodb`/`temporal` value blocks that Tasks 3–7 read.

- [ ] **Step 1: Write `Chart.yaml`**

Create `deploy/helm/ticketbottle/Chart.yaml`:
```yaml
apiVersion: v2
name: ticketbottle
description: TicketBottle V2 — portable deployment chart (infra tier + app services)
type: application
version: 0.1.0
appVersion: "2.0.0"
```

- [ ] **Step 2: Write `values.yaml` (shared defaults)**

Create `deploy/helm/ticketbottle/values.yaml`:
```yaml
# Shared defaults. Overlays (values-local/-k3s/-eks) override per target.
namespace: ticketbottle

postgres:
  image: postgres:15-alpine
  storage: 2Gi
  user: root
  password: root
  # app databases created by the init ConfigMap (Temporal DBs are created by Temporal itself)
  databases:
    - ticketbottle_user
    - ticketbottle_event
    - ticketbottle_payment
    - ticketbottle_inventory

redis:
  image: redis:7-alpine
  storage: 1Gi

redpanda:
  image: redpandadata/redpanda:v24.2.7
  storage: 2Gi

dynamodb:
  image: amazon/dynamodb-local:2.5.2
  awsCliImage: amazon/aws-cli:2.17.0
  tableName: ticketbottle-orders
  region: us-east-1

temporal:
  image: temporalio/auto-setup:1.27.2
  enableElasticsearch: false   # SQL/Postgres visibility — spec §4
```

- [ ] **Step 3: Write `values-local.yaml` (kind overlay)**

Create `deploy/helm/ticketbottle/values-local.yaml`:
```yaml
# kind overlay: smallest footprint, no resource pressure.
target: local
postgres:
  storage: 1Gi
redpanda:
  storage: 1Gi
```

- [ ] **Step 4: Write `_helpers.tpl`**

Create `deploy/helm/ticketbottle/templates/_helpers.tpl`:
```yaml
{{- define "tb.namespace" -}}
{{ .Values.namespace }}
{{- end -}}

{{- define "tb.labels" -}}
app.kubernetes.io/part-of: ticketbottle
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
```

Create `deploy/helm/ticketbottle/.helmignore`:
```gitignore
*.md
*.tmp
```

- [ ] **Step 5: Lint and render (the "test")**

Run:
```bash
helm lint deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml >/dev/null && echo "RENDER_OK"
```
Expected: `1 chart(s) linted, 0 chart(s) failed` and `RENDER_OK` (no templates yet, so render is empty but valid).

- [ ] **Step 6: Commit**

```bash
git add deploy/helm/ticketbottle
git commit -m "feat(deploy): Helm umbrella chart skeleton + values overlays (Phase 0A)"
```

---

### Task 3: Postgres StatefulSet with all databases

**Files:**
- Create: `deploy/helm/ticketbottle/templates/infra/postgres.yaml`

**Interfaces:**
- Consumes: `.Values.postgres.*`, `tb.labels`.
- Produces: Service `postgres:5432` reachable in-cluster; databases `ticketbottle_user|event|payment|inventory` created; superuser `root`/`root` available for Temporal (Task 7) to create its own DBs.

- [ ] **Step 1: Write the Postgres manifests (init ConfigMap + StatefulSet + Service)**

Create `deploy/helm/ticketbottle/templates/infra/postgres.yaml`:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: postgres-init
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
data:
  01-create-databases.sql: |
    {{- range .Values.postgres.databases }}
    SELECT 'CREATE DATABASE {{ . }}' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '{{ . }}')\gexec
    {{- end }}
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels: { app: postgres }
  template:
    metadata:
      labels: { app: postgres }
    spec:
      containers:
        - name: postgres
          image: {{ .Values.postgres.image }}
          ports: [{ containerPort: 5432 }]
          env:
            - { name: POSTGRES_USER, value: "{{ .Values.postgres.user }}" }
            - { name: POSTGRES_PASSWORD, value: "{{ .Values.postgres.password }}" }
            - { name: POSTGRES_DB, value: "{{ .Values.postgres.user }}" }
            - { name: PGDATA, value: /var/lib/postgresql/data/pgdata }
          volumeMounts:
            - { name: data, mountPath: /var/lib/postgresql/data }
            - { name: init, mountPath: /docker-entrypoint-initdb.d }
          readinessProbe:
            exec: { command: ["pg_isready", "-U", "{{ .Values.postgres.user }}"] }
            initialDelaySeconds: 5
            periodSeconds: 5
      volumes:
        - name: init
          configMap: { name: postgres-init }
  volumeClaimTemplates:
    - metadata: { name: data }
      spec:
        accessModes: ["ReadWriteOnce"]
        resources: { requests: { storage: "{{ .Values.postgres.storage }}" } }
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  selector: { app: postgres }
  ports: [{ port: 5432, targetPort: 5432 }]
```

> Note: the `\gexec` idempotent-create trick runs in `psql` (the init scripts are executed by the postgres entrypoint with `psql`), so re-applies are safe.

- [ ] **Step 2: Install just this template and watch it fail-then-pass**

Run (install the chart with only Task 3's template present):
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle \
  -f deploy/helm/ticketbottle/values-local.yaml
kubectl -n ticketbottle rollout status statefulset/postgres --timeout=120s
```
Expected: `statefulset rolling update complete 1 pods ready`.

- [ ] **Step 3: Verify all four app databases exist**

Run:
```bash
kubectl -n ticketbottle exec statefulset/postgres -- \
  psql -U root -tAc "SELECT datname FROM pg_database WHERE datname LIKE 'ticketbottle_%' ORDER BY 1;"
```
Expected (exactly these four lines):
```
ticketbottle_event
ticketbottle_inventory
ticketbottle_payment
ticketbottle_user
```

- [ ] **Step 4: Commit**

```bash
git add deploy/helm/ticketbottle/templates/infra/postgres.yaml
git commit -m "feat(deploy): Postgres StatefulSet with all app databases (Phase 0A)"
```

---

### Task 4: Redis

**Files:**
- Create: `deploy/helm/ticketbottle/templates/infra/redis.yaml`

**Interfaces:**
- Consumes: `.Values.redis.*`, `tb.labels`.
- Produces: Service `redis:6379`. Waitroom will use logical DB 0, gateway auth logical DB 1 (configured in 0B).

- [ ] **Step 1: Write the Redis manifests**

Create `deploy/helm/ticketbottle/templates/infra/redis.yaml`:
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  serviceName: redis
  replicas: 1
  selector:
    matchLabels: { app: redis }
  template:
    metadata:
      labels: { app: redis }
    spec:
      containers:
        - name: redis
          image: {{ .Values.redis.image }}
          ports: [{ containerPort: 6379 }]
          volumeMounts: [{ name: data, mountPath: /data }]
          readinessProbe:
            exec: { command: ["redis-cli", "ping"] }
            initialDelaySeconds: 3
            periodSeconds: 5
  volumeClaimTemplates:
    - metadata: { name: data }
      spec:
        accessModes: ["ReadWriteOnce"]
        resources: { requests: { storage: "{{ .Values.redis.storage }}" } }
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  selector: { app: redis }
  ports: [{ port: 6379, targetPort: 6379 }]
```

- [ ] **Step 2: Apply and verify**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml
kubectl -n ticketbottle rollout status statefulset/redis --timeout=60s
kubectl -n ticketbottle exec statefulset/redis -- redis-cli ping
```
Expected: rollout complete, then `PONG`.

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/ticketbottle/templates/infra/redis.yaml
git commit -m "feat(deploy): Redis StatefulSet (Phase 0A)"
```

---

### Task 5: Redpanda (Kafka-API broker)

**Files:**
- Create: `deploy/helm/ticketbottle/templates/infra/redpanda.yaml`

**Interfaces:**
- Consumes: `.Values.redpanda.*`, `tb.labels`.
- Produces: Kafka-API broker at `redpanda:9093` (the address app configs use in 0B). Auto-topic-creation on (Redpanda dev default), so the services' topics are created on first produce.

- [ ] **Step 1: Write the Redpanda manifests (single-node dev mode)**

Create `deploy/helm/ticketbottle/templates/infra/redpanda.yaml`:
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redpanda
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  serviceName: redpanda
  replicas: 1
  selector:
    matchLabels: { app: redpanda }
  template:
    metadata:
      labels: { app: redpanda }
    spec:
      containers:
        - name: redpanda
          image: {{ .Values.redpanda.image }}
          # Use args (NOT command): the image ENTRYPOINT is /entrypoint.sh which routes
          # `redpanda start ...` through `rpk`. `rpk redpanda start` understands --mode/
          # --kafka-addr; the raw `redpanda` broker binary does not. Overriding `command`
          # bypasses the entrypoint and fails with "unrecognised option '--mode'".
          args:
            - redpanda
            - start
            - --mode=dev-container
            - --smp=1
            # advertise the in-cluster service name so clients (0B) reach the broker
            - --kafka-addr=PLAINTEXT://0.0.0.0:9093
            - --advertise-kafka-addr=PLAINTEXT://redpanda:9093
          ports:
            - { containerPort: 9093 }   # Kafka API
            - { containerPort: 9644 }   # Admin API (health)
          volumeMounts: [{ name: data, mountPath: /var/lib/redpanda/data }]
          readinessProbe:
            httpGet: { path: /v1/status/ready, port: 9644 }
            initialDelaySeconds: 5
            periodSeconds: 5
  volumeClaimTemplates:
    - metadata: { name: data }
      spec:
        accessModes: ["ReadWriteOnce"]
        resources: { requests: { storage: "{{ .Values.redpanda.storage }}" } }
---
apiVersion: v1
kind: Service
metadata:
  name: redpanda
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  selector: { app: redpanda }
  ports:
    - { name: kafka, port: 9093, targetPort: 9093 }
    - { name: admin, port: 9644, targetPort: 9644 }
```

- [ ] **Step 2: Apply and verify the broker is up**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml
kubectl -n ticketbottle rollout status statefulset/redpanda --timeout=120s
kubectl -n ticketbottle exec statefulset/redpanda -- rpk cluster info --brokers localhost:9093
```
Expected: rollout complete, then cluster info showing 1 broker (`redpanda` / id 0).

- [ ] **Step 3: Verify topic create/produce/consume round-trips (Kafka-API compatibility check)**

Run:
```bash
kubectl -n ticketbottle exec statefulset/redpanda -- rpk topic create smoke --brokers localhost:9093
kubectl -n ticketbottle exec statefulset/redpanda -- bash -lc 'echo hello | rpk topic produce smoke --brokers localhost:9093'
kubectl -n ticketbottle exec statefulset/redpanda -- rpk topic consume smoke --num 1 --brokers localhost:9093
kubectl -n ticketbottle exec statefulset/redpanda -- rpk topic delete smoke --brokers localhost:9093
```
Expected: the consume prints a record whose `"value": "hello"`.

- [ ] **Step 4: Commit**

```bash
git add deploy/helm/ticketbottle/templates/infra/redpanda.yaml
git commit -m "feat(deploy): Redpanda single-node Kafka-API broker (Phase 0A)"
```

---

### Task 6: DynamoDB-local + table-init Job

**Files:**
- Create: `deploy/helm/ticketbottle/templates/infra/dynamodb.yaml`

**Interfaces:**
- Consumes: `.Values.dynamodb.*`, `tb.labels`.
- Produces: Service `dynamodb:8000`; table `ticketbottle-orders` (PK/SK + GSI1 + GSI2) ACTIVE. `order-svc` reaches it in 0B via `DYNAMODB_ENDPOINT=http://dynamodb:8000`.

- [ ] **Step 1: Write DynamoDB-local Deployment + Service + create-table Job**

Create `deploy/helm/ticketbottle/templates/infra/dynamodb.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dynamodb
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  replicas: 1
  selector:
    matchLabels: { app: dynamodb }
  template:
    metadata:
      labels: { app: dynamodb }
    spec:
      containers:
        - name: dynamodb
          image: {{ .Values.dynamodb.image }}
          # -inMemory keeps it light; data is recreated by the init Job each install.
          command: ["java","-jar","DynamoDBLocal.jar","-inMemory","-sharedDb"]
          ports: [{ containerPort: 8000 }]
          readinessProbe:
            tcpSocket: { port: 8000 }
            initialDelaySeconds: 3
            periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: dynamodb
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  selector: { app: dynamodb }
  ports: [{ port: 8000, targetPort: 8000 }]
---
apiVersion: batch/v1
kind: Job
metadata:
  name: dynamodb-init
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": post-install,post-upgrade
    "helm.sh/hook-weight": "0"
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
spec:
  backoffLimit: 10
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: create-table
          image: {{ .Values.dynamodb.awsCliImage }}
          env:
            - { name: AWS_ACCESS_KEY_ID, value: "local" }
            - { name: AWS_SECRET_ACCESS_KEY, value: "local" }
            - { name: AWS_REGION, value: "{{ .Values.dynamodb.region }}" }
            - { name: EP, value: "http://dynamodb:8000" }
            - { name: TABLE, value: "{{ .Values.dynamodb.tableName }}" }
          command:
            - /bin/sh
            - -c
            - |
              set -e
              until aws dynamodb list-tables --endpoint-url "$EP" >/dev/null 2>&1; do
                echo "waiting for dynamodb..."; sleep 2; done
              if aws dynamodb describe-table --endpoint-url "$EP" --table-name "$TABLE" >/dev/null 2>&1; then
                echo "table $TABLE already exists"; exit 0; fi
              aws dynamodb create-table --endpoint-url "$EP" --table-name "$TABLE" \
                --attribute-definitions \
                  AttributeName=PK,AttributeType=S AttributeName=SK,AttributeType=S \
                  AttributeName=GSI1PK,AttributeType=S AttributeName=GSI1SK,AttributeType=S \
                  AttributeName=GSI2PK,AttributeType=S AttributeName=GSI2SK,AttributeType=S \
                --key-schema AttributeName=PK,KeyType=HASH AttributeName=SK,KeyType=RANGE \
                --global-secondary-indexes \
                  'IndexName=GSI1,KeySchema=[{AttributeName=GSI1PK,KeyType=HASH},{AttributeName=GSI1SK,KeyType=RANGE}],Projection={ProjectionType=ALL},ProvisionedThroughput={ReadCapacityUnits=5,WriteCapacityUnits=5}' \
                  'IndexName=GSI2,KeySchema=[{AttributeName=GSI2PK,KeyType=HASH},{AttributeName=GSI2SK,KeyType=RANGE}],Projection={ProjectionType=ALL},ProvisionedThroughput={ReadCapacityUnits=5,WriteCapacityUnits=5}' \
                --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5
              aws dynamodb wait table-exists --endpoint-url "$EP" --table-name "$TABLE"
              echo "table $TABLE created"
```

> Note: schema mirrors `services/order-svc/scripts/init-dynamodb.sh` exactly (PK/SK + GSI1 + GSI2). `-inMemory` means the init Job re-creates the table on every `helm upgrade`; that's fine for local.

- [ ] **Step 2: Apply and verify the table is ACTIVE**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml
kubectl -n ticketbottle wait --for=condition=complete job/dynamodb-init --timeout=120s
kubectl -n ticketbottle run ddb-check --rm -i --restart=Never --image=amazon/aws-cli:2.17.0 \
  --env AWS_ACCESS_KEY_ID=local --env AWS_SECRET_ACCESS_KEY=local --env AWS_REGION=us-east-1 -- \
  dynamodb describe-table --endpoint-url http://dynamodb:8000 --table-name ticketbottle-orders \
  --query 'Table.TableStatus' --output text
```
Expected: job `complete`, then `ACTIVE`.

- [ ] **Step 3: Commit**

```bash
git add deploy/helm/ticketbottle/templates/infra/dynamodb.yaml
git commit -m "feat(deploy): DynamoDB-local + orders table init Job (Phase 0A)"
```

---

### Task 7: Temporal (no Elasticsearch, Postgres visibility)

**Files:**
- Create: `deploy/helm/ticketbottle/templates/infra/temporal.yaml`

**Interfaces:**
- Consumes: `.Values.temporal.*`, `.Values.postgres.*`, `tb.labels`.
- Produces: Temporal frontend at `temporal:7233`, namespace `default` registered. `order-svc` (0B) connects via `TEMPORAL_HOST_PORT=temporal:7233`.

- [ ] **Step 1: Write the Temporal manifests**

Create `deploy/helm/ticketbottle/templates/infra/temporal.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: temporal
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  replicas: 1
  selector:
    matchLabels: { app: temporal }
  template:
    metadata:
      labels: { app: temporal }
    spec:
      containers:
        - name: temporal
          image: {{ .Values.temporal.image }}
          # auto-setup creates the temporal + temporal_visibility databases and schema
          # in the shared Postgres (needs the root superuser), then registers "default".
          env:
            - { name: DB, value: postgres12 }
            - { name: DB_PORT, value: "5432" }
            - { name: POSTGRES_SEEDS, value: postgres }
            - { name: POSTGRES_USER, value: "{{ .Values.postgres.user }}" }
            - { name: POSTGRES_PWD, value: "{{ .Values.postgres.password }}" }
            - { name: DBNAME, value: temporal }
            - { name: VISIBILITY_DBNAME, value: temporal_visibility }
            - { name: ENABLE_ES, value: "{{ .Values.temporal.enableElasticsearch }}" }
            - { name: SKIP_DB_CREATE, value: "false" }
            # Listen on all interfaces so the Service (and 0B clients like order-svc) can
            # reach the frontend. auto-setup defaults to 127.0.0.1, which the Service can't
            # route to — and do NOT set TEMPORAL_ADDRESS to the Service name (see probe note).
            - { name: BIND_ON_IP, value: "0.0.0.0" }
          ports: [{ containerPort: 7233 }]
          readinessProbe:
            exec:
              # Target localhost, NOT the Service name. A Service has no endpoints until its
              # pod is Ready, so a Service-addressed probe deadlocks (never Ready). auto-setup's
              # own namespace registration hits 127.0.0.1 by default for the same reason.
              command: ["tctl", "--address", "127.0.0.1:7233", "cluster", "health"]
            initialDelaySeconds: 20
            periodSeconds: 10
            failureThreshold: 12
---
apiVersion: v1
kind: Service
metadata:
  name: temporal
  namespace: {{ include "tb.namespace" . }}
  labels: {{- include "tb.labels" . | nindent 4 }}
spec:
  selector: { app: temporal }
  ports: [{ port: 7233, targetPort: 7233 }]
```

> Note on `ENABLE_ES`: Helm renders the bool `false` as the string `"false"` here (quoted), which auto-setup reads correctly. Verify in Step 3 that no Elasticsearch is expected.

- [ ] **Step 2: Apply and wait for Temporal to become healthy**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml
kubectl -n ticketbottle rollout status deployment/temporal --timeout=240s
```
Expected: `deployment "temporal" successfully rolled out` (first boot runs schema setup, so allow up to ~4 min).

- [ ] **Step 3: Verify the `default` namespace and that visibility is SQL (not ES)**

Run:
```bash
kubectl -n ticketbottle exec deployment/temporal -- tctl --address temporal:7233 namespace list | grep -i "Name:"
kubectl -n ticketbottle exec deployment/temporal -- sh -c 'echo "advanced-visibility store = $ENABLE_ES (expect false)"'
kubectl -n ticketbottle logs deployment/temporal | grep -iE "visibility" | head -3
```
Expected: namespace list includes `Name: default`; `ENABLE_ES` prints `false`; logs do not reference an Elasticsearch host.

- [ ] **Step 4: Commit**

```bash
git add deploy/helm/ticketbottle/templates/infra/temporal.yaml
git commit -m "feat(deploy): Temporal (Postgres visibility, no Elasticsearch) (Phase 0A)"
```

---

### Task 8: One-command infra bring-up + teardown + full smoke gate

**Files:**
- Modify: `deploy/Makefile` (add infra targets)
- Create: `deploy/scripts/smoke-infra.sh`
- Create: `deploy/README.md`

**Interfaces:**
- Consumes: Tasks 1–7.
- Produces: `make infra-up`, `make infra-down`, `make smoke` — the Phase 0A gate.

- [ ] **Step 1: Add infra targets to `deploy/Makefile`**

Append to `deploy/Makefile`:
```makefile
CHART := helm/ticketbottle
RELEASE := tb
LOCAL_VALUES := $(CHART)/values-local.yaml

.PHONY: infra-up infra-down smoke

infra-up:              ## Install/upgrade the infra tier on the local cluster
	helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace -f $(LOCAL_VALUES) --wait --timeout 5m

infra-down:            ## Uninstall the chart (keeps the cluster)
	helm uninstall $(RELEASE) -n $(NS) || true

smoke:                 ## Verify every infra component end-to-end
	./scripts/smoke-infra.sh
```

- [ ] **Step 2: Write the smoke script**

Create `deploy/scripts/smoke-infra.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
NS=ticketbottle
K="kubectl -n $NS"
echo "== Postgres: databases =="
$K exec statefulset/postgres -- psql -U root -tAc \
  "SELECT count(*) FROM pg_database WHERE datname LIKE 'ticketbottle_%';" | grep -qx 4 \
  && echo "  OK 4 app databases"
echo "== Redis: ping =="
$K exec statefulset/redis -- redis-cli ping | grep -qx PONG && echo "  OK PONG"
echo "== Redpanda: broker =="
$K exec statefulset/redpanda -- rpk cluster info --brokers localhost:9093 | grep -qi redpanda \
  && echo "  OK broker up"
echo "== DynamoDB: table ACTIVE =="
$K run ddb-smoke --rm -i --restart=Never --image=amazon/aws-cli:2.17.0 \
  --env AWS_ACCESS_KEY_ID=local --env AWS_SECRET_ACCESS_KEY=local --env AWS_REGION=us-east-1 -- \
  dynamodb describe-table --endpoint-url http://dynamodb:8000 --table-name ticketbottle-orders \
  --query 'Table.TableStatus' --output text | grep -qx ACTIVE && echo "  OK ACTIVE"
echo "== Temporal: default namespace =="
$K exec deployment/temporal -- tctl --address 127.0.0.1:7233 namespace list | grep -q "Name: default" \
  && echo "  OK default namespace"
echo "ALL INFRA SMOKE CHECKS PASSED"
```
Make it executable: `chmod +x deploy/scripts/smoke-infra.sh`

- [ ] **Step 3: Write the Rung 1 runbook**

Create `deploy/README.md`:
```markdown
# TicketBottle Deployment (Rung 1: local kind)

Portable Helm chart for the TicketBottle stack. Rung 1 runs everything on a local
`kind` cluster for $0. See the spec: `docs/superpowers/specs/2026-07-09-aws-affordable-deployment-ladder-design.md`.

## Prerequisites
docker, kubectl, helm, kind.

## Bring up the infra tier (Phase 0A)
```bash
cd deploy
make cluster-up     # one-time: create the kind cluster
make infra-up       # install Postgres, Redis, Redpanda, DynamoDB-local, Temporal
make smoke          # verify every component
```

## Tear down
```bash
make infra-down     # remove the chart, keep the cluster
make cluster-down   # delete the cluster entirely
```

## What runs (infra tier)
| Component | Service:port | Notes |
|-----------|--------------|-------|
| Postgres | postgres:5432 | one instance, 4 app DBs + Temporal's 2 |
| Redis | redis:6379 | waitroom (DB0) + gateway auth (DB1) |
| Redpanda | redpanda:9093 | Kafka-API broker (replaces Kafka+ZK) |
| DynamoDB-local | dynamodb:8000 | orders table (PK/SK + GSI1 + GSI2) |
| Temporal | temporal:7233 | Postgres visibility, no Elasticsearch |

App services (8) + the two payment lambda-replacements land in Phase 0B.
```

- [ ] **Step 4: Full clean-slate gate — reinstall from scratch and smoke**

Run:
```bash
cd deploy
make infra-down
make infra-up
make smoke
```
Expected final line: `ALL INFRA SMOKE CHECKS PASSED`. Also confirm every pod is Ready:
```bash
kubectl -n ticketbottle get pods
```
Expected: `postgres-0`, `redis-0`, `redpanda-0`, `dynamodb-*`, `temporal-*` all `1/1 Running`, and `dynamodb-init` `Completed`.

- [ ] **Step 5: Commit**

```bash
git add deploy/Makefile deploy/scripts/smoke-infra.sh deploy/README.md
git commit -m "feat(deploy): one-command infra bring-up + smoke gate + runbook (Phase 0A)"
```

---

## Phase 0A completion criteria

- `make cluster-up && make infra-up && make smoke` from a clean state prints `ALL INFRA SMOKE CHECKS PASSED`.
- All infra pods `Running`/`Ready`; `dynamodb-init` `Completed`.
- No file under `services/**` was modified.
- Six commits (one per Task 1–8 group; Tasks 3–7 each commit their template).

**Next:** write Phase 0B (app services + payment lambda-replacements + migrations + end-to-end purchase-flow harness → Gate 1). It reuses this exact chart and cluster; it will add `templates/apps/*` and `values-local.yaml` config blocks, and needs a short investigation pass over each service's Prisma/GORM migration mechanism, the two lambda handlers' runtime deps, and the gateway's signup/event/waitroom/order request bodies.

## Self-review notes (author)

- **Spec coverage:** 0A implements spec §3 (chart+overlay skeleton), §4 (trimmed infra tier: Redpanda / no-ES Temporal / single Postgres / DynamoDB-local), and the "StatefulSets with PVCs" learning goal (§2.2). Cost controls (§8), IAM (§6), app config (§6), and the Gate-1 purchase flow (§7) are explicitly deferred to 0B/Phase 1 and are listed in the plan-series table — not dropped.
- **Placeholder scan:** none — every manifest and command is concrete.
- **Type/name consistency:** service names (`postgres`, `redis`, `redpanda:9093`, `dynamodb:8000`, `temporal:7233`), DB names, table name, and namespace (`ticketbottle`) are used identically across tasks and match the Global Constraints and the source `.env`/compose values.
