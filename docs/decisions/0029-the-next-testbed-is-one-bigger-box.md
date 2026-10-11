# 0029 — The next testbed is one bigger box, for one session

**Date:** 2026-10-09
**Status:** proposed
**Arc:** checkout-capacity — [plan](../plans/2026-10-09-capacity-on-a-bigger-box.md)
**Where it lives:** `deploy/loadtest/capacity-values.yaml`, `deploy/scripts/capacity-sweep.sh`, `deploy/loadtest/capacity.py`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| Which testbed answers "what fails first as checkouts rise"? | The k3s box resized in place to 2, 16 and 8 vCPUs for one session, with no workload limits and payment on the simulated provider | Ephemeral EKS with Temporal's database split out, or cost per purchase on the 2-vCPU box | One node: a limit found belongs to a topology where every service and database shares one machine and one disk, and what changes across nodes stays unmeasured |

## Context

The 2-vCPU k3s box saturates at ten buyers, so any higher load there measures the
box, not the design (`docs/plans/2026-09-27-checkout-latency-decomposition.md#what-this-says`).
That plan left the choice of the next testbed to the architect, with three options,
each of which answers a different question.

What the box did measure is cost: 0.4 core-seconds a purchase above idle, with
Temporal and its Postgres the largest consumers until 0018 halved Temporal's share.
Whether anything other than CPU limits throughput as cores grow is unknown.

## Options

**One bigger box, one session.** The everyday box resized in place: the m5 family
at 2, 16 and, if needed, 8 vCPUs, then back to the t3.large. It changes the number
of cores and nothing else. It needs a `terraform.tfvars` edit, not new
infrastructure, and reuses the load tooling. A session costs about $2. Its cost is
that the result comes from one node.

**Ephemeral EKS with Temporal's database split out.** It tests whether the
stateful-tier split pays off. But it builds RDS for Temporal (phase b of
`docs/design/eks-stateful-tier.md`) before anyone has measured that Temporal's
database is the wall. It also changes the nodes, the network hops and the database
at once, and every session adds 15–20 minutes of RDS provisioning.

**Cost per purchase on the 2-vCPU box.** The cheapest option, and 0018 has already
measured it. Extrapolating from it assumes that nothing other than CPU breaks as
load grows, which is the very question.

## Decision

The architect was shown three options with a recommendation: a bigger instance, a
simulated payment provider first, and k6 moved off the box. They replied: "agree,
write the plan" (2026-10-09).

Five choices came inside the plan and have not yet been put to the architect on
their own:
- **k6 stays on the box.** This revises the approved recommendation. k6 used 0.02
  cores at 10 to 40 buyers, and moving it would cost a second instance and two
  exposed Services. A run in which k6 is throttled is void.
- **The m5 family**, for its CPU generation, the same as the t3.large's. An m7i is
  faster per core and would break comparison with every earlier number.
- **Every workload limit lifted**, because the chart's 1-core limits on Temporal and
  Postgres would bind near 8 purchases a second.
- **Postgres's `max_connections` set to 500**, because Prisma's pools grow with the
  node's cores.
- **The simulated provider is a route in `payment-webhook`**, reached through a
  configurable create URL and answering in 330ms. The other way was a fake gateway
  inside payment-svc. The route keeps the real client code on the path.

## Consequences

- One session and a few dollars answer the question up to 16 vCPUs, with no new
  infrastructure.
- A limit that only appears across nodes, such as network hops or a database on
  its own machine, stays out of reach. Finding one needs the EKS option.
- The overlay holds only while it is used for capacity sessions alone, and the box
  goes back to the t3.large. The door speed of 2 a second was measured on the
  t3.large (0019).
