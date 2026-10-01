# Admission sizing — design

**Status:** built 2026-09-29; k3s measured, EKS not. Decisions:
[0019](../decisions/0019-the-waitroom-door-speed-is-sized-per-target.md),
[0020](../decisions/0020-a-config-change-rolls-the-app-that-reads-it.md),
[0021](../decisions/0021-an-order-takes-at-most-its-events-ticket-limit.md),
[0023](../decisions/0023-the-waitroom-stops-admitting-when-no-ticket-is-left.md),
[0024](../decisions/0024-an-unpaid-order-times-out-when-its-hold-expires.md),
[0025](../decisions/0025-an-event-admits-buyers-only-while-it-has-tickets-for-them.md).
**Applies to:** `waitroom-service` and the chart's per-target values.

## The problem

When this design began, the waitroom had two knobs, both global constants in
`deploy/helm/ticketbottle/templates/apps/config.yaml`:

| Knob | Setting | Value then |
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
load test    stay ~2-4s   3/s x ~4s = ~12 inside; the room never fills, the door is the limit
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
- Room size is each event's tickets available, read each tick: *Room size per event*
  below.

## What "the machine" is

A door speed measured on a target holds only while three things stay as they
were when it was measured:

| | k3s, 2026-09-29 |
|---|---|
| Instance | t3.large, two vCPUs |
| CPU credits | unlimited: a t3.large sustains 30% of each vCPU, 0.6 vCPUs, on its own credits and bills the surplus above that |
| Cost per purchase | build `sha-8312562` (service code as `sha-b6d2971`): node 0.6, Temporal 0.12 core-seconds |
| What it serves | 3.15 purchases a second saturated; 2 within the checkout SLO |

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
5. **The p99 leaves out checkouts caught in a payment-provider stall,** those whose
   `CreatePaymentIntent` ran past 5s, and counts them beside it with the raw p99.
   A slower door cannot shorten a provider stall, so the raw p99 would size the box
   on the provider (`saga_latency.without_stalls`).

`deploy/scripts/door-sweep.sh` runs steps 2–3 and prints one line per run.

## Current values

| Target | Door speed | Source |
|---|---|---|
| k3s | 2 a second | 2026-09-29, `sha-8312562`: `docs/plans/2026-09-29-admission-sizing.md#results` |
| EKS | 10, the chart default | unmeasured |

## Tickets per order

**Status:** built 2026-09-29; verified on k3s.
[0021](../decisions/0021-an-order-takes-at-most-its-events-ticket-limit.md), accepted.

Today nothing caps an order. `quantity` must only be at least 1, the list of items has
no length limit, and neither order-svc nor inventory checks a total. One buyer in one
chair can reserve every ticket left.

The rule: **each event carries `max_tickets_per_order`, and an order whose items add
up to more is refused before anything is held.**

| | |
|---|---|
| Where it lives | `EventConfig.max_tickets_per_order` (`proto/event.proto`), column `maxTicketsPerOrder` |
| Default | 4, the column default, for new configs and every existing one |
| Organizer's range | 1–10 at the gateway; event-svc also refuses more than 10 |
| Counted | across every item of the order, not per item |
| Refused with | `INVALID_ARGUMENT`, `ORD020`: the buyer fixes it by asking for fewer |
| When | in order-svc's `Create`, after the event is known to be on sale and before the checkout token, the purchase slot or any hold |
| 0 on the wire | means not set: create takes the default, update keeps the stored value, and order-svc checks nothing |

**Why 0 means "not set".** proto3 sends an unset `int32` as 0. During a rolling deploy
an event-svc that predates the field answers 0, and an order-svc that read 0 as a
limit would refuse every order.

Once each order is capped, chairs bound tickets held: tickets held ≤ chairs ×
`max_tickets_per_order`.

A cap on one buyer's total across several orders is a separate anti-scalping rule,
not this one.

## When tickets run out

**Status:** built 2026-09-30; verified on k3s.
[0023](../decisions/0023-the-waitroom-stops-admitting-when-no-ticket-is-left.md), accepted.

The door admits at its speed whatever inventory has left. Once every ticket is held
or sold, each buyer it admits costs the box a checkout that inventory refuses and
holds a chair for the token's 15 minutes, while everyone behind waits for nothing.

The rule: **before it admits anyone, each tick asks inventory what the event has
left, and the answer sets the door.**

| Inventory's counts | Door | The waitroom |
|---|---|---|
| no class that can still sell | open | admits as before: there is nothing to judge by |
| sold = total | sold out | ends the line's queued sessions; a join or a status poll gets 409 `WTR012` |
| nothing available now | paused | admits nobody and keeps every place; a status poll says `paused: true` |
| some available now | open | admits as before |

What inventory counts, in `GetEventStock` (`proto/inventory.proto`):
- `total` and `sold`: over the classes that can still sell, `ACTIVE` and not past
  their sale end.
- `available`: over those on sale now, the tickets neither held nor sold.

So a door pauses both while every ticket left is held and while the rest are not on
sale yet. `sold` never goes down, so only an organizer can reopen a sold-out door, by
raising `total` or re-activating a class.

**One answer per event, half a tick old at most.** The waitroom caches each event's
door for half of `QUEUE_PROCESS_INTERVAL`, so every tick asks afresh and every join
and status poll in between reads the same answer. Concurrent misses collapse into one
call, as the event gate's do. The tick asks only when someone in line is due, so an
event whose line is empty costs inventory nothing. A paused door drops the entries at
the front of its line whose sessions are gone, ended or expired, up to the first live
one. Nothing else would drop them while it admits nobody, and a line left with only
dead entries would go on asking. Inventory's read takes no lock, so it never waits on a
`Reserve` holding a hot row.

**It fails open.** If inventory does not answer within 1s, the door is open until
the next answer, and the waitroom admits exactly as it did before this rule. The door
only saves wasted checkouts; what stops an oversell is inventory's guarded `UPDATE`.
The failed answer is cached like any other, so a down inventory is asked once per
half tick, not once per join. `tb_waitroom_stock_checks_total{result="unavailable"}`
counts them.

**Closing a sold-out line:**
- A queued session is set to `sold_out` before its entry leaves the line, 100 per
  tick. A failure between the two leaves an ended session in line, which the next
  tick removes.
- An admitted session is never removed by closing. It holds a token, and its checkout
  gets inventory's own answer.
- A status poll sees sold out from the cached answer at once. Closing only stops the
  line being kept.
- Ended is final for that session. If the door reopens, its buyer joins again.

**Any rollout order is safe.** Each part is additive:
- a waitroom whose inventory lacks `GetEventStock` gets `UNIMPLEMENTED` and fails
  open;
- a gateway that predates `paused` ignores it;
- `WTR012` is `FAILED_PRECONDITION`, which every gateway maps to 409.

Not in this step:
- a sale that is over, every class past its end with tickets unsold: the door stays
  open, as today;
- chairs that outnumber the tickets left: *Room size per event* below;
- a chair freed when its hold expires: step 3.

## When a checkout is abandoned

**Status:** built and verified on k3s 2026-09-30.
[0024](../decisions/0024-an-unpaid-order-times-out-when-its-hold-expires.md), accepted.

A buyer who takes a hold and never pays is noticed by nothing that tells anyone else.
payment-svc records a failure only when a provider's webhook reports one, and a buyer
who walks away sends nothing:

| What | Today | Because |
|---|---|---|
| The inventory hold | released at about 9m | inventory's expiry worker, which tells no one |
| The waitroom chair | freed at 15m | the slot's own TTL; nobody publishes `checkout.expired` |
| The order | `PENDING` for ever | nothing times an order out |
| The buyer, with no waiting room | locked out of the event for 30 days | the purchase slot is keyed by buyer and event, and a retry resumes the `PENDING` order and its dead payment link until the slot's TTL |

The rule: **order-svc times out an order nobody has paid for when its hold expires.**
Every order the saga creates starts a workflow, `ExpireOrder`, delayed by
`PaymentTimeout + ReservationHoldGrace`, the hold's own length:

```
order still PENDING -> TIMEOUT; release the hold, free the purchase slot, publish checkout.expired
anything else       -> nothing: it was paid, failed or cancelled first
```

- **Why order-svc.** It sets the hold's length and owns the checkout's statuses.
  `ConfirmOrder` already has a branch for a payment on a `TIMEOUT` order, which
  nothing could reach until now.
- **Why a delayed workflow.** A timer that survives restarts, one per order, with
  nothing to poll. Temporal's delayed start costs nothing until it fires. It adds one
  start call to each checkout; that and the run at 9m are unmeasured on k3s.
- **The flip is conditional.** `PENDING` to `TIMEOUT` is a DynamoDB conditional
  write, so a timeout never overwrites a payment that confirmed first.
- **A payment that lands after the timeout** is still confirmed if inventory can
  re-acquire the ticket, and marked `REFUND_REQUIRED` only if it cannot. That keeps
  the backstop in `services/order-svc/docs/RESERVATION_HOLD.md`: a payment made
  inside the provider's 6-minute window whose confirmation ran long.
- **On the wire a `TIMEOUT` order reads `EXPIRED`**, since
  [0026](../decisions/0026-the-order-contract-tells-the-buyer-what-happened.md) gave every stored status its own wire value.
- **Rollout.** An order already `PENDING` at the deploy has no timer and behaves as
  today. The consumer registers `ExpireOrder` long before the first one fires, 9
  minutes after the first new checkout.

`tb_order_checkouts_expired_total` counts timed-out orders: the abandonment rate that
step 4 sizes the room by.

Not in this step:
- a chair whose buyer is admitted and never starts a checkout: it holds for the
  token's 15 minutes, since there is no hold to expire.

## Room size per event

**Status:** built and verified on k3s 2026-10-01.
[0025](../decisions/0025-an-event-admits-buyers-only-while-it-has-tickets-for-them.md), accepted.

Room size is how many buyers may be inside an event at once: admitted, and holding a
checkout pass. It was `QUEUE_DEFAULT_MAX_CONCURRENT`, 100, for every event, and that
was wrong twice:
- **For real buyers it was the limit, not the door.** A full room admits only as fast
  as buyers leave: 100 ÷ stay. At a 3-minute stay that is 0.56 a second, under a third
  of k3s's measured door of 2. Load tests stay 2–4s, so the room never fills there,
  and no test shows it.
- **It knew nothing about tickets.** A 3-ticket event admitted up to 100, and a buyer
  whose checkout `Reserve` refused kept their chair for the token's 15 minutes.

Door speed already bounds the machine, and a buyer who is paying costs it nothing. So
all that is left for room size is matching buyers to tickets, and that is per event.

The rule: **a tick admits only while the event has more tickets available than
buyers inside.**

```
inventory counted the event -> admit min(due, available - inside)
nothing to judge by         -> admit every buyer due: the door speed alone paces
```

| | |
|---|---|
| `due` | the door speed's batch: the buyers at the front whose turn has come |
| `available` | inventory's `GetEventStock`, the answer the door already asks for each tick |
| `inside` | chairs in `waitroom:<event>:checkouts`, once expired ones are swept |
| Nothing to judge by | inventory did not answer, or the event has no class that can still sell |
| `QUEUE_DEFAULT_MAX_CONCURRENT` | deleted |

**Every buyer inside counts as one ticket still to take.** A buyer inside has either
not ordered yet, and may take a ticket, or already holds theirs, which `available` no
longer counts. The tick cannot tell them apart:
- a buyer who already holds tickets is counted twice, so the last tickets of a
  sell-out can wait up to one payment time for a chair to free;
- a buyer who has not ordered yet may take up to `max_tickets_per_order`, so a tick
  can still admit someone who finds nothing left.

Telling them apart needs inventory to count the orders that hold tickets, an
additive field on `GetEventStock`. An inventory without the field answers 0, which
reads as this rule, so that refinement can follow without a coordinated rollout.

**No cap on chairs.** Each tick admits at most the door's batch, and a chair lives at
most the token's 15 minutes. So chairs never exceed door speed × 900s: 1800 on k3s,
9000 on EKS. A cap below that can only hold admission under the measured door.

**What a waiter sees.** `paused` keeps meaning "no ticket available". A waiter held
back only because the buyers inside could take every ticket left sees their position
stop moving, without `paused`.

**What changes for EKS.** The 100 also held real buyers under EKS's unmeasured door of
10 a second. Without it, that door is EKS's only limit. EKS has no real buyers and its
load tests never filled the room, so nothing measured changes. Its door is still
unmeasured (*Current values* above).

**Any rollout order is safe.** Only the waitroom changes, and its ConfigMap digest
rolls it when the key leaves the chart
([0020](../decisions/0020-a-config-change-rolls-the-app-that-reads-it.md)).

Not in this step:
- telling the buyers who hold tickets from those who have not ordered: the refinement
  above;
- a chair whose checkout `Reserve` refuses is still held for the token's 15 minutes.
  This rule admits fewer such buyers; it does not free their chairs.

## What comes next, in order

Agreed on 2026-09-29. Each gets its own plan once the one before it lands:

1. **Tickets per order.** The section above; built 2026-09-29.
2. **The waitroom knows when tickets run out.** It asks inventory each tick. The
   section *When tickets run out* above; built and verified on k3s 2026-09-30.
   - While every ticket left is held by someone paying, the door pauses: status
     `PAUSED`, with places kept.
   - Once sold equals total, the line closes: status `SOLD_OUT`, and new joins are
     refused with 409.
   - Why ask each tick rather than wait for a message: inventory has no outbox, and
     a lost "tickets came back" message would leave the line paused forever.
3. **A chair is freed when its hold expires.** The waitroom already consumes
   `checkout.expired`, but no service publishes it. So an abandoned checkout holds its
   chair for the token's 15 minutes, 6 minutes after its tickets went back on sale.
   The section *When a checkout is abandoned* above; built and verified on k3s
   2026-09-30: an abandoned chair frees at about 9 minutes.
4. **Room size per event.** The section *Room size per event* above; built and
   verified on k3s 2026-10-01. The design first read "the smaller of the event's size
   and door speed × time to pay". Door speed × time to pay is a property of the
   target, and it cannot bind, so only the event's tickets are left.

## Not in scope

- **An adaptive door**, one that slows admission as checkout latency rises. It
  would follow capacity without being measured again, but it puts a control loop
  in the admission path, and such a loop can oscillate. It would still need a
  measured ceiling, and this design provides one.
- **A shorter tick to spread each batch.** A tick admits its whole batch at once.
  This is left alone unless the sweep's opening wait shows it matters.
- **More than one waitroom replica.** The door runs in one replica
  (`services/waitroom-svc/CLAUDE.md`, *Single-replica constraint*); with more,
  door speed would multiply by the replica count.

## Where it lives

- The door speed reaches `config.yaml` from values: a default in
  `deploy/helm/ticketbottle/values.yaml`, and the measured k3s value in
  `deploy/helm/ticketbottle/values-k3s.yaml`.
- The room rule is `admitCount` in
  `services/waitroom-svc/internal/service/queue_processor.go`, fed by `StockGate.Stock`
  in `services/waitroom-svc/internal/service/stock_gate.go`.
- The credit mode is a `cpu_credits` variable on `deploy/terraform/modules/ec2-k3s/`,
  default `unlimited`, instead of the account's default for the family, which an
  account can change.
- A render check under `deploy/helm/ticketbottle/tests/` shows k3s getting its own
  value.
