# What bounds purchase throughput as the box grows

**Status: COMPLETE 2026-10-11.** The first wall is the `payment.completed` consumer,
at about 9.5 purchases a second on any box; past it, about 24, in Temporal and its
Postgres. Decision: → [0029](../decisions/0029-the-next-testbed-is-one-bigger-box.md), accepted.

**Goal:** Name the first part of the design that stops purchase throughput from
growing with CPU, or show that nothing does up to 16 vCPUs. Measured on one box,
resized in place, in one session.

**Question it serves:** the root `CLAUDE.md` Open row *What fails first as
concurrent checkouts rise into the thousands?* The 2-vCPU box saturates at ten
buyers, so any higher load there measures the box:
`docs/plans/2026-09-27-checkout-latency-decomposition.md#what-this-says`.

**Spec:** none. Derived from that plan's *The architect's calls this raises* and
from [0029](../decisions/0029-the-next-testbed-is-one-bigger-box.md).

**What "thousands" means here.** The waitroom exists so that thousands of buyers
never check out at once. What it leaves open is how far throughput grows as the
machine grows, and what stops it. Thousands of checkouts inside the 2s SLO is
about 500 purchases a second. At about 0.4 core-seconds each, that is about 200
cores, which one instance does not reach in this session. This plan finds the
first wall at or below 16 vCPUs, or shows there is none.

## How it works

One box, resized in place:

```
m5.large    2 vCPU   baseline: the same box shape as before, without the sandbox
m5.4xlarge 16 vCPU   where the wall, if any, shows
m5.2xlarge  8 vCPU   only if 16 is sub-linear: where the curve bends
t3.large    2 vCPU   back to the everyday box
```

At each size, the number of buyers steps up with the door open until throughput
stops rising. The run at the plateau names its limit from signals the run
records: node CPU, disk operations, what Postgres sessions wait on, how long
Temporal's first workflow task waits, and the CPU of every pod.

**Why m5.** It uses the same CPU generation as the t3.large (Intel Xeon Platinum
8000 series). So the 2-vCPU baseline should reproduce the t3.large's ceiling of
3.15 purchases a second (`docs/plans/2026-09-29-admission-sizing.md#results`),
and the family reaches 16 vCPUs, where t3 stops at 8. An m7i is faster per core,
which would break the comparison with every earlier number.

**Cost.** About $0.10, $0.38 and $0.77 an hour for the three m5 sizes. A session
of about three hours costs under $2.

## Found while writing this plan

- **Every workload has a CPU limit.** In `values.yaml`, Temporal and Postgres are
  limited to 1 core and the Go services to 0.5. Temporal costs 0.12 core-s a
  purchase (`docs/plans/2026-09-27-saga-short-steps-local.md#results`), so it
  would hit its limit near 8 purchases a second, and a bigger box would find the
  chart's limits before the design's. The capacity overlay removes every limit.
- **Postgres runs with its defaults, including 100 connections.** Prisma's
  documented default pool is twice the node's physical cores plus one: 3 a client
  on the t3.large, 17 on an m5.4xlarge. Three Prisma services, Temporal's pools,
  inventory's 25 and the Node workers' pools could pass 100 on the big box. The
  overlay sets 500, and every run prints its peak.
- **The payment call goes to ZaloPay's sandbox, at a URL fixed in the code**
  (`zalopay.gateway.ts`, `CREATE_ZALOPAY_PAYMENT_LINK_URL`). It stalled for 25s
  on 2026-09-29 and for 43s on 2026-09-28. At a hundred calls a second, a run
  would measure ZaloPay. Tasks 1 to 3 route a capacity run to the simulated
  provider, `payment-webhook`, which already plays ZaloPay's callback.
- **k6 used 0.02 cores at 10 to 40 buyers**
  (`docs/plans/2026-09-27-checkout-latency-decomposition.md#what-this-says`,
  finding 3). Moving it to a second instance would cost that instance and expose
  two Services. It stays on the box, and a run in which k6 is throttled is void.
  This revises the recommendation the architect approved; 0029 records it as not
  yet put to them.
- **Temporal's history shards are auto-setup's default.** The chart does not set
  `NUM_HISTORY_SHARDS`, and the default is 4. The count is fixed when the cluster
  is created. Task 6 reads the live value.
- **The root volume is gp3 at its baseline 3,000 IOPS** (`modules/ec2-k3s/main.tf`
  sets no `iops`). Postgres, with the app databases and Temporal's, and Redpanda
  all write to it.
- **Terraform resizes the box in place.** AWS provider 5.100.0 reads
  `credit_specification` only for burstable types and leaves it alone on a type
  change, so `cpu_credits` needs no guard.
- **The AWS vCPU quota is unread.** The CLI session had expired. Task 5 reads it
  before anything else, because raising it can take a day.

## Global constraints

- Commit messages describe the platform, never study progress, and end with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Pushing `dev` needs the architect's go-ahead.
- The k3s box and EKS never run at once: `aws eks list-clusters --region us-east-1`
  must print nothing before the box starts.
- Comment budget: 3 lines inline, 5 on a symbol, 8 for a module (root `CLAUDE.md`).
- The ZaloPay sandbox stays the default on every target. Only
  `deploy/loadtest/capacity-values.yaml` points payment at the simulated provider,
  so the gates still exercise the real integration.
- The capacity overlay is never a deployment target. The session ends with a plain
  deploy on the t3.large.
- One build for the whole session: pass its `sha-` tag as `REF`, never the moving
  `:dev`.

## Review focus

1. **A run with the sandbox still in the path measures ZaloPay.** The sweep refuses
   to start (Task 4), and Task 6 reads a payment's URL before the first sweep.
2. **A container limit binds before the design does.** A render check (Task 3) and
   the sweep's preflight (Task 4) both refuse any CPU limit.
3. **Postgres runs out of connections.** That is a failure of configuration, not a
   limit of the design. The overlay sets 500 (Task 3), and every run prints its peak
   against the limit (Task 4's `pg_summary` tests).
4. **k6 or a restart decides a run.** A throttled load generator measures itself,
   and a restarted container resets its counters. Either one makes a run void
   (Task 4's `void` tests).
5. **The session leaves the box large or on the overlay.** Task 6's last step reads
   the instance type, the payment URL, a limit and `max_connections` back.

## Setup, every session on the box

```bash
aws login                                                 # the CLI session expires
aws eks list-clusters --region us-east-1                  # must print nothing
make -C deploy my-ip                                      # if it differs from the tfvars:
make -C deploy k3s-allow-ip                               # plan, check only the SG changes, apply
```

The local network leaves from two rotating addresses. Add any second one as a /32
SSH rule (`aws ec2 authorize-security-group-ingress`); the next `k3s-allow-ip`
removes it.

```bash
ssh -N -o ServerAliveInterval=30 -L 6443:127.0.0.1:6443 -L 3000:127.0.0.1:30000 ec2-user@$(make -s -C deploy k3s-ip) &
make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090 &
export GW=http://localhost:3000/api
```

After a cold boot, `order-service` and `order-consumer` crash-loop until Temporal
is up. A resize is a cold boot with a new public IP: kill the tunnel and the
port-forward, then open both again.

---

### Task 1: Payment's create URL comes from config

**Files:**
- Modify: `services/payment-svc/src/shared/services/config.service.ts` (`zalopayConfig`)
- Modify: `services/payment-svc/src/modules/payment/gateways/zalopay/zalopay.gateway.ts`
- Create: `services/payment-svc/src/modules/payment/gateways/zalopay/zalopay.gateway.spec.ts`

**Interfaces:**
- Produces: the env var `ZALOPAY_CREATE_URL`, read by `AppConfigService.zalopayConfig.createUrl`;
  unset or empty means the ZaloPay sandbox. Task 3 sets it.

- [ ] **Step 1: Write the failing test** at `zalopay.gateway.spec.ts`:

```ts
import { HttpService } from '@nestjs/axios';
import { AppConfigService } from '@/shared/services/config.service';
import { CreatePaymentLinkInput } from '../gateway.interface';
import { ZalopayGateWay } from './zalopay.gateway';

const SANDBOX = 'https://sb-openapi.zalopay.vn/v2/create';
const INPUT: CreatePaymentLinkInput = {
  amount: 100000,
  orderCode: 'ORD-1',
  currency: 'VND',
  idempotencyKey: 'key-1',
  redirectUrl: 'https://example.com/done',
  timeoutSeconds: 300,
};

// The real config service reads the environment, so the default is tested too.
function build() {
  const post = jest.fn().mockResolvedValue({ data: { return_code: 1, order_url: 'https://pay/1' } });
  const http = { axiosRef: { post } } as unknown as HttpService;
  return { gateway: new ZalopayGateWay(http, new AppConfigService()), post };
}

describe('ZalopayGateWay.createPaymentLink', () => {
  const saved = process.env.ZALOPAY_CREATE_URL;
  afterEach(() => {
    if (saved === undefined) delete process.env.ZALOPAY_CREATE_URL;
    else process.env.ZALOPAY_CREATE_URL = saved;
  });

  it('posts to the ZaloPay sandbox when no create URL is configured', async () => {
    delete process.env.ZALOPAY_CREATE_URL;
    const { gateway, post } = build();

    await gateway.createPaymentLink(INPUT);

    expect(post).toHaveBeenCalledWith(SANDBOX, expect.anything(), expect.anything());
  });

  it('posts to ZALOPAY_CREATE_URL when it is set', async () => {
    process.env.ZALOPAY_CREATE_URL = 'http://payment-webhook:8080/v2/create';
    const { gateway, post } = build();

    await gateway.createPaymentLink(INPUT);

    expect(post).toHaveBeenCalledWith(
      'http://payment-webhook:8080/v2/create',
      expect.objectContaining({ app_trans_id: expect.stringMatching(/_ORD-1$/) }),
      expect.anything(),
    );
  });
});
```

- [ ] **Step 2: Run it.** From `services/payment-svc`:
  `npx jest src/modules/payment/gateways/zalopay`.
  Expected: `1 failed, 1 passed`. The failure says `Expected:
  "http://payment-webhook:8080/v2/create"` and `Received:
  "https://sb-openapi.zalopay.vn/v2/create"`.

- [ ] **Step 3: Take the URL from config.** In `config.service.ts`, `zalopayConfig`
  becomes:

```ts
  get zalopayConfig() {
    return {
      appID: this.getNumber('ZALOPAY_APP_ID'),
      key1: this.get('ZALOPAY_KEY1'),
      key2: this.get('ZALOPAY_KEY2'),
      // A capacity run points this at the simulated provider (decision 0029).
      createUrl: this.get('ZALOPAY_CREATE_URL') || 'https://sb-openapi.zalopay.vn/v2/create',
    };
  }
```

  In `zalopay.gateway.ts`, three edits:
  - Replace the field
    `private readonly CREATE_ZALOPAY_PAYMENT_LINK_URL = 'https://sb-openapi.zalopay.vn/v2/create';`
    with `private readonly createUrl: string;`.
  - In the constructor, after `this.key2 = this.configService.zalopayConfig.key2;`, add
    `this.createUrl = this.configService.zalopayConfig.createUrl;`.
  - In `createPaymentLink`, the post's first argument `this.CREATE_ZALOPAY_PAYMENT_LINK_URL,`
    becomes `this.createUrl,`.

- [ ] **Step 4: Run every suite.** `npx jest`, then `npx tsc --noEmit -p tsconfig.build.json`.
  Expected: `Tests: 1 skipped, 12 passed, 13 total`, and no output from `tsc`.

- [ ] **Step 5: Prove the default test can go red.** It passed on arrival. Change
  the default in `config.service.ts` to `.../v2/creat`, run Step 2's command, and
  expect `posts to the ZaloPay sandbox when no create URL is configured` to fail.
  Restore the default.

- [ ] **Step 6: Commit**

```bash
git add services/payment-svc/src/shared/services/config.service.ts services/payment-svc/src/modules/payment/gateways/zalopay
git commit -m "feat(payment): take ZaloPay's create URL from config"
```

---

### Task 2: The simulated provider answers a create call

**Files:**
- Modify: `deploy/adapters/payment-events/webhook.js`

**Interfaces:**
- Produces: `POST /v2/create` on `payment-webhook:8080`. It answers ZaloPay's success
  shape, `{"return_code": 1, "order_url": "http://payment-webhook:8080/pay/<app_trans_id>"}`,
  after `FAKE_CREATE_DELAY_MS` (default 330, ZaloPay's measured fast mode). A body
  without `app_trans_id`, or one that is not JSON, gets 400.

- [ ] **Step 1: Run the check that should fail.** Port 8080 must be free
  (`lsof -iTCP:8080 -sTCP:LISTEN` prints nothing). If the directory has no
  `node_modules`, run `npm install` there first.

```bash
cd deploy/adapters/payment-events
FAKE_CREATE_DELAY_MS=200 SERVER_METRICS_PORT=21120 node webhook.js & PID=$!; sleep 1
curl -s -w ' %{http_code} %{time_total}s\n' -X POST localhost:8080/v2/create -d '{"app_trans_id":"261009_TB-X"}'
kill $PID
```

  Expected: ` 404`. Stop the server by its PID, never with `pkill`: the pattern can
  match someone else's process.

- [ ] **Step 2: Add the route.** In `webhook.js`, after `const EVENT_TYPE = 'PAYMENT_COMPLETED';`:

```js
// Plays ZaloPay's create endpoint, so a load run measures this system and not the
// sandbox (decision 0029). The delay is the measured fast real call.
const CREATE_DELAY_MS = Number(process.env.FAKE_CREATE_DELAY_MS ?? 330);

async function createLink(req) {
  let body = '';
  for await (const chunk of req) body += chunk;
  const id = JSON.parse(body).app_trans_id;
  if (typeof id !== 'string' || !id) {
    return { code: 400, body: { return_code: -1, return_message: 'app_trans_id required' } };
  }
  await new Promise((r) => setTimeout(r, CREATE_DELAY_MS));
  const url = `http://payment-webhook:8080/pay/${encodeURIComponent(id)}`;
  return { code: 200, body: { return_code: 1, return_message: 'success', order_url: url } };
}
```

  And as the first route in the 8080 handler, just before
  `const m = req.url.match(/^\/complete\/(.+)$/);`:

```js
  if (req.method === 'POST' && req.url === '/v2/create') {
    const bad = { code: 400, body: { return_code: -1, return_message: 'bad request' } };
    const r = await createLink(req).catch(() => bad);
    res.writeHead(r.code, { 'Content-Type': 'application/json' }).end(JSON.stringify(r.body));
    return;
  }
```

- [ ] **Step 3: Run it again, with the two refusals.**

```bash
FAKE_CREATE_DELAY_MS=200 SERVER_METRICS_PORT=21120 node webhook.js & PID=$!; sleep 1
curl -s -w ' %{http_code} %{time_total}s\n' -X POST localhost:8080/v2/create -d '{"app_trans_id":"261009_TB-X"}'
curl -s -w ' %{http_code}\n' -X POST localhost:8080/v2/create -d 'not json'
curl -s -w ' %{http_code}\n' -X POST localhost:8080/v2/create -d '{}'
curl -s -w ' %{http_code}\n' localhost:8080/healthz
kill $PID; cd -
```

  Expected:

```
{"return_code":1,"return_message":"success","order_url":"http://payment-webhook:8080/pay/261009_TB-X"} 200 0.20...s
{"return_code":-1,"return_message":"bad request"} 400
{"return_code":-1,"return_message":"app_trans_id required"} 400
ok 200
```

- [ ] **Step 4: Commit**

```bash
git add deploy/adapters/payment-events/webhook.js
git commit -m "feat(payment-events): answer ZaloPay's create call in the simulated provider"
```

---

### Task 3: A capacity overlay

**Files:**
- Create: `deploy/loadtest/capacity-values.yaml`
- Modify: `deploy/helm/ticketbottle/values.yaml` (`postgres.args`, a new `payment:` block)
- Modify: `deploy/helm/ticketbottle/templates/apps/config.yaml` (`payment-config`)
- Modify: `deploy/helm/ticketbottle/templates/infra/postgres.yaml`
- Test: `deploy/helm/ticketbottle/tests/assert-render.sh`

**Interfaces:**
- Consumes: `ZALOPAY_CREATE_URL` (Task 1), `/v2/create` (Task 2).
- Produces: the chart values `payment.zalopayCreateUrl` (default `""`) and `postgres.args`
  (default `[]`), and the overlay file, which Task 6 deploys with
  `HELM_EXTRA="-f loadtest/capacity-values.yaml"`. `make -C deploy` runs from
  `deploy/`, so the path is relative to it.

- [ ] **Step 1: Write the overlay** at `deploy/loadtest/capacity-values.yaml`:

```yaml
# Layered on values-k3s.yaml for a capacity session only, never deployed as a
# target (decision 0029):
#   make -C deploy k3s-deploy REF=<sha-tag> HELM_EXTRA="-f loadtest/capacity-values.yaml"
# The box runs nothing else, so no CPU or memory limit should bind before the
# design does; requests still schedule.
resources:
  order-service:     { limits: null }
  order-consumer:    { limits: null }
  inventory-service: { limits: null }
  waitroom-service:  { limits: null }
  app-gateway:       { limits: null }
  user-service:      { limits: null }
  event-service:     { limits: null }
  payment-service:   { limits: null }
  payment-webhook:   { limits: null }
  outbox-relay:      { limits: null }
  postgres:          { limits: null }
  redpanda:          { limits: null }
  temporal:          { limits: null }
  temporal-ui:       { limits: null }
  redis:             { limits: null }

postgres:
  # Prisma sizes its pool from the node's physical cores, so a bigger box opens
  # more connections than the default 100 allows.
  args: ["postgres", "-c", "max_connections=500"]

payment:
  # The simulated provider: a capacity run measures this system, not the sandbox.
  zalopayCreateUrl: http://payment-webhook:8080/v2/create

waitroom:
  # Open, so the system and not the door limits the run.
  releaseRate: 1000
```

- [ ] **Step 2: Write the failing check.** In `tests/assert-render.sh`, just before
  the final `echo "all render assertions passed"`:

```bash
# A capacity session (decision 0029) lifts every limit, points payment at the
# simulated provider, and lets Postgres take the pools a large node opens.
cap=$(helm template tb "$CHART" -f "$CHART/values-k3s.yaml" -f "$SECRETS" -f "$CHART/../../loadtest/capacity-values.yaml")
grep -q 'ZALOPAY_CREATE_URL: "http://payment-webhook:8080/v2/create"' <<<"$cap" \
  || fail "the capacity overlay does not point payment at the simulated provider"
grep -q 'max_connections=500' <<<"$cap" || fail "the capacity overlay does not raise Postgres's max_connections"
limited=$(awk 'BEGIN { RS = "\n---\n" } /kind: (Deployment|StatefulSet)/ && /limits:/ {
  match($0, /\n  name: [a-z0-9-]+/); print substr($0, RSTART + 9, RLENGTH - 9) }' <<<"$cap" | tr '\n' ' ')
[ -z "$limited" ] || fail "under the capacity overlay these keep a limit: $limited"
echo "OK  the capacity overlay lifts every limit, uses the simulated provider, raises max_connections"
```

- [ ] **Step 3: Run it.** `deploy/helm/ticketbottle/tests/assert-render.sh`.
  Expected: `FAIL: the capacity overlay does not point payment at the simulated provider`.

- [ ] **Step 4: Teach the chart the two values.** In `values.yaml`, under
  `postgres.databases`, after `- ticketbottle_inventory`:

```yaml
  # Extra server arguments, e.g. ["postgres", "-c", "max_connections=500"]. The
  # first replaces the image's CMD, so it must be "postgres".
  args: []
```

  And a new block just before `waitroom:`:

```yaml
payment:
  # "" -> the ZaloPay sandbox. A capacity session points it at the simulated
  # provider (deploy/loadtest/capacity-values.yaml, decision 0029).
  zalopayCreateUrl: ""

```

  In `templates/apps/config.yaml`, in `payment-config`, after `ZALOPAY_APP_ID: "2554"`:

```yaml
  {{- with .Values.payment.zalopayCreateUrl }}
  ZALOPAY_CREATE_URL: {{ . | quote }}
  {{- end }}
```

  In `templates/infra/postgres.yaml`, after the `tb.resources` include line:

```yaml
          {{- with .Values.postgres.args }}
          args: {{- toYaml . | nindent 12 }}
          {{- end }}
```

- [ ] **Step 5: Run it.** Same command. Expected: `all render assertions passed`.
  The k3s render is byte-identical to its golden file, because both new values
  default to empty, so there is nothing to regenerate.

- [ ] **Step 6: Prove the limit check can go red.** Delete the `redis:` line from
  the overlay and run again. Expected:
  `FAIL: under the capacity overlay these keep a limit: redis`. Restore the line.

- [ ] **Step 7: Commit**

```bash
git add deploy/loadtest/capacity-values.yaml deploy/helm/ticketbottle/values.yaml deploy/helm/ticketbottle/templates deploy/helm/ticketbottle/tests/assert-render.sh
git commit -m "feat(chart): a capacity overlay that lifts every limit the design does not set"
```

---

### Task 4: Run and read a capacity sweep

**Files:**
- Create: `deploy/loadtest/capacity.py`, `deploy/loadtest/capacity_test.py`
- Create: `deploy/loadtest/pg-sample.sql`
- Create: `deploy/scripts/capacity-sweep.sh`
- Modify: `deploy/loadtest/job.yaml` (k6's limits), `deploy/scripts/gate4a-load.sh`

**Interfaces:**
- Consumes: the overlay (Task 3); `saga_latency.load_client`, `load_histories` and
  `pct`; `gate4a-load.sh`; `saga-histories.sh`.
- Produces: `capacity.py RUN_DIR`, which prints two lines a run. A run directory holds
  `meta.json`, `k6.log`, `pg.txt`, `histories.txt`, and the Prometheus responses
  `pods-{from,to}.json`, `node-{from,to}.json`, `disk-{from,to}.json`,
  `k6-throttled.json` and `k6-periods.json`. Also the env vars `K6_CPU` (default `1`)
  and `K6_MEMORY` (default `512Mi`) on `gate4a-load.sh`.

- [ ] **Step 1: Write the failing tests** at `deploy/loadtest/capacity_test.py`:

```python
"""The capacity run summary. Run: python3 deploy/loadtest/capacity_test.py"""
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import capacity  # noqa: E402

# Nothing binds: every signal well under its threshold.
CALM = {"vcpu": 16, "busy_cores": 4.0, "iops": 500.0, "busiest_node_js": ("app-gateway-a", 0.3),
        "pg_shares": {"CPU": 1.0}, "lock_db": "-", "first_task_p50": 0.05, "unexpected_share": 0.0}


def signals(**changes):
    s = dict(CALM)
    s.update(changes)
    return s


class PgSummary(unittest.TestCase):
    LINES = [
        "S|99|50|500",                                  # before the run
        "W|99|Lock|transactionid|ticketbottle_inventory|9",
        "S|100|120|500",
        "W|100|CPU|-|temporal|2",
        "W|100|IO|WALSync|temporal|1",
        "S|102|80|500",
        "W|102|LWLock|WALWrite|temporal|1",
        "W|102|Lock|transactionid|ticketbottle_inventory|4",
        "psql: error: connection to server failed",      # a sample that failed
        "S|200|900|500",                                 # after the run
    ]

    def test_counts_only_samples_inside_the_run(self):
        peak, limit, shares, lock_db = capacity.pg_summary(self.LINES, 100, 200)
        self.assertEqual((peak, limit), (120, 500))
        self.assertEqual(shares, {"CPU": 0.25, "IO": 0.25, "Lock": 0.5})
        self.assertEqual(lock_db, "ticketbottle_inventory")

    def test_a_wal_lwlock_waits_on_the_disk(self):
        _, _, shares, _ = capacity.pg_summary(["W|5|LWLock|WALWrite|temporal|3"], 0, 10)
        self.assertEqual(shares, {"IO": 1.0})

    def test_no_active_session_is_no_share(self):
        self.assertEqual(capacity.pg_summary(["S|5|10|100"], 0, 10), (10, 100, {}, "-"))


class Verdict(unittest.TestCase):
    def test_nothing_binds(self):
        self.assertEqual(capacity.verdict(CALM), [])

    def test_each_limit_on_its_own(self):
        cases = [
            (signals(busiest_node_js=("app-gateway-a", 0.9)), ["event loop: app-gateway-a"]),
            (signals(busy_cores=13.6), ["cpu"]),
            (signals(iops=2400.0), ["disk"]),
            (signals(pg_shares={"IO": 0.6, "CPU": 0.4}), ["disk"]),
            (signals(pg_shares={"Lock": 0.5, "CPU": 0.5}, lock_db="ticketbottle_inventory"),
             ["locks: ticketbottle_inventory"]),
            (signals(first_task_p50=0.5), ["temporal dispatch"]),
            (signals(unexpected_share=0.02), ["errors"]),
        ]
        for s, want in cases:
            self.assertEqual(capacity.verdict(s), want)

    def test_just_under_every_threshold_binds_nothing(self):
        s = signals(busiest_node_js=("app-gateway-a", 0.84), busy_cores=13.5, iops=2399.0,
                    pg_shares={"IO": 0.49, "Lock": 0.49}, first_task_p50=0.49, unexpected_share=0.01)
        self.assertEqual(capacity.verdict(s), [])

    def test_a_cause_comes_before_its_symptoms(self):
        s = signals(busy_cores=15.0, first_task_p50=2.0, unexpected_share=0.05)
        self.assertEqual(capacity.verdict(s), ["cpu", "temporal dispatch", "errors"])


class Void(unittest.TestCase):
    META = {"restarts_before": 3, "restarts_after": 3}

    def test_a_clean_run_counts(self):
        self.assertEqual(capacity.void(self.META, 10, 1000), [])

    def test_a_restart_voids_the_run(self):
        self.assertEqual(capacity.void({"restarts_before": 3, "restarts_after": 5}, 0, 1000),
                         ["2 container restarts"])

    def test_a_throttled_or_unread_load_generator_voids_the_run(self):
        self.assertEqual(capacity.void(self.META, 60, 1000), ["k6 throttled 6%"])
        self.assertEqual(capacity.void(self.META, 0, 0), ["k6 throttling unread"])


def prom(path, label, values):
    result = [{"metric": {label: k} if label else {}, "value": [0, str(v)]} for k, v in values.items()]
    with open(path, "w") as f:
        json.dump({"status": "success", "data": {"resultType": "vector", "result": result}}, f)


class Summarize(unittest.TestCase):
    def test_one_run_reads_back_as_its_numbers(self):
        with tempfile.TemporaryDirectory() as d:
            # A 100s run on 16 vCPUs: 250 purchases, 12 cores busy, 2500 IOPS.
            with open(f"{d}/meta.json", "w") as f:
                json.dump({"label": "v80-1", "vcpu": 16, "vus": 80, "from": "2026-10-09T10:00:00Z",
                           "to": "2026-10-09T10:01:40Z", "restarts_before": 0, "restarts_after": 0}, f)
            with open(f"{d}/k6.log", "w") as f:
                f.write('time="x" level=info msg="CHECKOUT TB-1 900.0 201" source=console\n'
                        'time="x" level=info msg="CHECKOUT TB-2 1100.0 201" source=console\n'
                        '{"metrics": {"tb_orders_completed": {"count": 250, "rate": 2.5}}}\n')
            prom(f"{d}/pods-from.json", "pod", {"temporal-abc-def": 10, "postgres-0": 5, "app-gateway-x-y": 0})
            prom(f"{d}/pods-to.json", "pod", {"temporal-abc-def": 310, "postgres-0": 205, "app-gateway-x-y": 60,
                                              "k6-load-z": 50})
            prom(f"{d}/node-from.json", "mode", {"user": 0, "system": 0, "idle": 0, "iowait": 0, "steal": 0})
            prom(f"{d}/node-to.json", "mode", {"user": 1000, "system": 200, "idle": 400, "iowait": 50, "steal": 30})
            prom(f"{d}/disk-from.json", "", {"": 1000})
            prom(f"{d}/disk-to.json", "", {"": 251000})
            prom(f"{d}/k6-throttled.json", "pod", {"k6-load-z": 1})
            prom(f"{d}/k6-periods.json", "pod", {"k6-load-z": 1000})
            with open(f"{d}/pg.txt", "w") as f:
                f.write("S|1791540000|130|500\nW|1791540000|CPU|-|temporal|3\nW|1791540000|IO|WALSync|temporal|1\n")
            open(f"{d}/histories.txt", "w").close()

            line, busiest = capacity.summarize(d)

        self.assertTrue(line.startswith("v80-1 16 vCPU 80 buyers | 2.50 purchases/s,"), line)
        self.assertIn("busy 12.0/16 iowait 0.5 | 2500 IOPS", line)
        self.assertIn("pg 130/500 conns, CPU 75% IO 25% Lock 0%", line)
        self.assertIn("first task p50/max n/a", line)
        self.assertIn("core-s/purchase temporal 1.200 postgres 0.800", line)
        self.assertTrue(line.endswith("0 unexpected -> disk"), line)
        self.assertEqual(busiest, "  busiest: temporal-abc-def 3.00, postgres-0 2.00, app-gateway-x-y 0.60")


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run them.** `PYTHONDONTWRITEBYTECODE=1 python3 deploy/loadtest/capacity_test.py`.
  Expected: `ModuleNotFoundError: No module named 'capacity'`.

- [ ] **Step 3: Write the module** at `deploy/loadtest/capacity.py`:

```python
"""What bound purchase throughput in one capacity run (decision 0029).

Reads the files deploy/scripts/capacity-sweep.sh saved for a run and prints two
lines: what the run served with the signals that name its limit, then the
busiest pods. How to read the verdict: docs/plans/2026-10-09-capacity-on-a-bigger-box.md.
  python3 deploy/loadtest/capacity.py RUN_DIR
"""
import json
import os
import re
import sys
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import saga_latency  # noqa: E402

CPU_BOUND = 0.85      # share of the node's vCPUs busy
EVENT_LOOP = 0.85     # cores one Node.js pod spends; its JavaScript runs on one thread
GP3_IOPS = 3000       # the box's root volume, gp3 at its baseline
DISK_BOUND = 0.8      # share of GP3_IOPS
WAIT_SHARE = 0.5      # share of active Postgres sessions in one wait class
TEMPORAL_WAIT = 0.5   # seconds a saga's first workflow task waits, p50
ERROR_SHARE = 0.01    # unexpected failures per attempted checkout
THROTTLED = 0.05      # k6's throttled CFS periods; past this k6 measured itself
NODE_JS = ("app-gateway-", "user-service-", "event-service-", "payment-service-",
           "payment-webhook-", "outbox-relay-")


def vector(path, label):
    """Returns {label value: sample} from a saved Prometheus query response."""
    with open(path) as f:
        return {r["metric"].get(label, ""): float(r["value"][1]) for r in json.load(f)["data"]["result"]}


def deltas(before, after):
    """Counter growth for each key in both; a key in one only started or ended mid-run."""
    return {k: after[k] - before[k] for k in after.keys() & before.keys()}


def pg_summary(lines, t0, t1):
    """Returns peak connections, max_connections, each wait class's share of active sessions, and the database most waited on for locks, in [t0, t1).

    Lines: S|epoch|connections|max_connections and W|epoch|class|event|database|count.
    An LWLock on the WAL counts as IO: either way the commit waits on the disk.
    """
    peak, limit, by_class, lock_dbs = 0, 0, {}, {}
    for line in lines:
        f = line.strip().split("|")
        if len(f) < 4 or not f[1].isdigit() or not t0 <= int(f[1]) < t1:
            continue
        if f[0] == "S":
            peak, limit = max(peak, int(f[2])), int(f[3])
        elif f[0] == "W" and len(f) == 6:
            kind = "IO" if f[2] == "LWLock" and f[3].startswith("WAL") else f[2]
            by_class[kind] = by_class.get(kind, 0) + int(f[5])
            if kind == "Lock":
                lock_dbs[f[4]] = lock_dbs.get(f[4], 0) + int(f[5])
    total = sum(by_class.values())
    shares = {k: v / total for k, v in by_class.items()} if total else {}
    return peak, limit, shares, max(lock_dbs, key=lock_dbs.get) if lock_dbs else "-"


def verdict(s):
    """Every limit the signals show, in the order the plan reads them; [] when none binds."""
    fired = []
    pod, cores = s["busiest_node_js"]
    if cores >= EVENT_LOOP:
        fired.append(f"event loop: {pod}")
    if s["busy_cores"] >= CPU_BOUND * s["vcpu"]:
        fired.append("cpu")
    if s["iops"] >= DISK_BOUND * GP3_IOPS or s["pg_shares"].get("IO", 0) >= WAIT_SHARE:
        fired.append("disk")
    if s["pg_shares"].get("Lock", 0) >= WAIT_SHARE:
        fired.append(f"locks: {s['lock_db']}")
    if s["first_task_p50"] >= TEMPORAL_WAIT:
        fired.append("temporal dispatch")
    if s["unexpected_share"] > ERROR_SHARE:
        fired.append("errors")
    return fired


def void(meta, throttled, periods):
    """Why the run measured something other than the system; [] when it did not."""
    reasons = []
    if meta["restarts_after"] > meta["restarts_before"]:
        reasons.append(f"{meta['restarts_after'] - meta['restarts_before']} container restarts")
    if periods == 0:
        reasons.append("k6 throttling unread")
    elif throttled / periods > THROTTLED:
        reasons.append(f"k6 throttled {100 * throttled / periods:.0f}%")
    return reasons


def text(path):
    with open(path) as f:
        return f.read()


def epoch(ts):
    return int(datetime.fromisoformat(ts.replace("Z", "+00:00")).timestamp())


def counter(log, name):
    m = re.search(rf'"{name}":\s*\{{[^}}]*"count":\s*(\d+)', log)
    return int(m.group(1)) if m else 0


def summarize(d):
    meta = json.loads(text(f"{d}/meta.json"))
    t0, t1 = epoch(meta["from"]), epoch(meta["to"])
    secs = t1 - t0
    log = text(f"{d}/k6.log")
    completed, unexpected = counter(log, "tb_orders_completed"), counter(log, "tb_unexpected_errors")
    client = saga_latency.load_client(f"{d}/k6.log")
    ok = [s for _, s, status in client if status < 400]

    pods = {p: c / secs for p, c in deltas(vector(f"{d}/pods-from.json", "pod"), vector(f"{d}/pods-to.json", "pod")).items()}
    modes = deltas(vector(f"{d}/node-from.json", "mode"), vector(f"{d}/node-to.json", "mode"))
    disk = deltas(vector(f"{d}/disk-from.json", ""), vector(f"{d}/disk-to.json", ""))
    node_js = [(p, c) for p, c in pods.items() if p.startswith(NODE_JS)]
    peak, limit, shares, lock_db = pg_summary(text(f"{d}/pg.txt").splitlines(), t0, t1)
    waits = [r["wft_queued"][0] for r in saga_latency.load_histories(f"{d}/histories.txt") if r["wft_queued"]]
    temporal = sum(c for p, c in pods.items() if p.startswith("temporal-") and not p.startswith("temporal-ui-"))
    postgres = pods.get("postgres-0", 0)

    s = {
        "vcpu": meta["vcpu"],
        "busy_cores": sum(v for m, v in modes.items() if m not in ("idle", "iowait", "steal")) / secs,
        "iowait_cores": modes.get("iowait", 0) / secs,
        "iops": sum(disk.values()) / secs,
        "busiest_node_js": max(node_js, key=lambda pc: pc[1]) if node_js else ("-", 0),
        "pg_shares": shares,
        "lock_db": lock_db,
        "first_task_p50": saga_latency.pct(waits, 50) if waits else 0,
        "unexpected_share": unexpected / max(1, len(client) + unexpected),
    }
    throttled = sum(vector(f"{d}/k6-throttled.json", "pod").values())
    periods = sum(vector(f"{d}/k6-periods.json", "pod").values())
    bad = void(meta, throttled, periods)
    per = lambda cores: cores * secs / max(1, completed)
    share = lambda k: f"{100 * shares.get(k, 0):.0f}%"
    first = f"{saga_latency.pct(waits, 50):.2f}/{max(waits):.2f}s" if waits else "n/a"
    line = (f"{meta['label']} {meta['vcpu']} vCPU {meta['vus']} buyers | {completed / secs:.2f} purchases/s,"
            + (f" checkout p50 {saga_latency.pct(ok, 50):.2f}s p99 {saga_latency.pct(ok, 99):.2f}s" if ok else " no checkout")
            + f" | busy {s['busy_cores']:.1f}/{meta['vcpu']} iowait {s['iowait_cores']:.1f} | {s['iops']:.0f} IOPS"
            + f" | pg {peak}/{limit} conns, CPU {share('CPU')} IO {share('IO')} Lock {share('Lock')}"
            + f" | first task p50/max {first} | core-s/purchase temporal {per(temporal):.3f} postgres {per(postgres):.3f}"
            + f" | {unexpected} unexpected -> {', '.join(verdict(s)) or 'none'}")
    top = sorted(pods.items(), key=lambda pc: -pc[1])[:6]
    return [("VOID (" + "; ".join(bad) + ") " if bad else "") + line,
            "  busiest: " + ", ".join(f"{p} {c:.2f}" for p, c in top)]


if __name__ == "__main__":
    print("\n".join(summarize(sys.argv[1])))
```

- [ ] **Step 4: Run them.** The same command with
  `-W error::ResourceWarning` after `python3`. Expected: `Ran 11 tests`, `OK`.

- [ ] **Step 5: Prove they can go red.** Make each mutation, run, expect a failure,
  and restore. Keep `PYTHONDONTWRITEBYTECODE=1`: a same-second edit otherwise
  reuses stale bytecode.
  - In `verdict`, `s["busy_cores"] >= CPU_BOUND` becomes `>`.
  - In `pg_summary`, the `kind = ...` line becomes `kind = f[2]`.
  - In `deltas`, `after[k] - before[k]` becomes `after[k]`.
  - In `summarize`, `m not in ("idle", "iowait", "steal")` becomes `m != "idle"`.

- [ ] **Step 6: Write the Postgres sample** at `deploy/loadtest/pg-sample.sql`:

```sql
-- One sample of Postgres for deploy/loadtest/capacity.py: connections against the
-- limit, then active sessions by wait event (none means on CPU).
SELECT 'S', extract(epoch FROM now())::bigint, count(*), current_setting('max_connections')
FROM pg_stat_activity;
SELECT 'W', extract(epoch FROM now())::bigint, coalesce(wait_event_type, 'CPU'),
       coalesce(wait_event, '-'), coalesce(datname, '-'), count(*)
FROM pg_stat_activity
WHERE state = 'active' AND backend_type = 'client backend' AND pid <> pg_backend_pid()
GROUP BY 3, 4, 5;
```

  Check it against any Postgres 15, for example a local container:
  `docker exec -i <container> psql -U <user> -d postgres -AtF'|' -f - < deploy/loadtest/pg-sample.sql`.
  Expected: one line `S|<epoch>|<connections>|100`, and `W` lines only for other active sessions.

- [ ] **Step 7: Give k6 a configurable ceiling.** k6 at 640 buyers needs more than
  512Mi. In `deploy/loadtest/job.yaml`, the limits line becomes:

```yaml
            limits:   { cpu: "${K6_CPU}", memory: "${K6_MEMORY}" }
```

  In `gate4a-load.sh`, after `VUS=${VUS:-300}`:

```bash
# k6's own ceiling; a capacity run at hundreds of buyers needs more than a gate.
K6_CPU=${K6_CPU:-1}
K6_MEMORY=${K6_MEMORY:-512Mi}
```

  And in the `envsubst` invocation, after `DURATION="$DURATION" \`, add the line
  `K6_CPU="$K6_CPU" K6_MEMORY="$K6_MEMORY" \`. Check the render:

```bash
PURCHASE_JS="    x" EVENT_ID=e TICKET_CLASS_ID=t VUS=5 USER_PREFIX=u DURATION=3m K6_CPU=2 K6_MEMORY=3Gi \
  envsubst < deploy/loadtest/job.yaml | grep 'limits:'
```

  Expected: `limits:   { cpu: "2", memory: "3Gi" }`.

- [ ] **Step 8: Write the sweep** at `deploy/scripts/capacity-sweep.sh`, then `chmod +x`:

```bash
#!/usr/bin/env bash
# Finds what bounds purchase throughput on the k3s box. Each argument is one run
# of that many buyers for $DURATION with the door open; capacity.py prints what
# it served and the limit it hit, also into $OUT/summary.txt. Deploy with
# deploy/loadtest/capacity-values.yaml first. Needs GW, KUBECONFIG, Prometheus on :9090.
#   deploy/scripts/capacity-sweep.sh 10 20 40 80
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=ticketbottle
PROM=${PROM:-http://localhost:9090}
OUT=${OUT:-/tmp/capacity-sweep}
DURATION=${DURATION:-3m}
: "${GW:?set GW, e.g. http://localhost:3000/api}"
[[ "$DURATION" =~ ^[0-9]+m$ ]] || { echo "DURATION must be whole minutes, e.g. 3m"; exit 1; }
MINS=${DURATION%m}

q() { curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" > "$3"; }
at() {
  curl -fsS "$PROM/api/v1/query" --data-urlencode "query=$1" --data-urlencode "time=$2" |
    python3 -c 'import sys, json; r = json.load(sys.stdin)["data"]["result"]; print(r[0]["value"][1] if r else "nan")'
}
temporal_idle() {
  local q='sum(rate(container_cpu_usage_seconds_total{namespace="ticketbottle",pod=~"temporal-[a-z0-9]+-[a-z0-9]+",container!=""}[1m]))'
  until python3 -c "import sys; sys.exit(0 if float('$(at "$q" "$(date -u +%FT%TZ)")') < 0.05 else 1)"; do sleep 10; done
}
restarts() {
  kubectl -n $NS get pods -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.restartCount}{"\n"}{end}{end}' |
    awk '{ s += $1 } END { print s + 0 }'
}
plus() { python3 -c 'import sys; from datetime import datetime, timedelta; t = datetime.fromisoformat(sys.argv[1].replace("Z", "+00:00")); print((t + timedelta(seconds=int(sys.argv[2]))).strftime("%Y-%m-%dT%H:%M:%SZ"))' "$1" "$2"; }

# A run measures the design only on the capacity overlay: say which part is missing.
url=$(kubectl -n $NS exec deploy/payment-service -- printenv ZALOPAY_CREATE_URL || true)
[ "$url" = "http://payment-webhook:8080/v2/create" ] || { echo "payment-service calls '${url:-the ZaloPay sandbox}', not the simulated provider"; exit 1; }
limited=$(kubectl -n $NS get pods --field-selector=status.phase=Running \
  -o jsonpath='{range .items[*]}{range .spec.containers[*]}{.resources.limits.cpu}{"\n"}{end}{end}' | grep -c . || true)
[ "$limited" = 0 ] || { echo "$limited running containers still have a CPU limit"; exit 1; }
conns=$(kubectl -n $NS exec statefulset/postgres -- psql -U root -d postgres -tAc 'SHOW max_connections')
[ "$conns" = 500 ] || { echo "Postgres allows $conns connections, not 500"; exit 1; }
VCPU=$(kubectl get nodes -o jsonpath='{.items[0].status.capacity.cpu}')
echo "box: $VCPU vCPU; runs of $DURATION; raw files under $OUT"

n=0
for VUS in "$@"; do
  n=$((n + 1)); L="v$VUS-$n"; D="$OUT/$VCPU-vcpu/$L"; mkdir -p "$D"
  temporal_idle
  before=$(restarts)
  # One exec samples Postgres every 2s from inside its pod, through seeding and the run.
  kubectl -n $NS exec -i statefulset/postgres -- sh -c \
    "cat > /tmp/pg-sample.sql; for i in \$(seq 1 $(( (MINS * 60 + 180) / 2 ))); do psql -U root -d postgres -AtF'|' -f /tmp/pg-sample.sql; sleep 2; done" \
    < "$HERE/../loadtest/pg-sample.sql" > "$D/pg.txt" 2>&1 &
  PG=$!
  TOTAL=100000 VUS=$VUS DURATION=$DURATION K6_CPU=2 K6_MEMORY=3Gi "$HERE/gate4a-load.sh" > "$D/run.txt" 2>&1 ||
    { kill $PG 2>/dev/null; echo "$L: load run failed: $(tail -1 "$D/run.txt")"; exit 1; }
  kill $PG 2>/dev/null || true
  after=$(restarts)

  kubectl -n $NS logs job/k6-load > "$D/k6.log"
  read -r K6POD FROM TO <<<"$(kubectl -n $NS get pod -l job-name=k6-load -o jsonpath='{.items[0].metadata.name} {.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}')"
  for edge in from to; do
    T=$FROM; [ $edge = to ] && T=$TO
    q 'sum by (pod) (container_cpu_usage_seconds_total{namespace="ticketbottle",container!=""})' "$T" "$D/pods-$edge.json"
    q 'sum by (mode) (node_cpu_seconds_total)' "$T" "$D/node-$edge.json"
    q 'sum(node_disk_reads_completed_total{device=~"nvme[0-9]+n[0-9]+"}) + sum(node_disk_writes_completed_total{device=~"nvme[0-9]+n[0-9]+"})' "$T" "$D/disk-$edge.json"
  done
  for m in throttled_periods periods; do
    q "max_over_time(container_cpu_cfs_${m}_total{namespace=\"ticketbottle\",pod=\"$K6POD\",container=\"k6\"}[$((MINS * 60 + 60))s])" "$TO" "$D/k6-${m%_periods}.json"
  done
  # Sagas started 60-90s in, past the opening burst: a whole run is too many to fetch.
  "$HERE/saga-histories.sh" "$(plus "$FROM" 60)" "$(plus "$FROM" 90)" "$D/histories.txt" > /dev/null

  printf '{"label": "%s", "vcpu": %s, "vus": %s, "from": "%s", "to": "%s", "restarts_before": %s, "restarts_after": %s}\n' \
    "$L" "$VCPU" "$VUS" "$FROM" "$TO" "$before" "$after" > "$D/meta.json"
  python3 "$HERE/../loadtest/capacity.py" "$D" | tee -a "$OUT/summary.txt"
done
```

  Check it parses: `bash -n deploy/scripts/capacity-sweep.sh`. Its live behaviour is
  Task 6's first run.

- [ ] **Step 9: Commit**

```bash
git add deploy/loadtest/capacity.py deploy/loadtest/capacity_test.py deploy/loadtest/pg-sample.sql deploy/loadtest/job.yaml deploy/scripts/capacity-sweep.sh deploy/scripts/gate4a-load.sh
git commit -m "feat(loadtest): sweep buyers on one box and name the limit each run hits"
```

---

### Task 5: Before the session

Done by the architect, the day before if the quota needs raising.

- [ ] **Step 1: The vCPU quota.** After `aws login`:

```bash
aws service-quotas get-service-quota --region us-east-1 --service-code ec2 \
  --quota-code L-1216C47A --query 'Quota.Value'
```

  Expected: 16 or more, because the box at 16 vCPUs is the only instance running.
  If it is lower, request 16 now:
  `aws service-quotas request-service-quota-increase --region us-east-1 --service-code ec2 --quota-code L-1216C47A --desired-value 16`.
  Do not start the session until it is granted.

- [ ] **Step 2: The sizes exist in the box's zone.**

```bash
AZ=$(aws ec2 describe-instances --region us-east-1 --instance-ids $(make -s -C deploy k3s-instance) \
  --query 'Reservations[0].Instances[0].Placement.AvailabilityZone' --output text)
aws ec2 describe-instance-type-offerings --region us-east-1 --location-type availability-zone \
  --filters Name=location,Values=$AZ Name=instance-type,Values=m5.large,m5.2xlarge,m5.4xlarge \
  --query 'InstanceTypeOfferings[].InstanceType' --output text
```

  Expected: all three types. A missing type goes back to the architect, because
  another family changes the CPU generation.

- [ ] **Step 3: Push and build.** With the architect's go-ahead, push `dev`. CI builds
  every image, because Tasks 1 and 2 change `services/**` and `deploy/adapters/**`.
  Note the build's tag:
  `aws ecr describe-images --region us-east-1 --repository-name ticketbottle/gateway --image-ids imageTag=dev --query 'imageDetails[0].imageTags'`.

---

### Task 6: The session

The architect runs it from this guide and pastes the output. About three hours.

**Resize, used at every size.** From the repo root, with `TYPE` set to the size:

```bash
sed -i '' -E "s/^(instance_type[[:space:]]*=[[:space:]]*).*/\1\"$TYPE\"/" deploy/terraform/envs/k3s/terraform.tfvars
terraform -chdir=deploy/terraform/envs/k3s plan -out=/tmp/resize.plan
```

The plan should show `~ instance_type = "<old>" -> "<TYPE>"` and
`Plan: 0 to add, 1 to change, 0 to destroy.` If it says `must be replaced` or
anything to destroy, stop and do not apply. Then:

```bash
terraform -chdir=deploy/terraform/envs/k3s apply /tmp/resize.plan   # stops, resizes, starts
kubectl get nodes -o jsonpath='{.items[0].status.capacity.cpu}'      # after the new tunnel
```

The size lives in `terraform.tfvars`, so a mid-session `k3s-allow-ip` keeps it. The
same edit, with `t3.large`, is the last step.

- [ ] **Step 1: Resize to `m5.large`, then run Setup.** Expected: the node reports
  `2`. Wait until `kubectl -n ticketbottle get pods` shows every pod Running.

- [ ] **Step 2: Deploy the overlay.**
  `make -C deploy k3s-deploy REF=<sha-tag> HELM_EXTRA="-f loadtest/capacity-values.yaml"`.
  Postgres restarts once for its new argument. Then check:

```bash
kubectl -n ticketbottle exec deploy/payment-service -- printenv ZALOPAY_CREATE_URL     # http://payment-webhook:8080/v2/create
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d postgres -tAc 'SHOW max_connections'   # 500
kubectl -n ticketbottle logs deploy/waitroom-service | grep 'Starting queue processor' | tail -1           # batch_size: 1000
kubectl -n ticketbottle exec deploy/temporal -- temporal operator cluster describe -o json | grep -i shard   # record it
```

  If the last command prints no shard count, `tctl admin cluster describe` does.
  Write the count into *Results*.

- [ ] **Step 3: One purchase through the simulated provider.** `make -C deploy k3s-gate2`,
  then:

```bash
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d ticketbottle_payment -tAc \
  'SELECT "paymentUrl" FROM payments ORDER BY "createdAt" DESC LIMIT 1'
```

  Expected: the gate passes, and the URL starts with `http://payment-webhook:8080/pay/`.
  A `zalopay.vn` URL means payment still calls the sandbox, so stop.

- [ ] **Step 4: Predict before measuring.** Written here, 2026-10-09:
  - **2 vCPU:** a plateau of 2.7–3.6 purchases a second, with verdict `cpu`. That
    is the t3.large's 3.15 give or take 15%, since the same CPU generation now runs
    without the sandbox's TLS. Outside that range, the box changed more than its
    size, so stop.
  - **16 vCPU:** a plateau under 6 times the 2-vCPU plateau, three quarters of linear,
    with a first verdict that is not `cpu`. The suspects are `disk` (one gp3 volume
    carries every commit, Temporal's included) and `temporal dispatch` (4 history
    shards).

- [ ] **Step 5: Sweep at 2 vCPU.** `deploy/scripts/capacity-sweep.sh 10 20 40 80`.
  It takes about 30 minutes. It stops with a reason if the overlay is not in force.

- [ ] **Step 6: Read every line** with these rules, in order:
  - **Void.** A line starting `VOID` does not count. Rerun that number of buyers once.
    If it is void again, stop and look.
  - **Plateau.** The plateau is reached when the top run's purchases a second is
    within 10% of the run below it. Otherwise, run again at twice the top number.
  - **Event loop.** If the plateau's verdict starts `event loop: <pod>`, one Node.js
    process is the limit. That is configuration, not design. Redeploy with
    `HELM_EXTRA="-f loadtest/capacity-values.yaml --set replicas.<workload>=<twice its count>"`,
    keep that `--set` on every later deploy this session, and rerun the top two
    numbers.
  - **The limit** is the first entry of the plateau's verdict. Later entries are
    usually its symptoms: a saturated CPU also delays Temporal's tasks.

- [ ] **Step 7: Resize to `m5.4xlarge`, reopen the tunnel, and sweep.** The node
  reports `16`, and the release keeps the overlay across the resize.
  `deploy/scripts/capacity-sweep.sh 80 160 320 640`. Read it with Step 6's rules.

- [ ] **Step 8: Only if 16 vCPUs served under 6 times the 2-vCPU plateau:** resize to
  `m5.2xlarge` and run `deploy/scripts/capacity-sweep.sh 40 80 160 320`, to show
  where the curve bends.

- [ ] **Step 9: Put the box back.** Resize to `t3.large`, reopen the tunnel, and
  deploy without the overlay: `make -C deploy k3s-deploy REF=<sha-tag>`. Then check
  each value is back:

```bash
kubectl -n ticketbottle exec deploy/payment-service -- printenv ZALOPAY_CREATE_URL     # nothing, exit 1
kubectl -n ticketbottle get deploy temporal -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}'   # 1
kubectl -n ticketbottle exec statefulset/postgres -- psql -U root -d postgres -tAc 'SHOW max_connections'   # 100
kubectl -n ticketbottle logs deploy/waitroom-service | grep 'Starting queue processor' | tail -1           # batch_size: 2
make -C deploy k3s-gate2                                                                                   # passes, through the sandbox
```

  Then kill the port-forward and the tunnel, run `make -C deploy stop-ec2-k3s`, and
  confirm both `stopped` and `grep instance_type deploy/terraform/envs/k3s/terraform.tfvars`
  printing `t3.large`. Last, `aws ec2 describe-instance-credit-specifications --instance-ids
  $(make -s -C deploy k3s-instance)` must print `unlimited`: a T instance resized from an m5
  takes the account's credit default, and the box's measured capacity needs `unlimited`
  (`modules/ec2-k3s/variables.tf`). `terraform -chdir=deploy/terraform/envs/k3s plan` must
  show no resource change; on a stopped box its outputs always differ.

---

### Task 7: Record

**Files:**
- Modify: this plan (*Results*, *What this says*, status)
- Modify: `docs/decisions/0029-the-next-testbed-is-one-bigger-box.md`, `docs/decisions/README.md` (generated)
- Modify: `CLAUDE.md` (the register row), `.claude/skills/system-map/references/order.md` (its row)

- [ ] **Step 1: Fill *Results*** from `/tmp/capacity-sweep/summary.txt`. Recompute
  every number from the files. A number read off one line from memory has been
  wrong four times in this repo.

- [ ] **Step 2: Write *What this says*:** the scaling ratio from 2 to 16 vCPUs, the
  limit and its evidence, whether the prediction held, and what the run did not
  foresee.

- [ ] **Step 3: Record.**
  - **0029:** `accepted`, with an `## Outcome` that cites the commits, the red-then-green
    tests and their mutations, the session's build, and the plateau lines.
  - **Root `CLAUDE.md`:** if a limit was named, move the Open row to *Decided*, owned by
    this plan, with the limit and the date. If not, rewrite its *Why it is open*
    with what was ruled out and up to what size.
  - **`order.md` map row:** the same state, pointing at the register.

- [ ] **Step 4: Check and commit.** Run `python3 docs/decisions/index.py`,
  `python3 docs/decisions/index.py --check` and
  `python3 .claude/skills/system-map/scripts/check_map.py`. Set this plan's status to
  COMPLETE, then:

```bash
git add docs CLAUDE.md .claude/skills/system-map
git commit -m "docs: record what bounds purchase throughput as the box grows"
```

## Results

| Setup | Size | vCPU | Buyers | Purchases/s | Checkout p50 / p99 | Busy / vCPU | IOPS | Postgres conns | Postgres CPU / IO / Lock | First task p50 / max | Temporal / Postgres core-s | Verdict |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 partition, 1 worker | m5.large | 2 | 10 | 2.11 | 0.58 / 1.33s | 1.0 / 2 | 188 | 65 | 49 / 40 / 0% | 0.02 / 0.11s | 0.101 / 0.092 | none |
| | m5.large | 2 | 20 | 3.97 | 0.90 / 2.93s | 1.6 / 2 | 234 | 70 | 62 / 26 / 0% | 0.03 / 0.35s | 0.090 / 0.098 | none |
| | m5.large | 2 | 40 | **4.26** | 1.42 / 3.92s | 1.9 / 2 | 295 | 72 | 41 / 42 / 0% | 0.12 / 2.60s | 0.098 / 0.103 | cpu |
| | m5.large | 2 | 80 | 3.80 | 1.75 / 12.76s | 1.8 / 2 | 322 | 76 | 50 / 39 / 0% | 0.10 / 2.34s | 0.104 / 0.109 | cpu |
| 1 partition, 1 worker | m5.4xlarge | 16 | 80 | 9.44 | 0.56 / 2.43s | 2.8 / 16 | 608 | 112 | 28 / 47 / 0% | 0.02 / 0.09s | 0.091\* / 0.068\* | none |
| | m5.4xlarge | 16 | 160 | **9.59** | 0.56 / 3.27s | 3.2 / 16 | 604 | 109 | 26 / 55 / 0% | 0.02 / 2.03s | 0.084 / 0.071 | disk |
| | m5.4xlarge | 16 | 320 | 9.09 | 0.61 / 5.94s | 3.8 / 16 | 710 | 111 | 22 / 53 / 0% | 0.02 / 0.09s | 0.099 / 0.083 | disk |
| | m5.4xlarge | 16 | 640 | 8.74 | 0.84 / 12.79s | 5.4 / 16 | 756 | 121 | 28 / 52 / 0% | 0.03 / 0.09s | 0.112 / 0.100 | event loop: app-gateway, disk |
| 8 partitions, 1 worker | m5.4xlarge | 16 | 160 | 23.30 | 1.22 / 4.47s | 7.1 / 16 | 1342 | 119 | 20 / 55 / 0% | 0.25 / 2.28s | 0.092 / 0.082 | disk |
| | m5.4xlarge | 16 | 320 | **25.49** | 6.97 / 15.30s | 7.2 / 16 | 1524 | 120 | 21 / 58 / 0% | 5.72 / 13.87s | 0.091 / 0.078 | disk, temporal dispatch |
| 1 partition, 2 workers | m5.4xlarge | 16 | 320 | 9.56 | 0.59 / 4.96s | 3.7 / 16 | 831 | 118 | 27 / 51 / 0% | 0.01 / 0.07s | 0.095 / 0.078 | disk |
| 8 partitions, 2 workers | m5.4xlarge | 16 | 320 | 23.39 | 0.91 / 6.44s | 7.9 / 16 | 1467 | 99 | 27 / 52 / 0% | 0.08 / 3.69s | 0.098 / 0.087 | event loop: app-gateway, disk |
| control, cold | t3.large | 2 | 40 | 2.70 | 1.82 / 10.23s | 1.7 / 2 | 331 | 83† | 58 / 28 / 0%† | 0.10 / 2.12s | 0.133 / 0.139 | cpu |
| control, warm | t3.large | 2 | 40 | 2.71 | 1.87 / 11.29s | 1.8 / 2 | 246 | 84 | 54 / 29 / 0% | 0.23 / 2.23s | 0.131 / 0.135 | cpu |

2026-10-11, 07:38–09:39Z, build `sha-e83dd91`, runs of 3 minutes on the capacity
overlay, each row recomputed from its run's files with `capacity.py`. *1 partition,
1 worker* is the chart as shipped: `payment.completed` has one partition and one
order-service replica runs the CreateOrder worker. The 8-partition rows raised the
topic by hand (`rpk topic add-partitions`), the 2-worker rows added
`--set replicas.order-service=2`, and both were reverted before the box went back.
The 1-partition, 2-worker run met a topic already reverted; its ConfirmOrders ran one
at a time, so it shows the second worker alone leaves the first wall where it is.
No run was void: no container restarted inside a run, and k6 was throttled in at most
0.8% of its periods. The plateaus are in bold.

\* The run began minutes after the resize, while Prometheus still returned each
restarted container's last sample from before it, so its per-pod counters ran
backwards (Temporal 354 to 159 core-s). These two are `increase()` over the run.
Node CPU, IOPS and throughput are unaffected.
† The warm rerun's sampler overwrote this run's Postgres file; the values are from the
line `capacity.py` printed when the run ended.


Temporal history shards: 4 (`historyShardCount`, `temporal operator cluster describe`,
read on the m5.large on 2026-10-11).

## What this says

**The first wall is the `payment.completed` consumer, at about 9.5 purchases a second
on any box.**
- On 16 vCPUs, throughput held at 9.44, 9.59, 9.09 and 8.74 from 80 to 640 buyers,
  with 2.8 to 5.4 of 16 cores busy.
- Every topic has one partition (`rpk topic list`): the chart creates none, and
  Redpanda's default is one. `order-consumer` takes a partition's messages in order,
  and for each starts `ConfirmOrder` and waits for it to finish
  (`services/order-svc/internal/order/service/consumer.go`, `wfRun.Get`).
- In 30 seconds of the 640-buyer run, 256 ConfirmOrders ran with no overlap: 109ms
  each on average, 7ms from one closing to the next starting, the consumer busy 93% of
  the time. One per 116ms is 8.5 a second, the confirm rate over the same window.
- With 8 partitions and nothing else changed, 160 buyers bought 23.30 a second
  instead of 9.59.

**Past it, the ceiling is about 24 a second, in Temporal and its Postgres. Which part
of that path binds is not separated.**
- With 8 partitions: 23.30 at 160 buyers, 25.49 at 320. Busy 7.1 to 7.9 of 16 cores,
  IOPS at most 1,524 of gp3's 3,000, no Lock waits.
- At 320 buyers checkout p50 rose to 6.97s, all of it the CreateOrder workflow's first
  task waiting for a worker (p50 5.72s). The worker runs the SDK's default of 2
  workflow-task pollers (`services/order-svc/internal/infra/temporal/workers.go`). A
  second order-service replica cut that wait to 0.08s and checkout p50 to 0.91s, and
  served 23.39 a second: the pollers set the latency, not the ceiling.
- About 90% of Postgres's active sessions belong to Temporal's two databases, and
  their IO waits are WAL flushes (`WALSync`, `WALWrite`). What is left is Temporal's
  4 history shards or commit flushes on the one volume every database shares. Telling
  them apart takes a server-side change, `synchronous_commit` or a shard count.

**From 2 to 16 vCPUs:** 2.25 times the throughput as shipped (4.26 to 9.59), 6.0
times with the consumer's wall removed (4.26 to 25.49).

**The prediction.**
- 2 vCPUs: 2.7 to 3.6 predicted, 4.26 measured, verdict `cpu`. The anchor was wrong:
  3.15 came from a run with the door at 4 a second, the sandbox in the path and the
  chart's limits on (`docs/plans/2026-09-29-admission-sizing.md#results`), not an
  open-door ceiling.
- 16 vCPUs: under 6 times held (2.25), and the first verdict was not `cpu`. The
  suspects it named, `disk` and `temporal dispatch`, were not the first wall; they are
  where the second one shows.

**What the run did not foresee.**
- **The verdict cannot see a serial stage.** The first run on the plateau read `none`,
  and later runs read `disk` from Postgres's IO share while IOPS sat at a fifth of
  the baseline. A stage that handles one item at a time saturates no resource; only
  ConfirmOrder's concurrency named it.
- **The m5.large is not the t3.large.** Under the same settings at 40 buyers, the
  t3.large served 2.70 and 2.71 a second, cold and warm, against the m5.large's 4.26,
  and spent about a third more CPU a purchase (Temporal 0.131 to 0.133 core-s against
  0.098), with no steal and the same CPU model (Xeon Platinum 8259CL). The cause is
  not identified. Earlier t3 numbers, the door speed of 2 among them, stand for the
  t3 and do not compare with the m5 runs.
- **Postgres would have run out of connections.** The 16-vCPU runs peaked at 99 to
  121; the default allows 100.
- **The first run after a resize reads stale counters** (the v80 note above).

## Limits of these numbers

- **One node.** Every hop is on one machine, and every database shares one disk. A
  limit found here belongs to this topology; what changes across nodes is unmeasured.
- **The provider is simulated** at its fast mode, 330ms. A deployment in Vietnam
  would sit closer to it; the slow mode and the stalls are left out on purpose.
- **Buyers loop.** Each one buys again as soon as a purchase completes, so a run
  measures throughput at a concurrency, not latency at an arrival rate.
- **k6 shares the box,** limited to 2 cores, and a throttled run is void.
- **Counters are read at the run's edges,** at Prometheus's scrape interval, so the
  first and last 15s of each run are approximate.
- **First-task waits are sampled** from sagas started 60–90s into each run.
