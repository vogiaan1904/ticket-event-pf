# 0030 — Payment confirmations run across partitions

**Date:** 2026-10-11
**Status:** proposed
**Arc:** checkout-capacity — [plan](../plans/2026-10-11-payment-confirmations-across-partitions.md)
**Where it lives:** `deploy/helm/ticketbottle/values.yaml` (`redpanda.partitions`), `deploy/helm/ticketbottle/files/ensure-topics.sh`, `services/order-svc/pkg/kafka/consumer.go`

| Question | Chose | Instead of | Cost |
|---|---|---|---|
| How do payment confirmations keep up as purchases rise? | `payment.completed` with 8 partitions, made so before order-consumer starts; a partition the consumer has no offset for is read from its oldest message | Confirming without waiting for each `ConfirmOrder`, or both | Order holds per payment, not across the topic; the count can grow but never shrink, and growing it moves keys; the consumer cannot start while the broker is down |

## Context

On 2026-10-11 the first wall to purchase throughput was the `payment.completed`
consumer, at about 9.5 a second on any box
(`docs/plans/2026-10-09-capacity-on-a-bigger-box.md#what-this-says`). The topic has
one partition, because the chart creates no topics and Redpanda's default is one,
and `order-consumer` waits for each `ConfirmOrder` before taking the next message.
With 8 partitions set by hand, the same box served 25.5 a second.

The wall is also a correctness risk. The door's speed is sized on checkout p99
(`docs/design/admission-sizing.md`), which does not include the confirm leg, and
EKS runs the unmeasured default of 10 a second. Once purchases outpace confirms,
the backlog grows without bound; past a hold's 9 minutes
(`services/order-svc/docs/RESERVATION_HOLD.md`) a sold-out ticket can be resold,
and the buyer who paid for it becomes `REFUND_REQUIRED`.

## Options

**A1. More partitions on `payment.completed`.** Measured: 9.6 to 23.3 a second at
160 buyers. The relay keys each message by payment id (`outbox-relay/src/kafka.ts`),
so one payment's events stay in order. The count can only grow, and growing it
moves a key to another partition. It needs the consumer to read a partition it has
no offset for from the oldest message: at the newest, a partition added to a live
topic skips what was written to it before the group claimed it.

**A2. Confirm without waiting.** Start `ConfirmOrder`, whose workflow id already
removes duplicates, and mark the message at once. One partition could confirm far
faster, but the consumer loses the failure signal it uses today: `mapWorkflowError`
decides whether a failed confirm is delivered again or settled. A workflow that
fails after it starts would need a new owner.

**A3. Both.** Headroom past the next ceiling, about 24 a second in Temporal and its
Postgres, which nothing needs yet, for A2's design work.

How A1 sets the count, chosen inside the plan:
- **A Helm hook Job, post-install and pre-upgrade.** On a fresh install the relay
  or the consumer creates the topic at 1 partition first. Partitions added while the
  consumer runs are claimed only after sarama's metadata refresh, every 10 minutes
  and up to twice over, past the hold's 3-minute grace.
- **Redpanda's default partition count.** Every auto-created topic would get it,
  including topics whose consumers were never examined for it.
- **An init container on order-consumer** that creates the topic or grows it, and
  fails until it has its count. The consumer cannot start before its partitions
  exist, on a fresh install, an upgrade or a restart.

## Decision

The architect was shown A1, A2 and A3 with a recommendation of A1, and replied:
"go with A1, write the plan" (2026-10-11).

Four choices came inside the plan and have not been put to the architect on their
own:
- **8 partitions**, the count measured on 2026-10-11.
- **The init container**, over a hook Job or the broker's default.
- **The oldest message for a partition without an offset.**
- **Only `payment.completed`.** `payment.failed` and the waitroom's topics keep one
  partition, and the waitroom's consumer keeps starting at the newest.

## Consequences

- The confirm leg's capacity is about partitions ÷ confirm latency: 8 ÷ 0.116s, or
  69 a second. That is above the ceiling measured past it and above both doors (2
  on k3s, 10 on EKS).
- The door is still sized on checkout alone, so that comparison has to keep holding:
  a door raised past the confirm leg's capacity lets confirms fall behind until
  holds expire.
- A delivery replayed from the oldest message is harmless: `ConfirmOrder` returns
  without change for a `COMPLETED` order, and every other final status is a no-op
  or already recorded.
- Order-consumer's pod pulls the Redpanda image and waits for the broker before it
  starts.
