# Checkout latency decomposition

**Status: IN PROGRESS 2026-09-27.** The evidence from the 06:43Z run is in. Tasks
1–5 are not started.

**Goal:** Say where a checkout's time goes at the load the waiting room admits
today, and what makes the opening burst of an on-sale slow, measured before
anything is changed.

**Question it serves:** the root `CLAUDE.md` Open row *What fails first as
concurrent checkouts rise into the thousands?* This plan answers the question
underneath it: why the checkout misses its SLO at twenty buyers.

**Spec:** none. Derived from the 06:43Z run's workflow histories and the
gateway's counters.

## Evidence so far: the 06:43Z run

20 buyers, 3 minutes, sustained, through the waiting room (`QUEUE_DEFAULT_RELEASE_RATE`
10 per 1s tick). 286 orders. No rollout overlapped it: helm revision 37 finished
at 06:32Z.

**At the gateway**, raw counters for `POST /api/orders` at 06:47:30Z: 257 of 287
under 2s, **89.5% against a 99% SLO.** The 96.4% reported the same day is about
what the run gives with its opening burst left out.

**Inside the saga**, from Temporal's workflow histories (workflow time only; the
gateway's time is on top):

| | n | over 2s | p50 | p99 | Where the time went |
|---|---|---|---|---|---|
| Burst: sagas started in the first 2s | 20 | 20 | 3.20s | 6.16s | Waiting for the **first workflow task**: p50 1.01s, max 5.28s. Each step ran as fast as in steady state. |
| Steady: the rest | 266 | 4 | 1.03s | 2.05s | `CreatePaymentIntent` p50 0.33s; hand-offs between steps ~0.35s summed; each other step ~0.05s |

1. **The opening burst is the SLO miss.** Every one of the first 20 sagas took
   over 2s, and most of that time passed before any service code ran: Temporal
   took up to 5.3s to hand a new workflow its first task.
2. **The payment call has two modes.** 263 calls took 0.2–0.6s, 23 took
   0.7–1.3s, and none fell between. From the box, one round trip to
   `sb-openapi.zalopay.vn` is 0.24s and a fresh TLS connection 0.96s. The slow
   mode is the price of a new connection, not of ZaloPay.
3. **The hand-offs cost as much as the work.** Excluding payment, the four steps
   run ~0.15s combined; the queue waits between them sum to ~0.35s at p50.

No activity was retried. Retries were a candidate cause (one retry adds at least
the 1s `InitialInterval` in `services/order-svc/internal/workflows/options.go`);
the histories rule them out.

## What the evidence does not say

| | Hypothesis | Predicts | Separated by |
|---|---|---|---|
| H1 | Cold: the first burst after idle is slow; a burst on a warm system is not | Run B's burst is fast | Task 3 |
| H2 | Queueing: the first-task wait grows with the size of the burst | Wait rises from 10 to 20 to 40 | Task 4 |
| H3 | The box: its 2 vCPUs saturate during the burst | `vmstat` idle near 0, run queue above 2 | Every run |
| H4 | Payment's slow mode is connections opened by concurrency | Slow payment calls cluster in bursts | Tasks 3–4 |

**Time outside the saga is unmeasured.** About 30 checkouts crossed 2s at the
gateway against 24 sagas, so the gateway and `order-service`'s own work before and after
the workflow cost something. Task 2 times each checkout at the client and joins
it to its saga by order code.

### What each outcome means

```
H1 alone          -> a cold start; the next plan narrows which pool goes cold
H2 with H3        -> the box is the bottleneck; past here the testbed is measured,
                     not the design
H2 without H3     -> Temporal dispatch queues with CPU to spare: the worker's 2
                     workflow-task pollers (SDK default) or Temporal's persistence
                     on the shared Postgres
neither           -> the 06:43Z burst does not reproduce; record that and stop
```

The poller default is `defaultConcurrentPollRoutineSize = 2`,
`go.temporal.io/sdk@v1.37.0/internal/internal_worker.go:43`, and
`services/order-svc/internal/infra/temporal/workers.go` does not set it.
Testing it needs a code change and a CI build, so it is not a task here.

## Limits of these numbers

- **The box is in us-east-1 and ZaloPay is in Vietnam.** A deployment serving
  Vietnam would sit near the provider, so the payment leg's absolute cost here
  belongs to the testbed. Its shape, two modes split by connection reuse, does
  not.
- k6 runs on the same 2 vCPUs as the system (`deploy/loadtest/job.yaml` limits
  it to 1 CPU).
- A k6 buyer orders the instant it is admitted; a person takes seconds. The
  burst here is the worst case of one admission tick, not the typical one.
- Temporal keeps a history for 24h (`WorkflowExecutionRetentionTtl` of the
  `default` namespace). Dump a run's histories the same day.

## Setup, every session

```bash
make -C deploy start-ec2-k3s          # prints the tunnel command; open it in its own terminal
make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml
kubectl -n monitoring port-forward svc/kps-kube-prometheus-stack-prometheus 9090:9090 &
export GW=http://localhost:3000/api
```

After a cold boot, `order-service` and `order-consumer` crash until Temporal is
up and settle within a few minutes; wait for all pods to be `Running`.

---

### Task 1: The decomposition tool

**Files:**
- Create: `deploy/loadtest/saga_latency_test.py`
- Create: `deploy/loadtest/saga_latency.py`
- Create: `deploy/scripts/saga-histories.sh`

**Produces:** `saga-histories.sh FROM TO OUT` writes one `=== <workflowId>`
line followed by one JSON history line per saga.
`saga_latency.py HISTORIES [K6_LOG] [--burst-secs 2]` prints the burst, steady
and all tables.

Not wired into CI: the tool runs by hand beside a load run, and its test runs
with it.

- [ ] **Step 1: Write the failing test**

`deploy/loadtest/saga_latency_test.py`:

```python
"""The saga latency decomposition. Run: python3 deploy/loadtest/saga_latency_test.py"""
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import saga_latency  # noqa: E402


def event(event_id, secs, kind, **attrs):
    e = {"eventId": str(event_id), "eventTime": f"2026-09-27T06:43:{10 + secs:09.6f}Z",
         "eventType": f"EVENT_TYPE_{kind}"}
    e.update(attrs)
    return e


# One saga: its first workflow task waits 1.0s, and ReserveInventory waits 0.2s
# for a worker, then runs 0.3s on its second attempt.
HISTORY = [
    event(1, 0.0, "WORKFLOW_EXECUTION_STARTED"),
    event(2, 0.0, "WORKFLOW_TASK_SCHEDULED"),
    event(3, 1.0, "WORKFLOW_TASK_STARTED"),
    event(5, 1.1, "ACTIVITY_TASK_SCHEDULED",
          activityTaskScheduledEventAttributes={"activityType": {"name": "ReserveInventory"}}),
    event(6, 1.3, "ACTIVITY_TASK_STARTED",
          activityTaskStartedEventAttributes={"scheduledEventId": "5", "attempt": 2}),
    event(7, 1.6, "ACTIVITY_TASK_COMPLETED",
          activityTaskCompletedEventAttributes={"scheduledEventId": "5"}),
    event(8, 1.7, "WORKFLOW_EXECUTION_COMPLETED"),
]


class ParseTest(unittest.TestCase):
    def test_splits_each_step_into_queued_and_ran(self):
        r = saga_latency.parse("CreateOrder:TB-1", HISTORY)
        self.assertEqual(r["code"], "TB-1")
        self.assertAlmostEqual(r["total"], 1.7)
        self.assertAlmostEqual(r["wft_queued"][0], 1.0)
        self.assertAlmostEqual(r["queued"]["ReserveInventory"], 0.2)
        self.assertAlmostEqual(r["ran"]["ReserveInventory"], 0.3)
        self.assertEqual(r["retries"], 1)

    def test_reads_checkout_lines_from_a_k6_log(self):
        with tempfile.NamedTemporaryFile("w", suffix=".log", delete=False) as f:
            f.write('time="2026-09-27T06:43:12Z" level=info msg="CHECKOUT TB-1 2150.4 201" source=console\n'
                    'time="2026-09-27T06:43:13Z" level=info msg="CHECKOUT - 12.0 409" source=console\n'
                    'time="2026-09-27T06:43:14Z" level=info msg="other" source=console\n')
        self.addCleanup(os.unlink, f.name)
        got = saga_latency.load_client(f.name)
        self.assertEqual([(code, status) for code, _, status in got], [("TB-1", 201), ("-", 409)])
        self.assertAlmostEqual(got[0][1], 2.1504)
        self.assertAlmostEqual(got[1][1], 0.012)

    def test_percentile_is_nearest_rank(self):
        xs = [float(x) for x in range(1, 101)]
        self.assertEqual(saga_latency.pct(xs, 50), 51.0)
        self.assertEqual(saga_latency.pct(xs, 99), 99.0)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run it and watch it fail**

Run: `python3 deploy/loadtest/saga_latency_test.py`
Expected: `ModuleNotFoundError: No module named 'saga_latency'`

- [ ] **Step 3: Write the tool**

`deploy/loadtest/saga_latency.py`:

```python
"""Where a checkout's time goes, from CreateOrder workflow histories.

Reads the dump saga-histories.sh writes; a k6 log from purchase.js adds the
client's view of each checkout, joined by order code.
  python3 deploy/loadtest/saga_latency.py HISTORIES [K6_LOG] [--burst-secs 2]
"""
import argparse
import json
import re
from datetime import datetime

STEPS = ["ReserveInventory", "CreateOrder", "CreateOrderItems", "CreatePaymentIntent"]
CHECKOUT = re.compile(r'msg="CHECKOUT (\S+) ([\d.]+) (\d+)"')


def parse(workflow_id, events):
    """Returns one saga's timings, in seconds from its workflow's start."""
    t0 = datetime.fromisoformat(events[0]["eventTime"])

    def at(e):
        return (datetime.fromisoformat(e["eventTime"]) - t0).total_seconds()

    r = {"code": workflow_id.split(":", 1)[1], "start": t0, "total": at(events[-1]),
         "wft_queued": [], "queued": {}, "ran": {}, "retries": 0}
    scheduled, wft = {}, 0.0
    for e in events:
        kind = e["eventType"].removeprefix("EVENT_TYPE_")
        if kind == "WORKFLOW_TASK_SCHEDULED":
            wft = at(e)
        elif kind == "WORKFLOW_TASK_STARTED":
            r["wft_queued"].append(at(e) - wft)
        elif kind == "ACTIVITY_TASK_SCHEDULED":
            name = e["activityTaskScheduledEventAttributes"]["activityType"]["name"]
            scheduled[e["eventId"]] = [name, at(e)]
        elif kind == "ACTIVITY_TASK_STARTED":
            a = e["activityTaskStartedEventAttributes"]
            scheduled[a["scheduledEventId"]].append(at(e))
            r["retries"] += int(a.get("attempt", 1)) - 1
        elif kind == "ACTIVITY_TASK_COMPLETED":
            name, sched, start = scheduled[e["activityTaskCompletedEventAttributes"]["scheduledEventId"]]
            r["queued"][name], r["ran"][name] = start - sched, at(e) - start
    return r


def load_histories(path):
    runs, workflow_id = [], None
    with open(path) as f:
        for line in f:
            if line.startswith("=== "):
                workflow_id = line[4:].strip()
            elif line.strip():
                runs.append(parse(workflow_id, json.loads(line)["events"]))
    return runs


def load_client(path):
    """Returns (order code, seconds, HTTP status) per checkout; a rejected one has code '-'."""
    with open(path) as f:
        return [(code, float(ms) / 1000, int(status)) for code, ms, status in CHECKOUT.findall(f.read())]


def pct(xs, p):
    xs = sorted(xs)
    return xs[min(len(xs) - 1, round(p / 100 * (len(xs) - 1)))]


def row(name, xs):
    if xs:
        print(f"  {name:<32}" + "".join(f"{pct(xs, p):7.2f}" for p in (50, 90, 99)) + f"{max(xs):7.2f}")


def report(label, runs, client):
    if not runs:
        return
    seen = {c: s for c, s, status in client if status < 400}
    joined = [(seen[r["code"]], r) for r in runs if r["code"] in seen]
    print(f"\n{label}: n={len(runs)} retries={sum(r['retries'] for r in runs)}"
          f" over 2s: workflow {sum(r['total'] > 2 for r in runs)}"
          + (f", client {sum(s > 2 for s, _ in joined)} of {len(joined)}" if client else ""))
    print(f"  {'':<32}{'p50':>7}{'p90':>7}{'p99':>7}{'max':>7}")
    row("checkout, client", [s for s, _ in joined])
    row("outside the workflow", [s - r["total"] for s, r in joined])
    row("workflow", [r["total"] for r in runs])
    row("first workflow task queued", [r["wft_queued"][0] for r in runs if r["wft_queued"]])
    row("all workflow tasks queued", [sum(r["wft_queued"]) for r in runs])
    for step in STEPS:
        row(f"{step} queued", [r["queued"][step] for r in runs if step in r["queued"]])
        row(f"{step} ran", [r["ran"][step] for r in runs if step in r["ran"]])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("histories")
    ap.add_argument("k6_log", nargs="?")
    ap.add_argument("--burst-secs", type=float, default=2.0,
                    help="sagas started this soon after the first are the opening burst")
    a = ap.parse_args()
    runs = load_histories(a.histories)
    client = load_client(a.k6_log) if a.k6_log else []
    first = min(r["start"] for r in runs)
    burst = [r for r in runs if (r["start"] - first).total_seconds() < a.burst_secs]
    report("burst", burst, client)
    report("steady", [r for r in runs if r not in burst], client)
    report("all", runs, client)
    rejected = [(s, status) for c, s, status in client if status >= 400]
    if rejected:
        print(f"\nrejected checkouts: {len(rejected)}, statuses {sorted({st for _, st in rejected})}")


if __name__ == "__main__":
    main()
```

- [ ] **Step 4: Run the test and watch it pass**

Run: `python3 deploy/loadtest/saga_latency_test.py`
Expected: `Ran 3 tests` … `OK`

- [ ] **Step 5: Prove the test can go red**

Swap the two values in the `ACTIVITY_TASK_COMPLETED` branch
(`r["queued"][name], r["ran"][name] = at(e) - start, start - sched`), rerun,
confirm `FAILED (failures=1)`, restore. Do the same with `r["retries"] += 0`.

- [ ] **Step 6: Write the history dump**

`deploy/scripts/saga-histories.sh`, then `chmod +x`:

```bash
#!/usr/bin/env bash
# Dumps the history of every CreateOrder saga started in [FROM, TO), for
# deploy/loadtest/saga_latency.py. Temporal keeps a history for 24h.
#   deploy/scripts/saga-histories.sh 2026-09-27T06:43:00Z 2026-09-27T06:47:00Z out.txt
set -euo pipefail
FROM=${1:?usage: saga-histories.sh FROM TO OUT  (RFC 3339, UTC)}
TO=${2:?usage: saga-histories.sh FROM TO OUT}
OUT=${3:?usage: saga-histories.sh FROM TO OUT}
NS=ticketbottle
QUERY="WorkflowType=\"CreateOrder\" AND StartTime >= \"$FROM\" AND StartTime < \"$TO\""

# One exec for the whole loop: a kubectl round trip per saga costs seconds on the tunnel.
kubectl -n $NS exec deploy/temporal -- sh -c "
  temporal workflow list --query '$QUERY' --limit 100000 -o jsonl \
    | sed -n 's/.*\"workflowId\":\"\([^\"]*\)\".*/\1/p' > /tmp/saga-ids
  for id in \$(cat /tmp/saga-ids); do echo \"=== \$id\"; temporal workflow show -w \"\$id\" -o jsonl; done > /tmp/saga-histories"
kubectl -n $NS exec deploy/temporal -- cat /tmp/saga-histories > "$OUT"
echo "$(grep -c '^=== ' "$OUT") sagas -> $OUT"
```

- [ ] **Step 7: Run it against a real run**

Before 2026-09-28T06:43Z the 06:43Z run is still retained:

```bash
deploy/scripts/saga-histories.sh 2026-09-27T06:43:00Z 2026-09-27T06:47:00Z /tmp/h-0643.txt
python3 deploy/loadtest/saga_latency.py /tmp/h-0643.txt
```

Expected: `286 sagas`, then `burst: n=20 retries=0 over 2s: workflow 20` with a
`workflow` p50 of 3.20, and `steady: n=266 … over 2s: workflow 4`. Verified
2026-09-27. After the retention window, run it against Task 3's run A instead.

- [ ] **Step 8: Commit**

```bash
git add deploy/loadtest/saga_latency.py deploy/loadtest/saga_latency_test.py deploy/scripts/saga-histories.sh
git commit -m "feat(loadtest): decompose a checkout's latency from its saga's history"
```

---

### Task 2: One line per checkout from the load script

**Files:**
- Modify: `deploy/loadtest/purchase.js`, the block after `// 3. create the order`

**Consumes:** the `CHECKOUT <code> <ms> <status>` format `saga_latency.load_client`
parses (Task 1).

- [ ] **Step 1: Log each checkout**

Replace the block from `// 409 is the only correct rejection` down to
`const code = or.json('data.order.code');`:

```js
  // 409 is the only correct rejection: sold out / sale closed / wrong-state all
  // arrive as FAILED_PRECONDITION. Any other 4xx is the harness (stale token,
  // bad body) and must not be counted as a buyer losing the race.
  if (or.status === 409) { rejected.add(1); return; }
  if (or.status >= 400) { unexpected.add(1, { step: 'order', status: or.status }); return; }

  const code = or.json('data.order.code');
```

with:

```js
  // saga_latency.py joins this line to the order's saga by its code.
  const code = or.status < 400 ? or.json('data.order.code') : null;
  console.log(`CHECKOUT ${code || '-'} ${or.timings.duration.toFixed(1)} ${or.status}`);

  // 409 is the only correct rejection: sold out / sale closed / wrong-state all
  // arrive as FAILED_PRECONDITION. Any other 4xx is the harness (stale token,
  // bad body) and must not be counted as a buyer losing the race.
  if (or.status === 409) { rejected.add(1); return; }
  if (or.status >= 400) { unexpected.add(1, { step: 'order', status: or.status }); return; }
```

The `if (!code)` line that follows stays as it is.

- [ ] **Step 2: Run a short load and check the lines**

```bash
TOTAL=100000 VUS=2 DURATION=30s deploy/scripts/gate4a-load.sh
kubectl -n ticketbottle logs job/k6-load | grep -c 'msg="CHECKOUT TB-'
```

Expected: a count equal to the `tb_orders_completed` count in the summary, give
or take the orders still being confirmed when the run ended. k6 0.54's format,
`level=info msg="CHECKOUT TB-X 9.5 404" source=console`, was checked in-cluster
on 2026-09-27.

- [ ] **Step 3: Commit**

```bash
git add deploy/loadtest/purchase.js
git commit -m "feat(loadtest): log each checkout's latency with its order code"
```

---

### Task 3: A cold burst and a warm one (H1, H3, H4)

Run A is the first checkout traffic after at least 10 minutes of none. Run B
starts as soon as A ends. `TOTAL=100000` keeps every checkout off the sold-out
path, which is a different code path with different latency.

- [ ] **Step 1: Confirm the system is idle**

```bash
curl -s localhost:9090/api/v1/query --data-urlencode \
  'query=sum(increase(tb_grpc_requests_total{service="app-gateway",method="POST /api/orders"}[10m]))'
```

Expected: `0`, or an empty result if the gateway has served no checkout since it
started. Anything else: wait and re-check.

- [ ] **Step 2: Run A, with `vmstat` on the box**

```bash
IP=$(make -s -C deploy k3s-ip)
ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=accept-new ec2-user@$IP 'vmstat -t 1' > /tmp/vmstat-A.txt &
VMSTAT=$!
TOTAL=100000 VUS=20 DURATION=3m deploy/scripts/gate4a-load.sh
kill $VMSTAT
kubectl -n ticketbottle logs job/k6-load > /tmp/k6-A.log
kubectl -n ticketbottle get pod -l job-name=k6-load \
  -o jsonpath='{.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}{"\n"}'
```

Keep the two timestamps printed last: they bound the run.

- [ ] **Step 3: Run B, straight after**

Repeat Step 2 with `-B` in place of `-A` in each file name.

- [ ] **Step 4: Decompose both**

For each run, with FROM the job's start and TO its finish plus 30s:

```bash
deploy/scripts/saga-histories.sh <FROM> <TO> /tmp/h-A.txt
python3 deploy/loadtest/saga_latency.py /tmp/h-A.txt /tmp/k6-A.log
```

Read the `vmstat` lines whose timestamps fall in the burst: `id` (idle CPU) and
`r` (runnable processes; above 2 means work is waiting for one of the 2 vCPUs).

- [ ] **Step 5: Cross-check the SLO instrument**

For each run, the gateway's count of checkouts over 2s must match the client's
within a few requests:

```bash
q() { curl -s localhost:9090/api/v1/query --data-urlencode "query=$1" --data-urlencode "time=<TO>"; }
q 'sum(increase(tb_grpc_request_duration_seconds_count{service="app-gateway",method="POST /api/orders"}[4m]))'
q 'sum(increase(tb_grpc_request_duration_seconds_bucket{service="app-gateway",method="POST /api/orders",le="2.0"}[4m]))'
```

The difference against `client N of M` in the tool's `all` line. `increase`
extrapolates to the window's edges, so expect a fraction, not an integer.

- [ ] **Step 6: Record**

Fill the run A and run B rows of the results table below.

---

### Task 4: Burst size (H2, H3, H4)

The burst is one admission tick, so its size is `QUEUE_DEFAULT_RELEASE_RATE`.
Run B is the size-20 point. This task adds 10 and 40 on a warm system.

- [ ] **Step 1: Set the release rate to N and roll the waiting room**

```bash
N=40
kubectl -n ticketbottle patch cm waitroom-config --type merge -p "{\"data\":{\"QUEUE_DEFAULT_RELEASE_RATE\":\"$N\"}}"
kubectl -n ticketbottle rollout restart deploy/waitroom-service
kubectl -n ticketbottle rollout status deploy/waitroom-service
```

`QUEUE_DEFAULT_MAX_CONCURRENT` is 100, above every N here, so it does not bind.

- [ ] **Step 2: Run and decompose**

```bash
ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=accept-new ec2-user@$IP 'vmstat -t 1' > /tmp/vmstat-$N.txt &
VMSTAT=$!
TOTAL=100000 VUS=$N DURATION=3m deploy/scripts/gate4a-load.sh
kill $VMSTAT
kubectl -n ticketbottle logs job/k6-load > /tmp/k6-$N.log
kubectl -n ticketbottle get pod -l job-name=k6-load \
  -o jsonpath='{.items[0].status.startTime} {.items[0].status.containerStatuses[0].state.terminated.finishedAt}{"\n"}'
deploy/scripts/saga-histories.sh <FROM> <TO> /tmp/h-$N.txt
python3 deploy/loadtest/saga_latency.py /tmp/h-$N.txt /tmp/k6-$N.log
```

FROM and TO as in Task 3 Step 4.

- [ ] **Step 3: Repeat for N=10**

- [ ] **Step 4: Restore the release rate**

Step 1 with `N=10`. The next `make -C deploy k3s-deploy` would also restore it
from the chart.

- [ ] **Step 5: Record**

Fill the N=10 and N=40 rows below.

---

### Task 5: Record the result

- [ ] **Step 1:** Fill *Results*, then write *What this says* against the
  outcome table in *What the evidence does not say*.
- [ ] **Step 2:** Rewrite the root `CLAUDE.md` Open row *What fails first as
  concurrent checkouts rise into the thousands?* with the finding and a pointer
  here. If the finding settles a narrower question, give that question its own
  row.
- [ ] **Step 3:** Name the architect's call the outcome raises: a bigger testbed
  for the ramp, a poller experiment, or whether the payment call belongs inside
  the 2s budget. It is theirs to make, not this plan's.
- [ ] **Step 4:** Set this plan's status line to COMPLETE, then commit:

```bash
git add docs/plans/2026-09-27-checkout-latency-decomposition.md CLAUDE.md
git commit -m "docs: record where a checkout's time goes, and what slows the opening burst"
```

## Results

| Run | Burst | Idle before | Burst over 2s | First task wait p50 / max | Burst `vmstat` idle / r | Steady over 2s (client) | Payment ran p90, burst / steady | Gateway vs client over 2s |
|---|---|---|---|---|---|---|---|---|
| 06:43Z | 20 | ~10 min | 20 of 20 (saga) | 1.01s / 5.28s | not captured | not captured | 1.14s / 0.44s | 30 of 287 / not captured |
| A | 20 | ≥10 min | | | | | | |
| B | 20 | none | | | | | | |
| N=10 | 10 | none | | | | | | |
| N=40 | 40 | none | | | | | | |

## What this says

Not yet written.
