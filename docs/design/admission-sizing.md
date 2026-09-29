# Admission sizing — design

**Status:** specified 2026-09-29, unbuilt. Decision:
[0019](../decisions/0019-the-waitroom-door-speed-is-sized-per-target.md), proposed.
**Applies to:** `waitroom-service` and the chart's per-target values.

## The problem

The waitroom has two knobs, both global constants in
`deploy/helm/ticketbottle/templates/apps/config.yaml`:

| Knob | Setting | Value |
|---|---|---|
| **Door speed**: buyers admitted per tick | `QUEUE_DEFAULT_RELEASE_RATE` per `QUEUE_PROCESS_INTERVAL` | 10 per 1s |
| **Room size**: buyers holding a checkout slot at once | `QUEUE_DEFAULT_MAX_CONCURRENT` | 100 |

They do different jobs.
- An admitted buyer costs the machine CPU twice, at checkout and at confirm:
  about 0.4 core-seconds above idle on k3s at `sha-b6d2971`.
- A buyer who is paying costs the machine nothing.

So door speed is what loads the machine. Room size is what bounds the tickets
held while people pay.

Little's law ties the two together: buyers inside = door speed × how long each
one stays. Which knob is the limit depends on the stay:

```
load test    stay ~6s     3/s x 6s = 18 inside; the room never fills, the door is the limit
real buyers  stay minutes the room fills first; the machine idles
sale opens   room empty   every tick admits a full batch; the door is the limit
```

The door at 10 a second is more than three times what the k3s box serves. At 20
buyers on 2026-09-29 the box ran saturated, and 78–86% of checkouts finished
under 2s against an SLO of 99%:
`docs/plans/2026-09-27-saga-short-steps-local.md#what-this-says`.

## The rule

**Door speed belongs to the deployment target; room size belongs to the event.**

- Door speed is set in each target's values overlay, from a measurement on that
  target. The chart default stays 10 and counts as unmeasured.
- Room size stays one global default until the per-event question is decided:
  root `CLAUDE.md`, decision register, open.

## What "the machine" is

A door speed measured on a target holds only while three things stay as they
were when it was measured:

| | k3s, 2026-09-29 |
|---|---|
| Instance | t3.large, two vCPUs |
| CPU credits | unlimited: a t3.large sustains 30% of each vCPU, 0.6 vCPUs, on its own credits and bills the surplus above that |
| Cost per purchase | build `sha-b6d2971`: node 0.6, Temporal 0.12 core-seconds |

Change any of the three and measure again. 0018 alone moved capacity from 1.7 to
3.0 purchases a second.

**Unlimited credits are required, not incidental.** The box uses 0.46–0.65 cores
at idle, about the 0.6 vCPUs it earns, so it banks almost no credits. In standard
mode a load test would be throttled to 0.6 vCPUs almost at once, leaving nearly
nothing for purchases.

**The t3.large is a testbed choice.** It is cheap because the box is stopped or
idle most of the month, and at full CPU it costs more than a non-burstable
instance of the same size. A production target sizes on a non-burstable instance,
whose capacity does not depend on a credit balance. The EKS nodes are burstable
too (spot `t3.large` and `t3a.large`), and spot picks which of the two runs.

## How a target's door speed is found

1. Deploy the build being measured, and wait until Temporal's CPU is back at idle.
2. Sweep door speeds of 1, 2, 3 and 4 buyers a second (`QUEUE_DEFAULT_RELEASE_RATE`
   at a 1s tick). Run 40 buyers for 5 minutes at each speed. That is more buyers
   than door speed × stay, so a queue exists throughout and the door is the
   only limit.
3. Record for each run:
   - checkout p99 and the share under 2s, at the gateway;
   - purchases a second;
   - cost per purchase (`deploy/scripts/purchase-cost.sh`);
   - queue depth and slots in use (`tb_waitroom_queue_depth`,
     `tb_waitroom_slots_in_use`). A queue that never empties shows the door
     was the limit, and depth ÷ door speed is the wait in the queue;
   - the opening's first-task wait (`deploy/loadtest/saga_latency.py --burst-first 20`).
4. **The door speed is the highest speed whose checkout p99 stays under 2s in two
   runs.** The next step up, which fails, is the headroom.

## Current values

| Target | Door speed | Source |
|---|---|---|
| k3s | 10, the chart default | unmeasured |
| EKS | 10, the chart default | unmeasured |

## Not in scope

- **Room size per event.** The register keeps it open.
- **An adaptive door**, one that slows admission as checkout latency rises. It
  would follow capacity without being measured again, but it puts a control loop
  in the admission path, and such a loop can oscillate. It would still need a
  measured ceiling, and this design provides one.
- **A shorter tick to spread each batch.** A tick admits its whole batch at once.
  This is left alone unless the sweep's opening wait shows it matters.
- **More than one waitroom replica.** The door runs in one replica
  (`services/waitroom-svc/CLAUDE.md`, *Single-replica constraint*); with more,
  door speed would multiply by the replica count.

## Where it lives, once built

- The door speed reaches `config.yaml` from values: a default in
  `deploy/helm/ticketbottle/values.yaml`, and the measured k3s value in
  `deploy/helm/ticketbottle/values-k3s.yaml`.
- The credit mode is a `cpu_credits` variable on `deploy/terraform/modules/ec2-k3s/`,
  default `unlimited`, instead of the account's default for the family, which an
  account can change.
- A render check under `deploy/helm/ticketbottle/tests/` shows k3s getting its own
  value.
