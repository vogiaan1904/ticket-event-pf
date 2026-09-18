# Purchase slot

One in-flight purchase per buyer. The slot is what makes `CreateOrder`
idempotent from the client's side: a buyer who double-clicks, retries a timed
out request, or opens a second tab lands back on the checkout they already
have instead of minting a second order, a second inventory hold and a second
payment intent.

Code: `internal/order/purchase_slot.go` (key), `internal/order/service/order.go`
(claim / resume / release), `internal/workflows` (release on completion).

## The key

| Event | Slot identity | Why |
|---|---|---|
| Waiting room on | `sessionID` | Admission already handed out exactly one |
| Waiting room off | `user#<id>:event#<id>` | Still suppressed, just not by the queue |

Derived in one place because two callers must agree on it byte for byte: the
service writes the claim before an order exists, the workflow releases it from
the order record once the purchase is over. A format string copied into the
second caller would drift, and the release would miss every claim.

## Lifecycle

```
claim (dedupeKey -> orderCode)   conditional write, DynamoDB
   |
   +-- won      -> run the CreateOrder saga
   +-- taken    -> resume: look up the order the slot names
                     |
                     +-- pending | completed -> return that order + payment URL
                     +-- terminal            -> release, retake the slot
                     +-- not written yet     -> see "Settle window"
```

`claimAttempts = 3` bounds the retake loop. A release must be followed by a
fresh claim — freeing the slot and proceeding without retaking it leaves the
create unguarded. Exhausting the loop means the slot is churning, not broken:
the buyer is asked to retry (`ErrPurchaseSlotUnsettled`), not handed a fault.

### Terminal statuses release the slot

`cancelled`, `payment_failed`, `timeout`, `refund_required`, `refunded` — the
buyer holds no ticket in any of them. A refund owed or already paid is tracked
independently of their ability to buy again, so blocking a new purchase would
only punish them for a failure on our side.

An unrecognised status is refused instead. Guessing either double-sells or
strands the buyer.

### Pending order with no payment intent

The saga writes the order row two steps before it creates the payment intent,
so a pending order whose payment lookup 404s is a checkout still being set up,
not a broken one. Passing that refusal through would tell a buyer whose
purchase is mid-success that they are forbidden. `ErrPurchaseSlotUnsettled`.

payment-svc answers an unknown idempotency key with `PermissionDenied`, not
`NotFound`, so both codes read as "no payment yet".

## Settle window

A claim naming an order that does not exist is **not** automatically stale. The
saga reserves inventory and starts a workflow before the order row is written,
so a duplicate arriving inside that window sees the same "not found" a genuinely
abandoned claim shows. Age against the window tells them apart:

| Claim age | Meaning | Action |
|---|---|---|
| `< window` | Create may still be running | Leave the claim, tell the caller to retry |
| `>= window` | Nothing is running; it finished (we'd have found the order) or died | Release, tell the caller to retry |

Sizing (`purchaseSlotSettleWindow`):

```
createTimeout                    caller's own leg: ticket-class lookup + workflow start
+ workflows.CreateOrderSlotBudget()   the saga's own budget, server-side
+ purchaseSlotSettleMargin (30s)      DynamoDB round trips + clock skew
```

Two budgets run back to back and the window has to cover both. `createTimeout`
(`ORDER_CREATE_TIMEOUT`) stops applying the moment `ExecuteWorkflow` returns —
`wfRun.Get` is a wait, not a leash, and nothing on this side cancels a workflow
that outlives it. After that the saga runs on Temporal's budget.

**Sizing on the caller's deadline alone is the bug this prevents.** It would
call a workflow still legitimately reserving inventory abandoned, hand its slot
to the buyer's retry, and end with two orders, two holds and two payment intents
out of one slot.

`CreateOrderSlotBudget` is computed from the activity options rather than
written down as a duration: raise the retry policy and the window widens with
it. It counts two activities at full retry budget — the inventory hold, then the
order write — because the row only appears when the second lands.

## Caller timeout

`wfRun.Get` returning on a dead context means the caller gave up, not that the
saga stopped. It runs server-side and would go on reserving inventory.

```
ctx.Err() != nil
  -> CancelWorkflow (detached ctx, 5s)
       +-- ok     -> release the slot; inventory is taken before the order row
       |             is written, so a cancelled workflow wrote nothing
       +-- failed -> keep the claim; the workflow may still complete and write
                     a live order, and releasing would let a retry mint a
                     second one behind its back
```

A slot held a little too long is the safer error than two orders on one slot.

Release runs on a context detached from the request — the case that needs it
most is the one where the caller already gave up. A failed release is logged and
swallowed: the claim carries a TTL, the next request to find it stranded clears
it, and the caller is owed the error that failed their create, not this one.
