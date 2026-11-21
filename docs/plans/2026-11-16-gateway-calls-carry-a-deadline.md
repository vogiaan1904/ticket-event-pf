# The gateway's gRPC calls carry a deadline

**Status: COMPLETE 2026-11-18.** A built in `6ff19a0`, 5s and 65s for `CreateOrder`, accepted as [0037](../decisions/0037-the-gateway-bounds-every-call-it-makes.md#outcome). Task 3 met half its pass: the backend's backlog shrank, but recovery took 12.6s after the lift; that tail is an open question in the root register. Not in [0031](../decisions/0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md)'s
ranking; where it slots is the architect's call.

**Question it serves:** the open row in the root `CLAUDE.md` register, *What bounds the
gateway's calls to a backend that answers slowly?*

**Spec:** none. Found by [0035](../decisions/0035-the-gateway-bounds-a-backend-reconnect.md)'s
k3s run, 2026-11-16 ([results](2026-10-12-gateway-recovery-after-a-backend-crash.md#results)).

## Context

The gateway sets no deadline on any gRPC call (`services/api-gateway/src`, checked
2026-11-16). After event-service came back from a forced stall, the gateway recovered
16s after the lift, not the bound's 6s: an open-loop probe at 50 a second, a box losing
up to 0.9 of 2 vCPUs to steal, and event-service holding up to 476 calls in flight while
the gateway had 200. A call whose HTTP client gave up at 5s still ran to the end.

Checked in the code, 2026-11-16:
- grpc-js 1.13.4 takes `interceptors` in the client options, and Nest's
  `ClientGrpcProxy` passes `channelOptions` through to them, the path `boundedChannelFactory`
  already uses. One interceptor can set every call's deadline.
- Nest's client cancels the gRPC call when its observable is unsubscribed; nothing
  unsubscribes when the HTTP client disconnects, since `firstValueFrom` waits.
- order-svc bounds `CreateOrder` itself at `order.createTimeout`, 60s, then cancels the
  saga (`services/order-svc/internal/order/service/order.go`). A gateway deadline
  propagates as the server's `ctx` deadline, so one below 60s would cancel sagas sooner.
- How a TS backend (event, user, payment) treats a call whose deadline passed is not
  known: grpc-js ends the call, but nothing tells the handler, and Prisma takes no
  abort signal. The Go backends cancel their `ctx`.

## Options

**A. A deadline on every call, by method class** (recommended). One interceptor beside
`BoundedChannel`, wired the same way. A read gets a short deadline; `CreateOrder` gets one
above order-svc's own 60s, so it never cancels a saga order-svc would have finished.
Bounds what the gateway holds and answers 504 instead of hanging. Does not stop a TS
backend's work, so it alone may not shorten a backlog like the 2026-11-16 one.

**B. A, and cancel the call when the HTTP client disconnects.** Frees a backend's Go
handler the moment the buyer leaves. Costs request-close plumbing in every controller or
an interceptor that sees the Express request.

**C. A, and TS backends refuse a call already past its deadline when they start it.**
Sheds the queued work that made the backlog. Costs a check in three services, and only
helps if the queue is ahead of the handler, which Task 1 has to show.

## Task 1: Measure, then choose

- [x] Locally, with event-svc's `docker-compose.dev.yml`: a call with a 1s deadline to a
  handler that sleeps 3s. Record whether the handler runs to the end, and whether a call
  queued behind a busy Prisma pool still starts after its deadline. That decides C.
- [ ] *Not done: the box was unreachable, and the values were chosen without it.* Read the gateway's `tb_grpc_request_duration_seconds` p99 per method from the last
  `k3s-load` run if its Prometheus data is still there, else the next one; a read's
  deadline sits well above its p99 under load.
- [x] Put the values and A/B/C to the architect; record the call as 0037. → [0037](../decisions/0037-the-gateway-bounds-every-call-it-makes.md)

### Task 1 results

**2026-11-18, locally.** event-svc built at `c440c7d`, its compose Postgres, and a scratch
patch to `dist/` only: `findById` on an id `stall-*` runs `SELECT pg_sleep(n)` through
Prisma and logs when the handler starts and when the query ends. The client is grpc-js
with a 1s deadline.

```
### A: pool 10, 1 call, 3s handler, 1s deadline
CLIENT stall-0 DEADLINE_EXCEEDED at +1007ms
STALL stall-0 handler-start 1794973647667
STALL stall-0 query-done 1794973650682 took=3015ms
### B: pool 1, 4 calls, 2s each, 1s deadline
CLIENT stall-0..3 DEADLINE_EXCEEDED at +1009..1012ms
STALL stall-0..3 handler-start within 2ms of each other
STALL stall-0 query-done took=2009ms
STALL stall-1 query-done took=4011ms
STALL stall-2 query-done took=6018ms
STALL stall-3 query-done took=8025ms
```

- **A TS handler runs to the end after its deadline.** Neither grpc-js nor Nest tells it.
- **The queue is in Prisma's pool, not ahead of the handler.** All four handlers started
  at once; their queries ran in turn, the last 7s after its deadline. So C as written,
  a check when the handler starts, sheds nothing: every handler starts in time.
- The gateway's per-method p99 under load was not read: the box's API server refused the
  connection on 2026-11-18. A checkout's p99 stays under 2s at the k3s door (0029), and
  event-service's p99 was 2–5s under the forced stall
  ([results](2026-10-12-gateway-recovery-after-a-backend-crash.md#results)).

## Task 2: Build the interceptor (TDD)

- [x] Test first, in the style of `bounded-channel.spec.ts`: a slow handler, a client
  through `ClientGrpcProxy` with the interceptor; the call fails `DEADLINE_EXCEEDED` at
  the bound, and the gateway answers 504 through `GRPC_TO_HTTP`. A `CreateOrder` call is
  not cut before its own deadline.
- [x] `services/api-gateway/src/shared/grpc/`, wired in every `*.module.ts` that has
  `boundedChannelFactory()`; `src/CLAUDE.md`'s client rule gains it.
- [x] Whatever Task 1 chose for B or C, test-first in the service that owns it. Neither was chosen.

## Task 3: On k3s

- [x] Rerun the gateway plan's Task 3 Step 3, the forced stall, on the new build. Pass:
  event-service's `tb_grpc_in_flight` stays near the gateway's, and the last failure lands
  within about 6s of the lift. A box with steal is named as such in the result.
- [x] `k3s-gate2` green. Accept 0037 with the run; move the register row to *Decided*. → [0037](../decisions/0037-the-gateway-bounds-every-call-it-makes.md#outcome)

### Task 3 results

**2026-11-18, build `sha-6ff19a0`, k3s.** `k3s-gate2` passed first (`GATE 1 PASSED`), which
also warmed the gateway's channel. Then the plan's forced stall, unchanged: a node `DROP`
on new connections from the gateway to 50053, the event-service pod deleted under the
probe at 50 a second, and the drop lifted 30s after the replacement was Ready. A sampler
in the gateway pod read both workloads' `tb_grpc_in_flight` each second. The box had
almost no steal (0.5% after the run; 0.5 to 0.9 vCPU on 2026-11-16).

| | 2026-11-16, no deadline | 2026-11-18, 5s deadline |
|---|---|---|
| Delete → Ready | 10.1s | 16.1s |
| Failures | 1966: 1125 503s, 841 timeouts | 1983: 1325 503s, 658 timeouts |
| Bound fired | every ~5s | every 5–6s, 04:16:58 to 04:17:31 |
| Last failure after the lift | 16s | 12.6s |
| event-service in flight, first reading after the lift | 476 (gateway 200) | 53 (gateway 86) |

- **The backlog multiplier is gone.** event-service now holds fewer calls than the gateway,
  where it held 2.4 times as many.
- **Recovery is still not ~6s.** From the lift (04:17:35) to 04:17:47, every probe request
  timed out at the probe's own 5s; the gateway sat at the probe's 200-VU cap
  (`rollout-probe.js` `maxVUs`), and event-service's `/metrics` did not answer within
  900ms, so its event loop was saturated. Its counter shows every `FindOne` it received
  answered `OK`: it did all the work it was given.
- **The harness hides the deadline.** The probe times out at 5s, the same as the gateway's
  deadline, so it gives up first: the gateway recorded 31 `DEADLINE_EXCEEDED`, but the
  probe saw them as timeouts (status 0), never as 504s.
- **Not shown:** what kept a fresh event-service pod saturated for 12s after the lift.
  Candidates are the burst of calls released at once when the channel reached `READY`,
  and a cold pod (new Prisma pool, unwarmed JIT). The run cannot tell them apart.

Raw logs: `crash-s` in the session scratchpad (not kept).

### Follow-up: a stall with the pod left running

**2026-11-18, same build, k3s.** To tell a cold pod from the burst, the event-service pod
(running since 04:16) was left alone. Instead, every packet from the gateway to 50053 was
dropped for 44s (06:17:36 to 06:18:20), under the same probe at 50 a second.

| | Cold pod (above) | Warm pod |
|---|---|---|
| Last failure after the lift | 12.6s | 30s |
| event-service in flight, peak after the lift | 53 (gateway 86) | 490 (gateway 200) |
| Gateway `INTERNAL` | 0 | 200 |
| Bound fired | every 5–6s | never |

- **A burst alone saturates a warm pod.** The gateway's requests, and the cancels it sent
  at each 5s deadline, sat in the stalled TCP connection. When it resumed, event-service
  received them together and ran each to the end, as a TS handler does
  (*Task 1 results*). It logged 1861 `findOne` calls from 06:18:30 to 06:18:59, and its
  in-flight count fell from 490 to 23 over 10s. Here C', skipping a query whose call has
  ended, would have shed most of that work.
- **A silent connection is not detected.** The channel stayed `READY` for the whole stall,
  so the bound never fired and no reconnect was tried. No gRPC keepalive is configured,
  so the gateway waited on TCP's retransmission backoff: traffic resumed about 20s after
  the lift.
- **A dropped session reads as our bug.** At 06:18:41 the HTTP/2 session closed with code
  2, and grpc-js failed its 200 in-flight calls with `13 INTERNAL: Received RST_STREAM
  with code 2`. Under the alerting policy, a network fault would page as a bug. event-service
  logged no error, so the session was not closed by a handler; which side closed it is not
  shown.

