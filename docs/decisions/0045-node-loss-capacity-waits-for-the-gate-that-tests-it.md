# 0045 — EKS's capacity to lose a node waits for the gate that tests it

**Date:** 2026-11-24
**Status:** proposed
**Arc:** next-work — [design](../design/eks-stateful-tier.md#gate-4b), found by [the door's plan](../plans/2026-11-20-eks-rds-then-the-door.md#5-the-door-sweep)
**Where it lives:** `docs/design/eks-stateful-tier.md` (*Gate 4b*, phase (f)); nothing built

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| A node holds at most 29 pods and the stack runs about 37, so losing one node strands pods; what is built, and when? | Nothing now; VPC CNI prefix delegation becomes phase (f)'s first task | Prefix delegation and a PriorityClass for the stateful tier now | Until phase (f), a spot reclaim leaves about 8 pods Pending until a new node joins, and a node-group replacement can strand a stateful pod outside its volume's zone until it is moved by hand |

## Context

On 2026-11-23, replacing the node group with on-demand nodes
([0043](0043-the-door-sweep-runs-on-on-demand-nodes.md)) put every pending pod on the
first node Ready until it held 29, the VPC CNI's limit for an `m6a.large`. Redpanda's
EBS volume is in that node's zone, so Redpanda stayed Pending, and every service that
needs Kafka crash-looped until pods were moved by hand.

What runs out is pods, not CPU or memory. The chart requests 0.82 cores and 2.3 GiB
across its 14 pods (EKS render, 2026-11-24), on a node with 2 vCPUs and 8 GiB. System
and monitoring pods bring the total to about 37. So neither node can hold the stack
alone: two nodes give no spare capacity for losing one.

The cluster is ephemeral, and it is used for two things:
- measuring doors, where a lost node costs a manual fix, never a wrong number:
  on-demand nodes and the sweep's INVALID check (0043);
- proving the flow survives a node loss, which is phase (f), the Gate 4b guard,
  deferred by [0031](0031-the-next-work-is-ranked-by-what-a-buyer-can-hit.md).

## Options

**A. Prefix delegation and a PriorityClass, now.** About 110 pods a node, and
redpanda and redis preempt a stateless pod on a full node in their zone. vpc-cni
becomes a Terraform-managed add-on, the node group is replaced once, and the chart
gains two objects. Nothing in use today needs it. The PriorityClass guards against a
node filling on CPU or memory, which today it does not.

**B. Prefix delegation only, now.** Removes the observed cause. It is still unused
until a node-loss test exists.

**C. A node group created before it is destroyed.** Needs a `name_prefix`, so
`gate4b-chaos.sh`'s lookup by the name `spot` changes. It does nothing for a spot
reclaim, the more common way to lose a node.

**D. Nothing now; prefix delegation opens phase (f) (chosen).** The gate that would
fail on the ceiling gets the fix when it starts. Unverified until then: whether a
managed node group computes max pods for prefix mode on its own, or needs the add-on
set before the group is created.

## Decision

With A–D on the table and D recommended, the architect answered, 2026-11-24: "yes do it".

## Consequences

- Phase (f)'s row in the design opens with prefix delegation, and is done only when one
  surviving node holds every pod.
- Until then, losing a node on EKS is fixed by hand: cordon the full node, delete
  stateless pods there, uncordon.
- Holds while EKS stays ephemeral and nothing but phase (f) tests node loss. A target
  that carries traffic needs spare capacity for one node from its first day.
