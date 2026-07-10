# AWS Deploy — Phase 0B-2: Payment Event-Loop Adapter & Gate-1 Purchase Flow — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the purchase loop on `kind`: deploy a purpose-built **payment-events adapter** (outbox→Redpanda publisher + a webhook trigger) and prove the full flow end-to-end (**Gate 1**): register → event → seed tickets → waitroom → order → simulated payment → **order COMPLETED**.

**Architecture:** Payment completion in this codebase lives in AWS Lambdas coupled to SAM (webhook receipt + outbox publishing). Rather than re-host that SAM-coupled code, 0B-2 adds a small **deploy-side** Node adapter (`deploy/adapters/payment-events/`, raw `pg` + `kafkajs`, ~150 LOC, no app-code reuse) with two entrypoints: (1) **outbox-publisher** — polls the payment `outbox` table and publishes each unpublished row's `payload` to the Kafka topic mapped from its `eventType`, then marks it published (a faithful transactional-outbox relay); (2) **webhook** — `POST /complete/:orderCode` marks the payment `COMPLETED` and writes a `PAYMENT_COMPLETED` outbox row in one transaction (a stand-in for the provider callback). The published message matches order-consumer's contract exactly, so it drives `ConfirmOrder` via Temporal. Ticket-class inventory is seeded directly (the app has no creation path — the gateway inventory module is a stub).

**Tech Stack:** Node 20 (`pg`, `kafkajs`), kind, Helm, kubectl, curl; Redpanda (Kafka API), Temporal, DynamoDB, Postgres.

## Plan series (where this fits)

Plan **3 of 5**. Prereq: **0A + 0B-1 deployed and green** (infra tier + all 8 app pods Ready).

| Plan | Scope | Gate |
|------|-------|------|
| 0A ✅ / 0B-1 (built/planned) | infra + app tier | infra smoke / signup works |
| **0B-2 (this doc)** | payment-events adapter + full purchase flow | **Gate 1 — order reaches COMPLETED** |
| 1 | Terraform + stoppable k3s EC2 (same chart + adapter) | Gate 2 |
| 2 (optional) | ephemeral EKS | Gate 3 |

## Global Constraints

Verified against the codebase — every task implicitly includes these:

- **Prereq:** `make cluster-up && make infra-up && make apps-up && make smoke && make smoke-apps` all green (0A + 0B-1).
- **Namespace** `ticketbottle`; chart `deploy/helm/ticketbottle`; release `tb`; overlay `values-local.yaml`.
- **Kafka contract (order-consumer, verified in `internal/order/delivery/kafka/`):** subscribes to `payment.completed` and `payment.failed`; unmarshals the message value into `{order_code, payment_id, amount_cents, currency, provider, paid_at}` and uses **`order_code`** to start `ConfirmOrder`. Only `order_code` is required; extra fields are ignored.
- **Payment DB (`ticketbottle_payment`, Prisma → camelCase columns, must be double-quoted in SQL):**
  - `payments` (`@@map`): `id`, `"orderCode"` (unique), `"amountCents"`, `currency`, `provider`, `status` (`PaymentStatus`: `PENDING|COMPLETED|CANCELLED|FAILED`), `"completedAt"`, …
  - `outbox` (`@@map`): `id` (uuid), `"aggregateId"`, `"aggregateType"`, `"eventType"`, `payload` (jsonb), `published` (bool), `"publishedAt"`, `"createdAt"`, `"retryCount"`, `"lastError"`.
- **Inventory DB (`ticketbottle_inventory`, GORM → snake_case columns, table name is `ticket_class` via `TableName()`):** `id` (bigserial), `event_id`, `name`, `price_cents`, `currency`, `total`, `reserved` (default 0), `sold` (default 0), `sale_start_at`/`sale_end_at` (nullable), `status` (default `'ACTIVE'`), `created_at`, `updated_at`.
- **Adapter topic map:** `PAYMENT_COMPLETED → payment.completed`, `PAYMENT_FAILED → payment.failed`, `PAYMENT_CANCELLED → payment.cancelled`.
- **Adapter code** lives under `deploy/adapters/payment-events/`; image `ticketbottle/payment-events:local`, `kind load`ed, `imagePullPolicy: IfNotPresent`.
- **No `services/**` edits.**

---

### Task 1: payment-events adapter — code + image

**Files:**
- Create: `deploy/adapters/payment-events/package.json`
- Create: `deploy/adapters/payment-events/publisher.js`
- Create: `deploy/adapters/payment-events/webhook.js`
- Create: `deploy/adapters/payment-events/Dockerfile`

**Interfaces:**
- Produces image `ticketbottle/payment-events:local` with two entrypoints (`node publisher.js`, `node webhook.js`). Both read env `DATABASE_URL` (payment DB) and `KAFKA_BROKERS`.

- [ ] **Step 1: `package.json`**

Create `deploy/adapters/payment-events/package.json`:
```json
{
  "name": "payment-events-adapter",
  "version": "1.0.0",
  "private": true,
  "dependencies": {
    "kafkajs": "^2.2.4",
    "pg": "^8.13.1"
  }
}
```

- [ ] **Step 2: `publisher.js` (outbox → Redpanda relay)**

Create `deploy/adapters/payment-events/publisher.js`:
```js
const { Pool } = require('pg');
const { Kafka } = require('kafkajs');

const TOPIC_MAP = {
  PAYMENT_COMPLETED: 'payment.completed',
  PAYMENT_FAILED: 'payment.failed',
  PAYMENT_CANCELLED: 'payment.cancelled',
};

const pool = new Pool({ connectionString: process.env.DATABASE_URL });
const kafka = new Kafka({ clientId: 'payment-events', brokers: (process.env.KAFKA_BROKERS || 'redpanda:9093').split(',') });
const producer = kafka.producer();

async function tick() {
  const { rows } = await pool.query(
    'SELECT id, "eventType", payload FROM outbox WHERE published = false ORDER BY "createdAt" ASC LIMIT 50',
  );
  for (const row of rows) {
    const topic = TOPIC_MAP[row.eventType];
    if (!topic) { console.warn('unknown eventType', row.eventType); continue; }
    await producer.send({ topic, messages: [{ value: JSON.stringify(row.payload) }] });
    await pool.query('UPDATE outbox SET published = true, "publishedAt" = now() WHERE id = $1', [row.id]);
    console.log('published', { id: row.id, topic, order_code: row.payload.order_code });
  }
}

(async () => {
  await producer.connect();
  console.log('outbox-publisher connected; polling every 2s');
  setInterval(() => tick().catch((e) => console.error('tick error', e.message)), 2000);
})();
```

- [ ] **Step 3: `webhook.js` (simulated provider callback → payment + outbox)**

Create `deploy/adapters/payment-events/webhook.js`:
```js
const http = require('http');
const { Pool } = require('pg');
const { randomUUID } = require('crypto');

const pool = new Pool({ connectionString: process.env.DATABASE_URL });

async function complete(orderCode) {
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const pay = await client.query(
      `UPDATE payments SET status = 'COMPLETED', "completedAt" = now()
       WHERE "orderCode" = $1 RETURNING id, "amountCents", currency, provider`,
      [orderCode],
    );
    if (pay.rowCount === 0) { await client.query('ROLLBACK'); return { code: 404 }; }
    const p = pay.rows[0];
    const payload = {
      order_code: orderCode, payment_id: p.id, amount_cents: p.amountCents,
      currency: p.currency, provider: p.provider, paid_at: new Date().toISOString(),
    };
    await client.query(
      `INSERT INTO outbox (id, "aggregateId", "aggregateType", "eventType", payload, published, "createdAt", "retryCount")
       VALUES ($1, $2, 'Payment', 'PAYMENT_COMPLETED', $3, false, now(), 0)`,
      [randomUUID(), p.id, JSON.stringify(payload)],
    );
    await client.query('COMMIT');
    return { code: 200, payload };
  } catch (e) { await client.query('ROLLBACK'); throw e; }
  finally { client.release(); }
}

http.createServer(async (req, res) => {
  if (req.method === 'GET' && req.url === '/healthz') { res.writeHead(200).end('ok'); return; }
  const m = req.url.match(/^\/complete\/(.+)$/);
  if (req.method === 'POST' && m) {
    try {
      const r = await complete(decodeURIComponent(m[1]));
      res.writeHead(r.code, { 'Content-Type': 'application/json' }).end(JSON.stringify(r.payload || { error: 'payment not found' }));
    } catch (e) { console.error(e); res.writeHead(500).end(JSON.stringify({ error: e.message })); }
    return;
  }
  res.writeHead(404).end();
}).listen(8080, () => console.log('payment-webhook listening on :8080'));
```

- [ ] **Step 4: `Dockerfile`**

Create `deploy/adapters/payment-events/Dockerfile`:
```dockerfile
FROM node:20-alpine
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev --no-audit --no-fund
COPY publisher.js webhook.js ./
# default entrypoint; overridden per workload via the pod command
CMD ["node", "publisher.js"]
```

- [ ] **Step 5: Build and load the image**

Run:
```bash
docker build -t ticketbottle/payment-events:local deploy/adapters/payment-events
kind load docker-image ticketbottle/payment-events:local --name ticketbottle
docker exec ticketbottle-control-plane crictl images | grep payment-events
```
Expected: build succeeds; the image appears in the kind node.

- [ ] **Step 6: Commit**

```bash
git add deploy/adapters/payment-events
git commit -m "feat(deploy): payment-events adapter (outbox publisher + webhook) (Phase 0B-2)"
```

---

### Task 2: Deploy the adapter + isolated verification

**Files:**
- Create: `deploy/helm/ticketbottle/templates/apps/payment-events.yaml`

**Interfaces:**
- Consumes: `payment-config` (0B-1, has `DATABASE_URL` + `KAFKA_BROKERS`), `postgres`, `redpanda`, `payment-service` (writes the payments row).
- Produces: `outbox-publisher` Deployment (no Service); `payment-webhook` Deployment + Service `payment-webhook:8080`.

- [ ] **Step 1: Write the two workloads**

Create `deploy/helm/ticketbottle/templates/apps/payment-events.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: outbox-publisher, namespace: {{ include "tb.namespace" . }}, labels: {{- include "tb.labels" . | nindent 4 }} }
spec:
  replicas: 1
  selector: { matchLabels: { app: outbox-publisher } }
  template:
    metadata: { labels: { app: outbox-publisher } }
    spec:
      containers:
        - name: outbox-publisher
          image: ticketbottle/payment-events:local
          imagePullPolicy: IfNotPresent
          command: ["node", "publisher.js"]
          envFrom: [{ configMapRef: { name: payment-config } }]
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: payment-webhook, namespace: {{ include "tb.namespace" . }}, labels: {{- include "tb.labels" . | nindent 4 }} }
spec:
  replicas: 1
  selector: { matchLabels: { app: payment-webhook } }
  template:
    metadata: { labels: { app: payment-webhook } }
    spec:
      containers:
        - name: payment-webhook
          image: ticketbottle/payment-events:local
          imagePullPolicy: IfNotPresent
          command: ["node", "webhook.js"]
          envFrom: [{ configMapRef: { name: payment-config } }]
          ports: [{ containerPort: 8080 }]
          readinessProbe: { httpGet: { path: /healthz, port: 8080 }, initialDelaySeconds: 3, periodSeconds: 5 }
---
apiVersion: v1
kind: Service
metadata: { name: payment-webhook, namespace: {{ include "tb.namespace" . }}, labels: {{- include "tb.labels" . | nindent 4 }} }
spec:
  selector: { app: payment-webhook }
  ports: [{ port: 8080, targetPort: 8080 }]
```

> Note: `payment-config` provides `DATABASE_URL=postgresql://root:root@postgres:5432/ticketbottle_payment` and `KAFKA_BROKERS=redpanda:9093` (both set in 0B-1 Task 2).

- [ ] **Step 2: Deploy and confirm both Ready**

Run:
```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle -f deploy/helm/ticketbottle/values-local.yaml --timeout 5m >/dev/null
kubectl -n ticketbottle rollout status deployment/outbox-publisher --timeout=90s
kubectl -n ticketbottle rollout status deployment/payment-webhook --timeout=90s
kubectl -n ticketbottle logs deployment/outbox-publisher --tail=3
```
Expected: both roll out; publisher logs `outbox-publisher connected; polling every 2s`.

- [ ] **Step 3: Isolated test — outbox row publishes to Redpanda**

Insert a fake unpublished outbox row, then confirm it lands on `payment.completed` and gets marked published:
```bash
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_payment -c \
  "INSERT INTO outbox (id, \"aggregateId\", \"aggregateType\", \"eventType\", payload, published, \"createdAt\", \"retryCount\") \
   VALUES (gen_random_uuid(), 'p-smoke', 'Payment', 'PAYMENT_COMPLETED', '{\"order_code\":\"SMOKE-1\"}', false, now(), 0);"
sleep 3
kubectl -n ticketbottle exec statefulset/redpanda -- rpk topic consume payment.completed --num 1 --brokers localhost:9093
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_payment -tAc \
  "SELECT published FROM outbox WHERE \"aggregateId\"='p-smoke';"
```
Expected: the consumed message `"value"` contains `"order_code":"SMOKE-1"`; the `published` column is now `t`.

- [ ] **Step 4: Isolated test — webhook updates payment + writes outbox**

Insert a fake PENDING payment, hit the webhook, confirm it flips to COMPLETED and produced an outbox row:
```bash
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_payment -c \
  "INSERT INTO payments (id, \"orderCode\", \"amountCents\", currency, provider, \"providerTransactionId\", \"idempotencyKey\", \"redirectUrl\", \"paymentUrl\", status, \"createdAt\", \"updatedAt\") \
   VALUES (gen_random_uuid(), 'WH-1', 10000, 'VND', 'zalopay', 'tx-wh-1', 'idem-wh-1', 'http://x', 'http://x', 'PENDING', now(), now());"
kubectl -n ticketbottle run wh-curl --rm -i --restart=Never --image=curlimages/curl:8.10.1 -- \
  -s -X POST http://payment-webhook:8080/complete/WH-1
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_payment -tAc \
  "SELECT status FROM payments WHERE \"orderCode\"='WH-1';"
```
Expected: the curl returns a JSON payload with `"order_code":"WH-1"`; the payment `status` is now `COMPLETED`. (The publisher will also relay the new outbox row to `payment.completed` within ~2s.)

- [ ] **Step 5: Commit**

```bash
git add deploy/helm/ticketbottle/templates/apps/payment-events.yaml
git commit -m "feat(deploy): deploy payment-events adapter + isolated verification (Phase 0B-2)"
```

---

### Task 3: Ticket-class seed helper

**Files:**
- Create: `deploy/scripts/seed-ticketclass.sh`

**Interfaces:**
- Produces a row in `ticket_class` (inventory DB) for a given `event_id`; prints the new numeric `id` (used as `ticketClassId` in the order).

- [ ] **Step 1: Write the seed script**

Create `deploy/scripts/seed-ticketclass.sh`:
```bash
#!/usr/bin/env bash
# Usage: seed-ticketclass.sh <event_id> [total] [price_cents]
# Prints the new ticket_class id. The app has no create path (gateway inventory module is a stub),
# so Gate-1 seeds inventory directly.
set -euo pipefail
EVENT_ID="$1"; TOTAL="${2:-100}"; PRICE="${3:-10000}"
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
  "INSERT INTO ticket_class (event_id, name, price_cents, currency, total, reserved, sold, status, created_at, updated_at)
   VALUES ('${EVENT_ID}', 'GA', ${PRICE}, 'VND', ${TOTAL}, 0, 0, 'ACTIVE', now(), now())
   RETURNING id;" | tr -d '[:space:]'
```
Make executable: `chmod +x deploy/scripts/seed-ticketclass.sh`

- [ ] **Step 2: Smoke the seed against a dummy event id**

Run:
```bash
cd deploy && ID=$(./scripts/seed-ticketclass.sh evt-smoke 50 5000); echo "seeded ticket_class id=$ID"
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
  "SELECT id,event_id,total,available:=total-reserved-sold FROM ticket_class WHERE event_id='evt-smoke';" 2>/dev/null \
  || kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
     "SELECT id,event_id,total FROM ticket_class WHERE event_id='evt-smoke';"
```
Expected: prints a numeric `id`; the row exists with `total=50`.

- [ ] **Step 3: Commit**

```bash
git add deploy/scripts/seed-ticketclass.sh
git commit -m "feat(deploy): direct ticket-class seed helper (Phase 0B-2)"
```

---

### Task 4: Gate-1 — full purchase-flow harness

**Files:**
- Create: `deploy/scripts/gate1-purchase-flow.sh`
- Modify: `deploy/Makefile` (add `gate1`)

**Interfaces:**
- Consumes: everything above (gateway at `localhost:3000`, adapter, seed helper).
- Produces: `make gate1` — the Phase 0B-2 acceptance gate.

**Flow (each hop verified; dynamic values captured from responses):**
register → create event → seed ticket class → publish/activate event → join waitroom → capture checkout token on admission → create order → trigger payment webhook → poll order until COMPLETED.

- [ ] **Step 1: Write the harness (structured with capture points)**

Create `deploy/scripts/gate1-purchase-flow.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
GW=http://localhost:3000/api
J() { python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }   # tiny JSON extractor

echo "== 1. register =="
EMAIL="gate1+$(date +%s)@example.com"
TOK=$(curl -s -X POST $GW/auth/signup -H 'Content-Type: application/json' \
  -d "{\"firstName\":\"Gate\",\"lastName\":\"One\",\"email\":\"$EMAIL\",\"password\":\"Password123!\"}" | J "['accessToken']")
AUTH="Authorization: Bearer $TOK"; echo "  token acquired"

echo "== 2. create event =="
EVENT_ID=$(curl -s -X POST $GW/events -H "$AUTH" -H 'Content-Type: application/json' -d '{
  "name":"Gate1 Show","description":"e2e","startDate":"2027-01-01T00:00:00Z","endDate":"2027-01-02T00:00:00Z",
  "thumbnailUrl":"https://example.com/t.png","venue":"Test Arena","street":"1 St","city":"HCMC","country":"VN"
}' | J "['id']"); echo "  eventId=$EVENT_ID"

echo "== 3. seed ticket class =="
TCID=$(./scripts/seed-ticketclass.sh "$EVENT_ID" 100 10000); echo "  ticketClassId=$TCID"

echo "== 4. publish/activate event =="
# The order/waitroom path may require the event to be PUBLISHED. CAPTURE-AND-ADAPT:
# read services/api-gateway/src/modules/events/events.controller.ts for the status-change route.
# If none exists, set it directly:
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_event -c \
  "UPDATE events SET status='PUBLISHED' WHERE id='$EVENT_ID';" 2>/dev/null || echo "  (adjust: verify event status column/value)"

echo "== 5. join waitroom =="
JOIN=$(curl -s -X POST $GW/waitroom/join -H "$AUTH" -H 'Content-Type: application/json' -d "{\"eventId\":\"$EVENT_ID\"}")
echo "  join response: $JOIN"
SESSION=$(echo "$JOIN" | J "['sessionId']" 2>/dev/null || echo "$JOIN" | J "['session']['id']")

echo "== 6. capture checkout token on admission =="
# Waitroom admits from the queue on its background tick (max 100 concurrent → first user is admitted quickly).
# CAPTURE-AND-ADAPT: the checkout token is delivered via the admission status. Poll the position endpoint
# (or the join response if it already carries a token). Field name confirmed at runtime from step 5's output.
CHECKOUT=""
for i in $(seq 1 15); do
  ST=$(curl -s -H "$AUTH" "$GW/waitroom/position/$SESSION" || true)
  CHECKOUT=$(echo "$ST" | J "['checkoutToken']" 2>/dev/null || true)
  [ -n "$CHECKOUT" ] && break; sleep 1
done
echo "  checkoutToken=${CHECKOUT:0:12}..."

echo "== 7. create order =="
ORD=$(curl -s -X POST $GW/orders -H "$AUTH" -H 'Content-Type: application/json' -d "{
  \"eventId\":\"$EVENT_ID\",\"userFullname\":\"Gate One\",\"userEmail\":\"$EMAIL\",\"userPhone\":\"0900000000\",
  \"paymentMethod\":\"zalopay\",\"items\":[{\"ticketClassId\":\"$TCID\",\"quantity\":1}],\"currency\":\"VND\",
  \"checkoutToken\":\"$CHECKOUT\",\"redirectUrl\":\"http://localhost/done\"}")
echo "  order response: $ORD"
ORDER_CODE=$(echo "$ORD" | J "['orderCode']" 2>/dev/null || echo "$ORD" | J "['code']")
echo "  orderCode=$ORDER_CODE"

echo "== 8. trigger payment completion (via the webhook adapter) =="
kubectl -n ticketbottle run g1-curl --rm -i --restart=Never --image=curlimages/curl:8.10.1 -- \
  -s -X POST "http://payment-webhook:8080/complete/$ORDER_CODE" >/dev/null
echo "  webhook fired"

echo "== 9. poll order until COMPLETED (ConfirmOrder via Kafka + Temporal) =="
for i in $(seq 1 20); do
  O=$(curl -s -H "$AUTH" "$GW/orders/code/$ORDER_CODE")
  STATUS=$(echo "$O" | J "['status']" 2>/dev/null || echo "?")
  echo "  [$i] status=$STATUS"
  [ "$STATUS" = "COMPLETED" ] && { echo "GATE 1 PASSED: order $ORDER_CODE COMPLETED"; exit 0; }
  sleep 2
done
echo "GATE 1 FAILED: order did not reach COMPLETED"; exit 1
```
Make executable: `chmod +x deploy/scripts/gate1-purchase-flow.sh`

> **Honest note on Steps 4–7 (the async hops):** these three field names — the event status-change route, the waitroom **sessionId** field, and the **checkoutToken** field on admission — are the only values not pinnable from a DTO alone (waitroom admission is an async background process and its response shape is defined in `services/api-gateway/src/modules/waitroom`). The script prints each raw response and the plan marks exactly where to adapt. Everything else (signup, event create, order create, webhook, order poll) is pinned from verified DTOs/schemas.

- [ ] **Step 2: Add the Makefile target**

Append to `deploy/Makefile`:
```makefile
.PHONY: gate1
gate1:                 ## Run the full purchase-flow acceptance test
	./scripts/gate1-purchase-flow.sh
```

- [ ] **Step 3: Run Gate 1**

Run:
```bash
cd deploy && make gate1
```
Expected final line: `GATE 1 PASSED: order <CODE> COMPLETED`.
If it stalls at a hop, the script's per-step output shows the failing response; fix the captured field name (Steps 4–7 note) or the failing service before re-running. Watch the saga with:
```bash
kubectl -n ticketbottle logs deployment/order-service --tail=30
kubectl -n ticketbottle logs deployment/order-consumer --tail=30
```

- [ ] **Step 4: Confirm inventory reflects the sale**

Run:
```bash
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_inventory -tAc \
  "SELECT total, reserved, sold FROM ticket_class WHERE id=<TCID from run>;"
```
Expected: `sold = 1`, `reserved = 0` (Confirm converted the hold into a sale).

- [ ] **Step 5: Commit**

```bash
git add deploy/scripts/gate1-purchase-flow.sh deploy/Makefile
git commit -m "feat(deploy): Gate-1 full purchase-flow harness (Phase 0B-2)"
```

---

## Phase 0B-2 completion criteria (Gate 1)

- The payment-events adapter is deployed; both isolated tests (Task 2 Steps 3–4) pass.
- `make gate1` prints `GATE 1 PASSED: order <CODE> COMPLETED`.
- `ticket_class.sold` incremented for the purchased class.
- **Phase 0 is complete:** the full distributed purchase flow (virtual queue → atomic inventory → Temporal saga → payment → Kafka → Temporal confirm) runs on local Kubernetes for $0. Ready for **Phase 1 (k3s EC2)** — the same chart + adapter, deployed to a stoppable cloud box.

## Self-review notes (author)

- **Spec coverage:** implements spec §7's Gate 1 (full purchase flow green on kind, the free gate before any AWS spend) via the user-chosen adapter approach (§6's payment event-driven work, re-homed as deploy-side workloads instead of SAM lambdas — decision recorded 2026-07-10). Non-goal preserved: no `services/**` change.
- **Placeholder scan:** the adapter, seed, and all pinned request bodies are concrete. Three async-flow field names (event status route, waitroom sessionId/checkoutToken) are explicitly marked capture-and-adapt with the exact file to consult and each raw response printed — the irreducible runtime surface of an async e2e harness, not vague requirements.
- **Contract consistency:** publisher topic map + payload (`order_code`) exactly match order-consumer's `PaymentCompletedEvent` json tags; webhook writes the `outbox`/`payments` columns with Prisma's camelCase quoting; the seed uses GORM's `ticket_class` table + snake_case columns; `PaymentStatus.COMPLETED` and `CreateOrderItemDto{ticketClassId,quantity}` are used as defined.
