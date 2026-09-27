# 0016 — A stopping gateway drains for up to 65s inside a 75s grace period

**Date:** 2026-09-27
**Status:** proposed
**Arc:** rollout-drain — [plan](../plans/2026-09-27-rollout-drain-fixes.md), from [the measurement](../plans/2026-09-24-rollout-drain-measurement.md)
**Where it lives:** `services/api-gateway/src/shared/utils/http-drain.util.ts`, `services/api-gateway/src/main.ts`, `shutdown.gatewayDrainSeconds` in `deploy/helm/ticketbottle/values.yaml`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How long may a stopping gateway keep serving requests in flight? | Up to 65s: `Connection: close` on every response, idle sockets closed, the rest cut at 65s; a 75s grace period | Up to 20s inside the default 30s grace period | In the worst case a gateway stop takes 75s |

## Context

On 2026-09-25 the gateway closed its listener on SIGTERM but went on serving 17
keep-alive connections for 30s, until SIGKILL. `server.close()` waits for every open
socket, and a client that keeps sending never lets its socket go idle. Every gateway
rollout took 40–42s to remove the old pod, and anything in flight at the SIGKILL was
cut. `7cd6774` had measured a 1s exit with no client connected.

A local replay with four busy keep-alive clients reproduced it: without the header,
`close()` never returned. With `Connection: close` on each response sent after the
drain begins, it returned in 5–25ms.

A checkout is the long request. The gateway sets no deadline on `CreateOrder`, and
order-svc lets it run to `ORDER_CREATE_TIMEOUT`, 60s. A cut checkout can be retried
safely: a repeat `CreateOrder` for the same buyer finds their purchase slot and
`resumeExistingOrder` returns the same order. The buyer still sees one error.

## Options

**Drain up to 65s, grace period 75s** (5s sleep + 65s + 5s margin). A gateway stop never
cuts a checkout. Once keep-alive clients are told to leave, a stop lasts as long as its
slowest request in flight, usually well under a second; 75s is the ceiling.

**Drain up to 20s, the default 30s grace period.** Every stop finishes within 30s. A
checkout slower than 20s is cut, and the buyer's retry resumes the same order.

## Decision

"Up to 65s, grace 75s (Recommended)" — the architect, 2026-09-27, choosing from the two
options above. The recommendation was the agent's.

## Consequences

One value, `shutdown.gatewayDrainSeconds`, sets both the gateway's
`SHUTDOWN_DRAIN_SECONDS` and its `terminationGracePeriodSeconds` (sleep + drain + 5), so
the two cannot drift.

A response already sent without the header leaves an idle socket that is closed when
the client reuses it or when Node's keep-alive timeout (5s) expires, so a stop with no
slow request ends within about 5s of SIGTERM.

A node drain, or an HPA scale-in on EKS, can wait up to 75s per gateway pod. If
`ORDER_CREATE_TIMEOUT` rises above 60s, the drain rises with it.
