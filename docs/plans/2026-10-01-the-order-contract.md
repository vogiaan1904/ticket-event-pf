# The order contract

**Status: COMPLETE 2026-10-01.** A buyer reads, lists and cancels their own orders,
nobody else can, and every stored status reaches them as itself; verified on k3s.
Decision: → [0026](../decisions/0026-the-order-contract-tells-the-buyer-what-happened.md), accepted.

**Goal:** A buyer can read, list and cancel their own orders through the gateway,
nobody else can, and every status order-svc stores reaches them as itself.

**Owner of the question:** the root `CLAUDE.md` register, open row "Why does the
gateway send order fields the contract no longer has?". This plan closes it.

## Global constraints

- Field numbers in `proto/order.proto` stay; new fields and enum values take new
  numbers.
- Regenerate every consumer of `order.proto`: order-svc (Go) and the gateway (TS).
  Commit stubs with the contract.
- The error taxonomy decides codes: a stranger's order is `NOT_FOUND`; a cancel that
  loses to a payment is `FAILED_PRECONDITION`; a missing `user_id` is
  `INVALID_ARGUMENT`.
- The gateway's TS layout is not converged (`docs/design/ts-layout.md`): keep the
  orders module's shape, change its contents.
- order-svc returns declared error vars, never `fmt.Errorf`.
- Its repository tests need `ticketbottle-order-dynamodb` (port 8000).

## Review focus

1. **A stranger learns nothing.** Read and cancel answer 404 before any status check.
2. **Cancel never beats a payment.** The flip is conditional, and tickets are released
   only after it lands.
3. **The first page has no cursor.** An empty cursor means "from the start".
4. **An unknown status is not a 500.** A gateway older than a new status reads it as
   `UNSPECIFIED`.
5. **The timed-out checkout now reads `EXPIRED`.** `gate-checkout-expiry.sh` asserted
   `CANCELED` and must follow.

---

### Task 1: every stored status reaches the wire

- `proto/order.proto`: `ORDER_STATUS_EXPIRED = 5`, `ORDER_STATUS_REFUND_REQUIRED = 6`,
  `ORDER_STATUS_REFUNDED = 7`. Regenerate order-svc (`make -C services/order-svc
  protoc-all`) and the gateway (`npm run proto:order`).
- `presenter.go`: `GrpcOrderStatusValue` maps every `models.OrderStatus`; `OrderStatus`
  (the filter map) maps every wire value back.
- Tests, `presenter_test.go`: `TestEveryStoredStatusHasItsOwnWireValue` iterates every
  model status and fails on `UNSPECIFIED` or a shared value; it replaces
  `TestATimedOutOrderReadsCanceled`. Red first: `REFUND_REQUIRED` reads `UNSPECIFIED`.

### Task 2: an order is read and cancelled by its owner only

- Contract: `GetOrderRequest.user_id = 2`, `CancelOrderRequest.user_id = 2`.
- `validation.go`: both require `user_id`. `GetManyOrders` no longer requires a
  cursor.
- Service: `GetByID(ctx, code, userID)` and `Cancel(ctx, code, userID)` answer
  `ErrOrderNotFound` when the order is someone else's, before anything else.
  `GetByID` also returns the order's items, and the presenter sends them and the
  phone.
- Tests (service, real DynamoDB): `TestGetByID_AStrangerIsToldThereIsNoSuchOrder`,
  `TestGetByID_TheOwnerSeesTheItems`, `TestCancel_AStrangerIsToldThereIsNoSuchOrder`;
  (delivery) `TestGetManyOrders_TheFirstPageNeedsNoCursor`,
  `TestGetOrder_RefusesARequestWithNoOwner`.

### Task 3: a cancel never overwrites a payment

- Repository: `CancelIfPending(ctx, code)` with the same shape as `ExpireIfPending`,
  through one shared conditional write.
- Service `Cancel`: owner, then `CancelIfPending`. Not flipped → `ErrOrderNotPending`.
  Flipped → release the tickets, then publish `checkout.failed`.
- Tests: `TestCancelIfPending_LeavesAPaidOrderAlone` (repository);
  `TestCancel_APaymentThatLandedFirstWins` (service: the order turns `COMPLETED`
  after the read and before the write — refused, no release).

### Task 4: the gateway's routes follow the contract

- Regenerated `order.pb.ts` (Task 1). Service passes `userId` to `getOrder` and
  `cancelOrder`; the list sends `cursor` and `pageSize`.
- Routes: `GET /orders/code/:code`, `DELETE /orders/code/:code`, `GET /orders`
  (`cursor`, `limit`, `eventId`, `status`). `GET /orders/:id` and `DELETE /orders/:id`
  are removed.
- DTOs: `OrderRespDto` loses `id`; the list's `meta` is `{ perPage, count, nextCursor,
  hasNext }`; `OrderStatus` gains `EXPIRED`, `REFUND_REQUIRED`, `REFUNDED`.
- `OrderStatusMapper.toEnum` answers `UNSPECIFIED` for a value it does not know.
- Tests: `order-status.mapper.spec.ts` (every wire value; an unknown one),
  `orders.service.spec.ts` (owner passed on read and cancel; first page sends no
  cursor).

### Task 5: acceptance and documents

- `deploy/scripts/gate-orders.sh`, `make -C deploy k3s-gate-orders`: a buyer with a
  pending order lists it on the first page; a stranger reads and cancels it and gets
  404 both times; the owner cancels it (`CANCELED`), and cancelling again answers 409.
- `gate-checkout-expiry.sh` expects `EXPIRED` for A's order.
- order-svc `CLAUDE.md`: the statuses on the wire and the owner rule; the root
  register row moves to *Decided*, owned by 0026; the system map cites the gate.

### Task 6: verify on k3s, then record

Push; wait for CI. Start the box (the EKS check first), deploy, then
`k3s-gate2`, `k3s-gate-sold-out`, `k3s-gate-room`, `k3s-gate-orders` and
`k3s-gate-checkout-expiry`. Stop the box. 0026 accepted with an Outcome; this plan
COMPLETE with Results.

## Results

Tasks 1–5 landed as `71f737f`, `f82ae31`, `c1fdcc1`, `b1d8379` and `6077629`. Every
test the tasks name went red first, then green, and the deliberate breaks failed. The
final review was a self-review, with no fresh reviewer. It found nothing Critical or
Important, and deferred one minor: a cancel does not void the payment link at the
provider, so a buyer who cancels and then pays is charged, the order reads
`REFUND_REQUIRED`, and `OrdersNeedingRefund` pages.

Where the run departs from the tasks as written:
- The gateway's regenerated stub was committed in Task 4, not Task 1, so that no commit
  leaves the gateway unable to compile.
- Task 6's first k3s run found the phone, the payment method and every item's quantity
  and price unstored. `f4a7bb3` fixed all three, with
  `TestCreateOrder_StoresWhatTheBuyerAskedFor` and
  `TestCreateOrderInput_KeepsEveryFieldTheBuyerSent`. Orders stored before it keep
  their zeros.
- The list nests its `{ data, meta }` inside the gateway's envelope, as the event list
  does; `a6e1ef1` corrects the gate's path.
- The SSH allowlist needed the other of this network's two egress addresses again.

Task 6, on k3s on 2026-10-01:

| Check | Result |
|---|---|
| `make -C deploy k3s-gate-orders` | passed on revisions 60 and 61: owner reads 2 tickets and lists the order; stranger 404 and 404; cancel 200, `CANCELED`, reservation `CANCELLED`, second cancel 409 |
| gate 2, sold-out | passed on revisions 59, 60 and 61 |
| `make -C deploy k3s-gate-room` | passed on revisions 59 and 61, with `paused` read strictly as `False` |
| `make -C deploy k3s-gate-checkout-expiry` | passed on revision 61: A's order read `EXPIRED` on the wire 555s after it was placed |
