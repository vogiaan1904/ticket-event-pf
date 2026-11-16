# The gateway's recovery after a backend crash

**Status: Step 2 RUN 2026-11-14 on `sha-a5842c2`; Step 3 RUN 2026-11-15 on `sha-83537aa`,
re-planned around the idle channel (see *Results*). The 133s did not recur. The fix, a
bound on the reconnect, is chosen → [0035](../decisions/0035-the-gateway-bounds-a-backend-reconnect.md);
built, its k3s check (Task 3, with a forced stall added 2026-11-16) open.** Ranked first by
[0031](../decisions/0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md).

**Question it serves:** the open row in the root `CLAUDE.md` register, *Why did one
refused reconnect cost the gateway ~40s on 2026-09-25, and what bounds it when a
backend crashes?*

**Goal:** on k3s, find what holds the gateway's channel to event-service failing
after the backend is back, by deleting event-service's pod under load with and
without a grpc-js trace. The run names the cause or shows it cannot be reached on
today's build. Choosing a fix comes after it.

**Spec:** none. Derived from the 2026-09-25 and 2026-09-27 results:
`docs/plans/2026-09-24-rollout-drain-measurement.md`,
`docs/plans/2026-09-27-rollout-drain-fixes.md#results`.

## What is known

**From k3s.** The rollouts on both days ran the same build, `sha-39e4300`. The
2026-09-27 control ran on `sha-e3f0b51`, which added 0015's 5s `preStop` sleep.

| Day | Gateway | Pod deleted (control) | Rollouts |
|---|---|---|---|
| 2026-09-25 | running before the run | 68s+ of 503s, still failing 59s after the replacement was Ready | 40.3, 40.4, 40.5 and 72.9s in 4 of 5 |
| 2026-09-27 | restarted by `kubectl set env` for the trace | 437 failures at 50 a second, about 9s | about 1s in 3 of 3 |

Every failure carried `No connection established. Last error: ECONNREFUSED`.

**Proven locally, 2026-10-12, with the gateway's grpc-js 1.13.4.** One reconnect
that never completes keeps the channel failing for as long as it hangs, and every
call reports the stale `ECONNREFUSED`. The repro is in the appendix. It cuts the
backend, refuses for 1.8s as measured on 2026-09-25, then accepts the first
reconnect and never answers it:

| Run | Result |
|---|---|
| no stall | recovered 2.8s after the crash |
| first reconnect stalled, one address | still failing at 30.0s, the end of the run |
| first reconnect stalled, `localhost` (two addresses) | recovered 2.5s after: pick_first tried the other address 250ms later |

Three facts make that possible:
- **No limit on an attempt.** `transport.js` calls `net.connect` with no timeout
  and waits for the server's HTTP/2 settings with no timer. grpc-js 1.14.6, the
  latest, has no such option either.
- **Sticky failure.** Once every address has failed, pick_first answers every call
  at once with the last error, while a new attempt is still in flight
  (`load-balancer-pick-first.js`, `calculateAndReportNewState`).
- **One address.** `event-service:50053` is a ClusterIP Service, so there is no
  other address to try (`deploy/helm/ticketbottle/templates/apps/config.yaml`).

**Ruled out in the code.** The backoff resets on every `READY` (`subchannel.js`,
`stopBackoff`), so an earlier outage does not leave it long. A reconnect waits on
the subchannel's backoff, not on DNS re-resolution and its 30s minimum. A channel
revived after its idle timeout recovered like a fresh one locally, though the run
did not trace that it went idle.

**Not known:** what stalls an attempt on k3s, and what ends it at about 40s.

## What the run tells apart

| Hypothesis | The trace between the first `ECONNREFUSED` and `READY` |
|---|---|
| H1. A SYN goes to the gone pod and is dropped | one attempt `CONNECTING` for the whole window, with no `Established TCP connection` |
| H2. TCP connects, HTTP/2 never answers | `Established TCP connection`, then nothing until the window ends |
| H3. Attempts keep failing fast | `CONNECTING -> TRANSIENT_FAILURE` repeated at backoff intervals: the cause is routing, not the client |
| Not reached | recovery within a backoff step of the replacement turning Ready, as on 2026-09-27 |

**Prediction, written 2026-10-12:** the untraced runs reproduce the long failure,
and the traced runs show H1.

## Global constraints

- The k3s box and EKS never run at once.
- **Wait for an idle box.** A box stopped more than 24 hours after a heavy session
  spends its first ~15 minutes deleting expired Temporal workflows.
- `kubectl set env` restarts the gateway. The 2026-09-27 trace did the same and the
  failure did not appear, so the untraced runs go first, on the gateway as it boots.
- Remove the trace afterwards (`GRPC_TRACE-`). An env var set by hand outlives the
  run and blocks the next chart deploy of that field.
- One deployed build for the session, deployed by its `sha-` tag.

## Task 1: Run it on k3s

The architect runs it and pastes the output. About 50 minutes; 15 more with Step 4.

Every probe runs 180s. At the script's default 90s, the 2026-09-25 control was still
failing when it ended, so a run that short cannot show what ends the failure.

- [ ] **Step 1: Start the box and wait for idle.**

```bash
aws eks list-clusters --region us-east-1        # "clusters": []
make -C deploy start-ec2-k3s                    # run the printed tunnel in its own terminal
make -C deploy k3s-kubeconfig && export KUBECONFIG=/tmp/k3s.yaml
kubectl -n ticketbottle exec deploy/temporal -- temporal workflow count --query "CloseTime < \"$(date -u +%Y-%m-%d)T00:00:00Z\""
kubectl top pods -n ticketbottle --sort-by=cpu | head -4    # postgres-0 and temporal under ~200m
kubectl -n ticketbottle get deploy app-gateway event-service \
  -o jsonpath='{range .items[*]}{.metadata.name} {.spec.template.spec.containers[0].image}{"\n"}{end}'
```

- [ ] **Step 2: Three untraced crashes.** For each, the failures and when the
  replacement turned Ready:

```bash
for i in 1 2 3; do
  mkdir -p /tmp/crash-u$i && DURATION=180s OUT=/tmp/crash-u$i deploy/scripts/rollout-drain.sh control event-service
  kubectl -n ticketbottle get pod -l app=event-service \
    -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].lastTransitionTime}{"\n"}'
  sleep 60
done
for i in 1 2 3; do grep ready /tmp/crash-u$i/timeline.log; tail -1 /tmp/crash-u$i/failures.jsonl; done
```

The last loop prints, for each run, when the replacement was seen Ready and the last
failure. A gap over 10s in any run reproduces the failure.

- [ ] **Step 3: Three traced crashes.** A gateway just restarted times out at 50 req/s,
  and the script voids a probe that fails before the delete, so it warms up first.

```bash
kubectl -n ticketbottle set env deploy/app-gateway GRPC_TRACE=subchannel,transport,pick_first GRPC_VERBOSITY=DEBUG
kubectl -n ticketbottle rollout status deploy/app-gateway
sleep 60
for i in 1 2 3; do
  T0=$(date -u +%FT%TZ)
  mkdir -p /tmp/crash-t$i && DURATION=180s OUT=/tmp/crash-t$i deploy/scripts/rollout-drain.sh control event-service
  kubectl -n ticketbottle get pod -l app=event-service \
    -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].lastTransitionTime}{"\n"}'
  kubectl -n ticketbottle logs deploy/app-gateway --since-time=$T0 > /tmp/crash-t$i-gateway.log
  sleep 60
done
```

- [ ] **Step 4: Only if Step 2 did not reproduce it, three traced crashes without the
  drain.** Today's build sleeps 5s before it stops a deleted pod (0015); the
  2026-09-25 build stopped it at once. `GRACE=1` is the closest to that.

```bash
for i in 1 2 3; do
  T0=$(date -u +%FT%TZ)
  mkdir -p /tmp/crash-g$i && GRACE=1 DURATION=180s OUT=/tmp/crash-g$i deploy/scripts/rollout-drain.sh control event-service
  kubectl -n ticketbottle get pod -l app=event-service \
    -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].lastTransitionTime}{"\n"}'
  kubectl -n ticketbottle logs deploy/app-gateway --since-time=$T0 > /tmp/crash-g$i-gateway.log
  sleep 60
done
```

- [ ] **Step 5: Remove the trace, stop the box.**

```bash
kubectl -n ticketbottle set env deploy/app-gateway GRPC_TRACE- GRPC_VERBOSITY-
kubectl -n ticketbottle get deploy app-gateway -o jsonpath='{.spec.template.spec.containers[0].env}'; echo   # no GRPC_
make -C deploy stop-ec2-k3s
```

Each run's failures stay in `/tmp/crash-*/failures.jsonl`, and the traced gateway logs in
`/tmp/crash-[tg]*-gateway.log`, for the analysis.

## Task 2: Record

- [ ] Fill *Results*: for each run, the seconds failing after the replacement turned
  Ready, and, for the traced ones, the attempts from the trace.
- [ ] Name the hypothesis the trace shows, or that the failure was not reached, for
  each stop that ran.
- [ ] Put the fix options to the architect, with what each costs. The candidates
  so far:
  - the gateway bounds an attempt itself and recreates the channel;
  - a headless Service with `round_robin`, so a stalled pod address is one of several;
  - kernel SYN retries shortened in the gateway's pod, if H1.

## Task 3: The bound, on k3s

Built 2026-11-15: `BoundedChannel` in `services/api-gateway/src/shared/grpc/`, on every
gateway client. Its test is the local repro: a plain channel still fails at 10s; the
bounded one recovers 0.3s after the backend returns. A channel that serves then idles is
never replaced; mutating the timer's reset turns that test red.

The stall reproduced once in five crashes, so three crashes alone likely end with the
bound never firing: they show no regression, not the bound. Step 3 forces the stall.
The architect runs it and pastes the output. About 75 minutes, 31 of them idle.

- [ ] **Step 1: Start, deploy, check the box enforces NetworkPolicy.** Task 1 Step 1's
  commands, then:

```bash
REF=sha-<pushed HEAD, 7 chars> make -C deploy k3s-deploy
kubectl -n ticketbottle get deploy app-gateway -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
cat > /tmp/stall.yaml <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: stall-gateway-to-event, namespace: ticketbottle }
spec:
  podSelector: { matchLabels: { app: event-service } }
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector: {}
          podSelector: { matchExpressions: [{ key: app, operator: NotIn, values: [app-gateway] }] }
EOF
probe() { kubectl -n ticketbottle exec deploy/app-gateway -- node -e \
  "require('net').connect(50053,'event-service').on('connect',()=>{console.log('open');process.exit(0)}).setTimeout(3000,()=>{console.log('dropped');process.exit(1)})"; }
probe                                            # open
kubectl apply -f /tmp/stall.yaml && sleep 3 && probe   # dropped
kubectl -n ticketbottle delete netpol stall-gateway-to-event && sleep 3 && probe   # open
```

`dropped` must print in the middle; if it prints `open`, the box does not enforce
NetworkPolicy and Step 3 cannot run.

- [ ] **Step 2: Wait 31 minutes with no traffic, then three untraced crashes.** Task 1
  Step 2's two loops, unchanged. Pass: in each run the last 503 lands within about 6s
  of `event-service ready` (5s bound plus one connect), against u1's 133s.

```bash
kubectl -n ticketbottle logs deploy/app-gateway --since=15m | grep -c 'replacing the channel'
```

- [ ] **Step 3: Forced stall, right after Step 2** (the channel must be busy, or the
  script's warm-up probe wakes it into the policy and voids the run). The policy keeps
  the live connection and drops new ones, so deleting the pod leaves the gateway's
  reconnect hanging, the shape of H1. It is lifted 30s after the replacement is Ready.

```bash
kubectl apply -f /tmp/stall.yaml
mkdir -p /tmp/crash-s && (DURATION=120s OUT=/tmp/crash-s deploy/scripts/rollout-drain.sh control event-service > /tmp/crash-s/run.log 2>&1 &)
until grep -q 'event-service ready' /tmp/crash-s/timeline.log 2>/dev/null; do sleep 1; done
sleep 30; kubectl -n ticketbottle delete netpol stall-gateway-to-event; date -u +%FT%TZ   # lifted at (BSD date: whole seconds)
until grep -q 'gone' /tmp/crash-s/timeline.log; do sleep 5; done; sleep 90
cat /tmp/crash-s/run.log; tail -1 /tmp/crash-s/failures.jsonl
kubectl -n ticketbottle logs deploy/app-gateway --since=5m | grep 'replacing the channel'
kubectl get netpol -A                            # No resources found
```

Pass: `replacing the channel` warnings about every 5s while the policy held, and the
last 503 within about 6s of *lifted at*. Red: 503s running on to the probe's end,
which is what an unbounded connect does until the kernel gives up, about 127s.

- [ ] **Step 4:** `make -C deploy k3s-gate2` still green. Leave the box up for the
  late-payment plan's ticket 5 run, then `make -C deploy stop-ec2-k3s`.
- [ ] Record the three crashes and the stall under *Results*; accept 0035 with them.

## Results

**2026-11-14, Step 2, build `sha-a5842c2`.** The box was idle (no expired Temporal
workflows; top pod 37m CPU); the gateway had run for 162 minutes with no traffic and
no trace env. Each run probed for 180s at 50 req/s; `Ready` is the timeline's
`event-service ready`.

| Run | Delete → Ready | 503s | First, last 503 after delete | Last 503 after Ready |
|---|---|---|---|---|
| u1 | 10.7s | 6850, no gap over 1s | +7.1s, +143.7s | **133.0s** |
| u2 | 15.0s | 543 | +7.0s, +17.0s | 2.1s |
| u3 | 9.4s | 141 | +7.1s, +9.7s | 0.3s |

**Reproduced, once in three, on the first crash.** u1 failed every request for 136.6s,
133s of them with the replacement Ready. Every failure was a gateway 503 (`UNAVAILABLE`).
The first 503 comes 7s after the delete in every run: 0015's 5s `preStop` sleep, then
the stop.

**What sets u1 apart.** The gateway sets no channel options
(`services/api-gateway/src`, grpc-js 1.13.4), so its channel goes idle after 30
minutes without a call. Before u1 it had been idle for hours; the probe woke it 15s or
more before the delete, the script's check that the probe passes before anything stops. Before u2 and u3 it had been busy 60s earlier. The 2026-09-25 runs
also began on a gateway already running; the 2026-09-27 traced runs began on one just
restarted, and recovered.

**Not shown, and why Step 3 waits.** No trace ran, so H1–H3 are not told apart. 133s
is close to Linux's default give-up on an unanswered connect, about 127s
(`tcp_syn_retries=6`), plus one backoff step, which fits H1; that is a lead, not a
finding. Step 3 as written restarts the gateway and crashes 60s later, so its channel
is never idle and it would likely recover, as on 2026-09-27. The re-planned run traces
a crash on a channel left idle for 31 minutes or more.

Raw logs: `/tmp/crash-u{1,2,3}` on the machine that ran it (not kept).

## Appendix: the local repro

Run from the repository root: `node repro.js` for the control, `STALL=1 node repro.js`
for a stalled first reconnect.

```js
// Cut the backend behind a proxy, refuse for 1.8s, then come back. With STALL=1 the
// first reconnect after that is accepted and never answered.
const grpc = require(process.cwd() + '/services/api-gateway/node_modules/@grpc/grpc-js');
const net = require('net');
const STALL = process.env.STALL === '1';
const id = (b) => b;
const def = { Echo: { path: '/t.T/Echo', requestStream: false, responseStream: false,
  requestSerialize: id, requestDeserialize: id, responseSerialize: id, responseDeserialize: id } };
const server = new grpc.Server();
server.addService(def, { Echo: (call, cb) => cb(null, Buffer.from('ok')) });

let listener = null, socks = new Set(), stalled = false, crashed = false;
function listen(port) {
  listener = net.createServer((c) => {
    socks.add(c); c.on('close', () => socks.delete(c)); c.on('error', () => {});
    if (STALL && crashed && !stalled) { stalled = true; return; }
    const up = net.connect(50999, '127.0.0.1'); up.on('error', () => c.destroy());
    c.pipe(up).pipe(c);
  }).listen(port, '127.0.0.1');
}
server.bindAsync('127.0.0.1:50999', grpc.ServerCredentials.createInsecure(), () => {
  listen(50998);
  const Client = grpc.makeGenericClientConstructor(def, 'T');
  const client = new Client('127.0.0.1:50998', grpc.credentials.createInsecure());
  const t0 = Date.now(); let crashAt = 0, lastFail = null, firstOkAfter = null, msg = '';
  const tick = setInterval(() => {
    client.Echo(Buffer.from('x'), { deadline: Date.now() + 1000 }, (err) => {
      const now = (Date.now() - t0) / 1000;
      if (err) { lastFail = now; msg = err.details; }
      else if (crashAt && firstOkAfter === null && now > crashAt + 0.5) firstOkAfter = now;
    });
  }, 50);
  setTimeout(() => {
    crashAt = (Date.now() - t0) / 1000; crashed = true;
    listener.close(); for (const s of socks) s.destroy();
    setTimeout(() => listen(50998), 1800);
  }, 2000);
  setTimeout(() => {
    clearInterval(tick);
    console.log(firstOkAfter === null
      ? `still failing at ${(lastFail - crashAt).toFixed(1)}s; last error "${msg}"`
      : `recovered ${(firstOkAfter - crashAt).toFixed(1)}s after the crash`);
    process.exit(0);
  }, 32000);
});
```

**2026-11-15, Step 3 re-planned, build `sha-83537aa`.** Each crash followed 32 minutes
with no gateway call to event-service, with a grpc-js trace (`subchannel,transport,
pick_first,channel`) and, on the node, every SYN and RST to `:50053` and the gateway
socket's TCP state each second. t1 ran on a gateway restarted for the trace, its channel
never connected before the probe; t2 on the same gateway, its channel connected in t1 and
since gone idle.

| Run | Delete → Ready | Failures | Channel `READY` after Ready | Last failure after Ready |
|---|---|---|---|---|
| t1 | 11.2s | 313, all 503 | — (trace rotated away) | 2.4s |
| t2 | 21.4s | 603 503s; 1827 probe timeouts at 5s | 3.4s | 91s, all timeouts |

**Not reached in either run.** In t1 the first SYN after the stop went to the ClusterIP
and was refused, with no endpoint Ready; the next, 3s later, was rewritten to the new pod
and connected. No SYN went to the gone pod's IP. In t2 the trace shows the old pod's
`GOAWAY`, `ECONNREFUSED` at each backoff step, then `READY` 3.4s after the replacement:
H3's shape, which is routing, and ends when the endpoint does.

**t2's timeouts are the instruments.** They began 7s before the delete, on the old pod.
event-service's own p99 was 2–5s with every call `OK`, CPU-throttled 36–56% of periods
against its 500m limit; the node ran 1.9 of 2 cores, about 0.9 outside pods, carrying the
per-call trace through the log pipeline and a live log stream. t1, without the stream, ran
1.7 cores and event-service's p99 stayed under 1s. A trace that logs every call cannot
watch a crash at 50 req/s on this box.

**Where that leaves it.** One crash in five since 2026-11-14 failed past a backoff step,
and the condition that separates it is not known: an idle channel, woken 15s before the
crash, recovered twice. The mechanism the local repro proved stands without a trigger: an
attempt grpc-js never bounds holds every call failing for as long as it hangs.

Raw logs: `crash-t{1,2}` in the session's scratch directory (not kept).
