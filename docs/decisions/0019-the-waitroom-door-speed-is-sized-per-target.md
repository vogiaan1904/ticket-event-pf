# 0019 — The waitroom's door speed is sized per deployment target

**Date:** 2026-09-29
**Status:** proposed
**Arc:** admission-sizing — [design](../design/admission-sizing.md)
**Where it lives:** `deploy/helm/ticketbottle/templates/apps/config.yaml` (`QUEUE_DEFAULT_RELEASE_RATE`), `deploy/helm/ticketbottle/values-k3s.yaml`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| What sets how fast the waitroom admits buyers? | A door speed per deployment target, measured on it at the checkout SLO | One global constant, or a door that adapts to latency | A number that is wrong as soon as the machine, its CPU credit mode or the cost per purchase changes, until someone measures again |

## Context

The waitroom admits `QUEUE_DEFAULT_RELEASE_RATE` buyers a tick, 10 a second, into
at most `QUEUE_DEFAULT_MAX_CONCURRENT` slots, 100. Both are global constants.
Every admitted buyer costs the machine CPU, so door speed is what loads it. On the
k3s box the door admits more than three times what the box serves. On 2026-09-29,
at 20 buyers, the box ran saturated and 78–86% of checkouts finished under 2s:
`docs/plans/2026-09-27-saga-short-steps-local.md#what-this-says`.

What the box serves is not a property of the code. It depends on:
- the instance;
- its CPU credit mode (a t3.large sustains 30% of each vCPU without unlimited
  credits);
- the cost per purchase, which 0018 halved.

## Options

**A measured door speed per target.** Each target's values overlay sets the door
speed from a sweep on that target (`docs/design/admission-sizing.md`). It is config
only. It is right until the machine or the cost per purchase changes, and then
someone has to measure again.

**One global constant, retuned.** The simplest option. k3s and EKS would share
one number, right for at most one of them.

**A door that adapts.** The waitroom slows admission when checkout latency rises.
Nothing has to be measured again, but it puts a control loop in the admission
path that can oscillate, and it still needs a measured ceiling.

## Decision

The architect named the dependency: "we choose this machine t3 large, so it affect
our decision on how many in waitroom ? right ? if so, we need to record this"
(2026-09-29).

The architect chose the scope: "A" (2026-09-29). That was the door speed only,
sized per target, with room size per event left open.

The architect approved the design: "yes continue for me" (2026-09-29). The design
had four parts:
- the sweep;
- the rule of the highest speed that holds checkout p99 under 2s in two runs;
- EKS left unmeasured at the default;
- the credit mode written into Terraform.

After a review of how credit modes are usually pinned, the architect approved
two refinements with "yes do it" (2026-09-29):
- the credit mode is a `cpu_credits` module variable;
- the design states that the t3.large is a testbed choice, and that a production
  target sizes on a non-burstable instance.

## Consequences

- Each target carries its own door speed, with the date, build and instance it was
  measured on.
- A change to cost per purchase makes a measured door speed stale. The work that
  changes the cost owns measuring it again.
- EKS runs the unmeasured default until it is measured.
- Door speed and room size stop sharing one register question. Room size per
  event stays open.
