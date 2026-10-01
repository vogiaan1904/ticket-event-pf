# The order contract

**Status: in progress.** Calls delegated to the agent on 2026-10-01. Decision:
→ [0026](../decisions/0026-the-order-contract-tells-the-buyer-what-happened.md), proposed.

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
