# Rollout drain measurement — plan

**Status: RUN 2026-09-25, without `preStop`.** Every target failed, so the decision
rule calls for a delay. The run also found two defects this plan did not look for;
see *Results*. The architect's calls are
[0015](../decisions/0015-app-pods-sleep-before-sigterm.md) and
[0016](../decisions/0016-the-gateway-drains-for-up-to-65s.md). The work and the
re-run are [the fixes plan](2026-09-27-rollout-drain-fixes.md), **COMPLETE 2026-09-27**:
ten rollouts with the fixes, zero failures ([its results](2026-09-27-rollout-drain-fixes.md#results)).

**Goal:** Measure whether a rollout refuses requests now that the NestJS services
exit on SIGTERM (`7cd6774`), and whether a `preStop` delay removes the refusals.

## Why this is open

Until `7cd6774`, a terminating gateway, user, event or payment pod went on serving
until the kubelet's SIGKILL, 30s later. That was a bug, but it hid a race.

Kubernetes sends SIGTERM and removes the pod from its Service's endpoints **at the
same time, not in order**. Until kube-proxy has applied the removal, a new
connection can still be routed to the pod — which now closes its port on SIGTERM.
The usual remedy is a `preStop` sleep, so the pod keeps serving while routing
catches up. It costs that many seconds on every pod stop, and it would go in
`deploy/helm/ticketbottle/templates/apps/_appservice.tpl`, shared by every app.

Whether the window ever produces a failed request on k3s is unmeasured.

## Why the purchase harness cannot answer it

- `deploy/loadtest/purchase.js` reaches event-svc and payment-svc only through
  order-svc's Temporal activities, which retry. A refused reconnect there is
  absorbed and never reaches the client.
- Sign-in and sign-up are throttled per client IP
  ([0013](../decisions/0013-the-gateway-throttles-sign-in-not-purchase.md)), and a
  k6 pod is one IP, so a user-svc path would measure the limiter.

`GET /api/events/:id` avoids both: the gateway calls event-svc directly, and a
failure surfaces as a 503. It needs a JWT, which the probe mints the way
`purchase.js` does — `sign()` with the mounted `ACCESS_SECRET`.

## Method

**Probe.** A k6 script, `deploy/loadtest/rollout-probe.js`: constant arrival rate,
about 50 req/s of `GET /api/events/:id` against one seeded event, for the length of
a target's run. It runs in-cluster as a Job shaped like `deploy/loadtest/job.yaml`,
so it reaches `app-gateway` through its ClusterIP — the kube-proxy path being
measured. It records every non-200 with its timestamp and status; status 0 is a
connection-level failure.

**Positive control first.** k3s runs one replica of each app (`replicas: {}` in
`values.yaml`). `kubectl -n ticketbottle delete pod -l app=<target>` therefore
leaves no ready endpoint until the replacement passes readiness, and the probe
**must** record failures. If it records none, it is blind: fix it before trusting
any zero below.

**Targets**, one at a time, five rollouts each, 60s apart, under the probe:

| Target | What it exercises |
|---|---|
| `app-gateway` | The HTTP edge: k6 → ClusterIP → a gateway that closes on SIGTERM |
| `event-service` | gRPC behind the gateway: the gateway's HTTP/2 connection gets GOAWAY and reconnects through the ClusterIP |

Each rollout is `kubectl -n ticketbottle rollout restart deploy/<target>`, then
`rollout status`. The default RollingUpdate brings the new pod to Ready before the
old one gets SIGTERM, so a failure isolates the drain race, not missing capacity.

**Cross-check.** The gateway records a refused downstream reconnect as `UNAVAILABLE`:
`tb_grpc_requests_total{service="app-gateway",code="UNAVAILABLE"}` over each run
should match the probe's 503 count.

## The time budget a delay must fit

`terminationGracePeriodSeconds` is unset in the chart, so every pod gets 30s. The
gateway sets no deadline on the checkout call, and order-svc lets `CreateOrder` run
to `ORDER_CREATE_TIMEOUT`, 60s (`values.yaml`, `order.createTimeout`). A checkout
in flight on a terminating gateway pod is already killed at 30s, and a `preStop`
sleep spends part of those 30. A delay therefore comes with a grace period of at
least *sleep + 60s* on `app-gateway`, or with the stated cost that a slow checkout
is cut during a gateway rollout.

## Decision rule

| Result without `preStop` | Then |
|---|---|
| Control red; zero failures across all ten rollouts | No delay on k3s. Record the counts below. The question stays open for EKS, where the ALB deregisters targets on its own, slower clock. |
| Any failure | Add a `preStop` sleep, re-run the same matrix, expect zero. Its length, per-service or chart-wide, and the grace period above are the architect's call; draft the decision record in the same change. |

## Out of scope

- **EKS.** The cluster is ephemeral and every run costs money; the ALB target
  deregistration delay is a separate setting. Measure there when k3s says a delay is
  needed, or before EKS carries real traffic.
- **The Go services' SIGTERM path.** `7cd6774` did not touch it, and this plan does
  not examine it.

## Results

Run 2026-09-25 on k3s, build `sha-39e4300` (includes `7cd6774`), with
`deploy/scripts/rollout-drain.sh`, `deploy/loadtest/rollout-probe.js` at 50 req/s.

**The probe path also calls user-svc.** The access guard runs user-svc `FindOne`
on the token's `sub` on every request, so the probe's user must exist. The runner
seeds it, and refuses to run if the probe fails before any pod is stopped. Its
first control run returned 401 for every request, and without that check it
would have passed as red.

**Positive controls: both red.**

| Target | Failures | Window |
|---|---|---|
| `app-gateway` | 190, all status 0 (`dial: connection refused`) | 6s, until the replacement was Ready |
| `event-service` | 3437, all 503 | 68s+, still failing 59s after the replacement was Ready |

**Rollouts, no `preStop`: every target fails.**

| Target | Rollouts with failures | Failures | Per affected rollout |
|---|---|---|---|
| `app-gateway` | 5 of 5 | 126 of 27,713 | 18–31 refused connects, all within ~1s of the old pod's SIGTERM |
| `event-service` | 4 of 5 | 9,718 of 27,719 (35%) | 40.3s, 40.4s, 40.5s and 72.9s of 503s on every request |

The gateway's `tb_grpc_requests_total{code="UNAVAILABLE"}` matched the probe's
503 count exactly in both event-service runs (3437, 9718).

**Finding 1: one refused reconnect costs ~40s of event reads.** In a separate
event-service rollout, raw TCP connects from the gateway pod to
`event-service:50053`, every 250ms, failed three times and then succeeded from
1.8s after the stop onward. The gateway's gRPC calls failed for 40.7s. The
network race lasts about 2s; the gateway's grpc-js channel
(`No connection established. Last error: ECONNREFUSED`) stretches it to about 40s.
Which timer produces a steady ~40s has not been found. The same amplification
applies to any refused reconnect (crash, OOM, eviction), not only to rollouts.

**Finding 2: a gateway under keep-alive traffic never drains.** After SIGTERM
the gateway closes its listener, but 17 established keep-alive connections kept
receiving and serving requests until SIGKILL at 30s. `server.close()` waits for
connections that a busy client never lets go idle, so `app.close()` does not
return. Every gateway rollout took 40–42s to remove the old pod. Anything in
flight at the SIGKILL is cut. The probe recorded no failures at that moment,
probably because k6 retries an idempotent GET on a reused connection; a
`POST /api/orders` would not be retried. `7cd6774` measured its 1s exit with no
client connected.

**Decision rule outcome:** any failure, so add a `preStop` sleep and re-run this
matrix. These were put to the architect, and settled on 2026-09-27 in 0015 and 0016:

- the sleep's length, and whether it goes chart-wide or per service;
- the gateway's grace period (see *The time budget*): because of Finding 2, every
  gateway stop already runs to the 30s limit;
- whether Findings 1 and 2 are fixed in the services. A `preStop` sleep hides the
  rollout trigger for Finding 1, but not a crash, and does nothing for Finding 2.
