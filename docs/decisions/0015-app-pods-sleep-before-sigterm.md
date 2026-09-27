# 0015 — An app with a Service sleeps 5s before SIGTERM

**Date:** 2026-09-27
**Status:** proposed
**Arc:** rollout-drain — [plan](../plans/2026-09-27-rollout-drain-fixes.md), from [the measurement](../plans/2026-09-24-rollout-drain-measurement.md)
**Where it lives:** `deploy/helm/ticketbottle/templates/apps/_appservice.tpl`, `deploy/helm/ticketbottle/templates/apps/payment-events.yaml`, `shutdown.preStopSleepSeconds` in `deploy/helm/ticketbottle/values.yaml`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Does an app pod keep serving after it leaves its Service's endpoints? | A native `preStop` sleep of 5s on every app a Service routes to | 2s on the same apps, or 5s on all ten | 5s on every pod stop, and nothing for a pod that crashes |

## Context

Kubernetes sends SIGTERM and removes the pod from its endpoints at the same time.
Since `7cd6774` the NestJS services close their port on SIGTERM, so for as long as
kube-proxy still routes to the pod, a new connection is refused.

Measured on k3s on 2026-09-25, with no delay:

- 5 of 5 gateway rollouts refused 18–31 connections each, all within ~1s of SIGTERM.
- 4 of 5 event-service rollouts caused 40–73s of 503s on every event read. Raw TCP
  from the gateway to `event-service:50053` recovered 1.8s after the stop; the
  gateway's gRPC calls took 40.7s.

grpc-js alone recovers the moment the port listens again (a local replay: 48 failures
over a 1.0s refusal), and grpc-js 1.13.4 has no connect timeout. The ~40s is therefore
spent on a reconnect that met the vanishing pod — a reading, which the plan's trace
confirms or refutes.

The sleep action is native to the kubelet (`lifecycle.preStop.sleep`), so no image
needs a `sleep` binary. k3s runs v1.36; EKS runs its current default version.

## Options

**5s, apps with a Service** — `app-gateway`, `user-service`, `event-service`,
`order-service`, `payment-service`, `waitroom-service`, `inventory-service`,
`payment-webhook`. About five times the window measured on an idle node. Costs 5s on
every stop of those pods.

**5s, all ten apps** — adds `order-consumer` and `outbox-relay`, whose only Services
are for metrics. Nothing routes requests to them, so for those two it is only a delay.

**2s, apps with a Service** — faster stops, but only twice the window measured on an
idle node, with less margin when the node is busy.

**No delay** — the other branch of the measurement's decision rule, ruled out by the
failures above.

## Decision

"5s, apps with a Service (Recommended)" — the architect, 2026-09-27, choosing from the
three options above. The recommendation was the agent's.

## Consequences

A rollout's new connections go to the new pod before the old one closes its port, so
the gateway's reconnect to a backend never meets a closing pod.

A crash, an out-of-memory kill or an eviction gets no sleep. Finding 1's ~40s outage
remains for those, and stays open in the root `CLAUDE.md` register.

On EKS the ALB deregisters targets on its own, slower clock. 5s is not assumed to be
enough there; it is measured before EKS carries traffic.

It holds while kube-proxy propagates an endpoint change well inside 5s, and while every
app with a Service finishes its shutdown in the 25s left of the default 30s grace
period. The gateway, which cannot, has its own grace period:
[0016](0016-the-gateway-drains-for-up-to-65s.md).
