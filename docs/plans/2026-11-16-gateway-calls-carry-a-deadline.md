# The gateway's gRPC calls carry a deadline

**Status: planned 2026-11-16; not started. The deadline values are the architect's call
(Task 1).** Not in [0031](../decisions/0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md)'s
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

- [ ] Locally, with event-svc's `docker-compose.dev.yml`: a call with a 1s deadline to a
  handler that sleeps 3s. Record whether the handler runs to the end, and whether a call
  queued behind a busy Prisma pool still starts after its deadline. That decides C.
- [ ] Read the gateway's `tb_grpc_request_duration_seconds` p99 per method from the last
  `k3s-load` run if its Prometheus data is still there, else the next one; a read's
  deadline sits well above its p99 under load.
- [ ] Put the values and A/B/C to the architect; record the call as 0037.

## Task 2: Build the interceptor (TDD)

- [ ] Test first, in the style of `bounded-channel.spec.ts`: a slow handler, a client
  through `ClientGrpcProxy` with the interceptor; the call fails `DEADLINE_EXCEEDED` at
  the bound, and the gateway answers 504 through `GRPC_TO_HTTP`. A `CreateOrder` call is
  not cut before its own deadline.
- [ ] `services/api-gateway/src/shared/grpc/`, wired in every `*.module.ts` that has
  `boundedChannelFactory()`; `src/CLAUDE.md`'s client rule gains it.
- [ ] Whatever Task 1 chose for B or C, test-first in the service that owns it.

## Task 3: On k3s

- [ ] Rerun the gateway plan's Task 3 Step 3, the forced stall, on the new build. Pass:
  event-service's `tb_grpc_in_flight` stays near the gateway's, and the last failure lands
  within about 6s of the lift. A box with steal is named as such in the result.
- [ ] `k3s-gate2` green. Accept 0037 with the run; move the register row to *Decided*.
