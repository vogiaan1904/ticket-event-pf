# 0035 — The gateway bounds a backend reconnect by replacing the channel

**Date:** 2026-11-15
**Status:** accepted
**Arc:** gateway-recovery — [plan](../plans/2026-10-12-gateway-recovery-after-a-backend-crash.md)
**Where it lives:** `services/api-gateway/src/shared/grpc/bounded-channel.ts`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What bounds the gateway's failure after a backend crash, while its trigger is unknown? | Each gRPC client's channel is replaced when it stays `CONNECTING` or `TRANSIENT_FAILURE` for 5s | Shorter kernel SYN retries in the gateway pod; more traced crashes to find the trigger first | Our own wrapper around grpc-js's channel, and a fix shipped before the cause is known |

## Context

On 2026-11-14 one event-service crash in three held every gateway call failing for
133s after the replacement was Ready. Two traced crashes on 2026-11-15, set up the
same way, recovered within 3.4s, and the trace on a 2-vCPU box distorts the run it
watches. The trigger is unknown. The mechanism is not: grpc-js puts no limit on a
connect attempt, and while one hangs, pick_first fails every call with its last
error. The 2026-10-12 local repro showed a single stalled reconnect holding a
channel failing for as long as the run lasted.

## Options

**A. Bound the attempt in the gateway** (recommended). Watch each channel's state;
replace it when it has not reached `READY` within a bound. Covers any trigger that
leaves a channel stuck, including ones not yet seen. Costs code around the client
library that grpc-js upgrades must keep working.

**B. Shorter `tcp_syn_retries` in the gateway pod.** One line in the chart. Bounds
only a SYN that is dropped (H1), which no run has shown; a TCP connection that never
answers HTTP/2 (H2) is untouched.

**C. Find the trigger first.** About 35 minutes per crash at one reproduction in
five, with an instrument that perturbs the box.

## Decision

The architect was shown A, B and C with their costs and answered "go with A",
2026-11-15.

Three choices came with A and were not put as separate calls: the bound is 5s,
fixed in code; the wrapper plugs in through grpc-js's `channelFactoryOverride`, so
no feature service changes; every gateway client gets it, not only event-service's.

## Consequences

- A stuck channel costs at most about 5s of failures plus one connect, whatever
  stalls it.
- During a real outage the channel is replaced every 5s, so attempts run every 5s
  instead of grpc-js's backoff, up to 120s. One connect per client every 5s is
  negligible at five clients.
- A replaced channel's hung socket stays open until the kernel abandons it, up to
  about 127s: one per replacement.
- What stalls the attempt on k3s stays unknown. The open register row keeps its
  first half.

## Outcome

Built test-first in `8f32853`: the local repro failed for its whole 10s on a plain
channel, and recovered 0.3s after the backend returned on a bounded one. Mutating the
timer's reset turns the idle-channel test red.

Run on k3s 2026-11-16, `sha-4355dd3`
([plan, *Results*](../plans/2026-10-12-gateway-recovery-after-a-backend-crash.md#results)):
- Under a node `DROP` on the gateway's new connections to event-service, the channel was
  replaced every 5 to 6s, each replacement failing its queued calls at once.
- Two bounded clients in the gateway pod, on the global and on a local subchannel pool,
  both reached `READY` 1.5s after the drop was lifted.
- The plan's end-to-end pass, the last failure within about 6s of the lift, failed: 16s.
  The gap was queueing, not the channel. The box lost 0.5 to 0.9 of its 2 vCPUs to steal,
  and the gateway sets no deadline on a gRPC call, so event-service ran up to 476
  abandoned calls. That is a question of its own, open in the root `CLAUDE.md` register.
- `k3s-gate2` passed.

The architect accepted on this evidence: "yes do what you propose", 2026-11-16, to
accepting on the isolated proof rather than rerunning the loaded test on a box without
steal. What stalls the attempt on k3s is still unknown; the failure is bounded instead.
