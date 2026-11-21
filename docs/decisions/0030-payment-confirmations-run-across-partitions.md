# 0030 — Payment confirmations run across partitions

**Date:** 2026-10-11
**Status:** accepted
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

## Outcome

Built test-first in four commits, each check run red before its change:
`39a4c81` (the consumer's config: its test failed on `Offsets.Initial = -1, want
sarama.OffsetOldest (-2)`, then passed; order-svc's 9 packages pass under `-race`),
`323f4f8` (`ensure-topics.sh`: its test against Redpanda v24.2.7 failed on the missing
script, then passed; deleting the grow branch fails it with `ensure-topics older=4
exited non-zero`), `b4810b9` (the init container: the render check failed for k3s,
then passed for both overlays; a count of 4 fails it), `021a129`
(`confirm_overlap.py`: 6 tests, and a close sorted after a start at the same instant
fails one; on the 2026-10-11 lists it prints `at most 1 at once` for 256 and for 293
confirms).

Verified on k3s with build `sha-021a129`, 2026-10-12. The upgrade grew
`payment.completed` from 1 partition to 8 before order-consumer started, the group
claimed all 8, gate2 passed, and a restarted pod changed nothing. Under 20 buyers
for 3 minutes at the door of 2 a second, `ConfirmOrder` ran up to 4 at once where
one partition never ran 2. All 368 purchases completed, create to `COMPLETED` p99
2.76s ([the plan's results](../plans/2026-10-11-payment-confirmations-across-partitions.md#results)).
A first run was void: the box was deleting the 2026-10-11 sweep's workflows, whose
24-hour retention fell due while it was stopped.

Found while building:
- **The oldest message applies to the whole group**, so `payment.failed` also replays
  when its offset is gone. A replay is harmless: `Release` does nothing on released
  rows, the status write repeats `PAYMENT_FAILED`, and the waitroom drops a
  `checkout.failed` whose session is gone.
- **The first upgrade overlaps the old consumer and the new one.** If the old pod,
  still starting at the newest, claims a new partition in that rebalance and marks a
  message there, what was written before its claim is skipped. Only this one
  upgrade can hit it, and only with payments in flight. The k3s upgrade ran idle and
  EKS installs fresh, so it was accepted rather than closed with a `Recreate`
  strategy.

How the four choices not put to the architect held:
- **8 partitions:** not sized again. At most 4 were in use at a door of 2 a second;
  the count stands on 2026-10-11's measurement.
- **The init container:** the upgrade's log shows `add-partitions` growing the topic
  before the consumer started; a restart found 8 and did nothing.
- **The oldest message for a partition without an offset:** not exercised. The new
  partitions were empty when claimed and no offset was lost; replay safety rests on
  the code read above.
- **Only `payment.completed`:** the chart passes only `payment.completed=8`. The
  other topics' counts were not read back on the box.
