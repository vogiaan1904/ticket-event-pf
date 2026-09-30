# An abandoned checkout expires

**Status: Tasks 1–5 built 2026-09-30; Task 6, the k3s run, waits on the architect's
go-ahead.** The architect answered the three calls below on 2026-09-30, choosing the
recommended option each time, so every task stands as written. Decision:
→ [0024](../decisions/0024-an-unpaid-order-times-out-when-its-hold-expires.md), proposed.

**Goal:** An order nobody pays for times out when its hold expires. At that point it
releases the hold, frees the buyer's purchase slot, and publishes `checkout.expired`,
so the waitroom frees the chair at about 9 minutes instead of 15.

**Spec:** `docs/design/admission-sizing.md`, *When a checkout is abandoned*. This is
step 3 of that design's *What comes next*. Where this plan and the spec disagree, the
spec wins.

**Architecture:**
- Each successful `Create` in order-svc starts `ExpireOrder` on the confirm-order
  task queue, with a Temporal start delay of `CheckoutLifetime`, the hold's own
  length.
- `ExpireOrder` flips `PENDING` to `TIMEOUT` with a DynamoDB conditional write. If
  the flip happens, it then releases the hold, frees the slot and publishes
  `checkout.expired`, which the waitroom already consumes.

## The architect's calls

This plan is written for the recommended answer to each call. If an answer goes
the other way, the tasks named change:

| Call | Recommended | If not |
|---|---|---|
| 1. Who runs the clock | order-svc, a delayed `ExpireOrder` at the hold's expiry | The plan is void; write a new one for the chosen owner |
| 2. A payment after the timeout | Confirm if inventory can re-acquire, refund if not | Drop Task 3's `ConfirmOrder` change and its two tests; the switch refunds `TIMEOUT` today |
| 3. What the buyer sees | `TIMEOUT` reads `CANCELED` on the wire | Drop Task 3's presenter line; `EXPIRED` needs its own plan, since it touches the stale gateway `order.pb.ts` |

## Resuming after a break or a compaction

- Progress lives in the ledger,
  `.superpowers/sdd/2026-09-30-an-abandoned-checkout-expires/progress.md`, which is
  gitignored. A task with a `Task N: complete` line there is done: resume at the first
  task without one.
- `git log --oneline` shows each task's commit; the messages are given in the tasks.
- Before resuming, read the spec section, 0024's Decision and this plan's *Global
  constraints* again.

## Global constraints

- Commit messages describe the platform, never study progress, and end with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **Pushing `dev` needs the architect's go-ahead.** So does starting the k3s box. The
  box and EKS never run at once.
- **order-svc returns declared error vars, never `fmt.Errorf`.**
- Comment budget: 3 lines inline, 5 on a symbol.
- order-svc's repository and activity tests need DynamoDB local on port 8000. It is
  the container `ticketbottle-order-dynamodb`, started with
  `docker compose -f services/order-svc/docker-compose.dev.yml up -d`.
- A step run through `executeShortStep` (a local activity) must be safe to run twice
  (0018).
- The new pieces:
  - the workflow ID is `ExpireOrder:<code>`, on task queue `confirm-order-tasks`;
  - the start delay is `CheckoutLifetime = PaymentTimeout + ReservationHoldGrace`
    (9m);
  - the topic is `checkout.expired`, whose body is `session_id`, `user_id`,
    `event_id`, `expired_at` and `timestamp`, all RFC3339 strings;
  - the metric is `tb_order_checkouts_expired_total`.

## Review focus

1. **A timeout never overwrites a paid order.** Task 1,
   `TestExpireIfPending_LeavesAPaidOrderAlone`.
2. **A retried expiry step sees its own write as done.** A local activity can re-run.
   Task 1, `TestExpireIfPending_AnOrderItAlreadyTimedOutAnswersTrueAgain`.
3. **A payment made in time still gets a ticket that is still there.** Task 3,
   `TestConfirmOrder_APaymentOnATimedOutOrderStillGetsItsTicket`.
4. **A failed clock start never fails a purchase.** Task 4,
   `TestCreate_AFailedClockDoesNotFailThePurchase`.
5. **A failed release still frees the chair.** Task 3,
   `TestExpireOrder_AFailedReleaseStillFreesTheChair`.
6. **The waitroom can read the event.** Task 2's RFC3339 case, and Task 6's k3s run.

---

### Task 1: an order can be timed out only while it is pending

**Files:**
- Modify: `services/order-svc/internal/order/repository/interface.go`
- Modify: `services/order-svc/internal/order/repository/order.go`
- Test: create `services/order-svc/internal/order/repository/order_expire_test.go`

**Interfaces — produces:**
`Repository.ExpireIfPending(ctx context.Context, code string) (models.Order, bool, error)`.
It returns `true` when the order is now `TIMEOUT`, whether this call wrote it or an
earlier one did. It returns `ErrOrderNotFound` for a missing order.

- [ ] **Step 1: Write the failing tests** (`order_expire_test.go`):

```go
package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/models"
)

func seedOrder(t *testing.T, r *implRepository, code string, st models.OrderStatus) {
	t.Helper()
	if _, err := r.Create(context.Background(), CreateOrderOption{
		Code: code, UserID: "u1", EventID: "e1", Currency: "VND", TotalAmount: 1000, Status: st,
	}); err != nil {
		t.Fatalf("seed %s: %v", code, err)
	}
}

func TestExpireIfPending_TimesOutAPendingOrder(t *testing.T) {
	r := newTestRepo(t)
	seedOrder(t, r, "TB-EXP-0001", models.OrderStatusPending)

	o, expired, err := r.ExpireIfPending(context.Background(), "TB-EXP-0001")
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if !expired || o.Status != models.OrderStatusTimeout {
		t.Fatalf("expired = %v, status = %s; want true, TIMEOUT", expired, o.Status)
	}
}

// A payment that confirmed first must never be overwritten by the timeout.
func TestExpireIfPending_LeavesAPaidOrderAlone(t *testing.T) {
	r := newTestRepo(t)
	seedOrder(t, r, "TB-EXP-0002", models.OrderStatusCompleted)

	_, expired, err := r.ExpireIfPending(context.Background(), "TB-EXP-0002")
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	stored, err := r.GetByCode(context.Background(), "TB-EXP-0002")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if expired || stored.Status != models.OrderStatusCompleted {
		t.Fatalf("expired = %v, stored status = %s; want false, COMPLETED", expired, stored.Status)
	}
}

// A retried step must see its own earlier write as done, not as someone else's.
func TestExpireIfPending_AnOrderItAlreadyTimedOutAnswersTrueAgain(t *testing.T) {
	r := newTestRepo(t)
	seedOrder(t, r, "TB-EXP-0003", models.OrderStatusPending)

	if _, _, err := r.ExpireIfPending(context.Background(), "TB-EXP-0003"); err != nil {
		t.Fatalf("first expire: %v", err)
	}
	_, expired, err := r.ExpireIfPending(context.Background(), "TB-EXP-0003")
	if err != nil || !expired {
		t.Fatalf("second expire = %v, %v; want true, nil", expired, err)
	}
}

func TestExpireIfPending_AMissingOrderIsNotFound(t *testing.T) {
	r := newTestRepo(t)

	_, _, err := r.ExpireIfPending(context.Background(), "TB-EXP-NONE")
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}
```

- [ ] **Step 2: Run them.**
  `cd services/order-svc && go test ./internal/order/repository/ -run TestExpireIfPending -v`.
  Expected: they fail to compile, because `ExpireIfPending` is undefined. If they
  `SKIP` instead, DynamoDB local is not running.

- [ ] **Step 3: Implement.** In `interface.go`, after `Update`, add
  `ExpireIfPending(ctx context.Context, code string) (models.Order, bool, error)`.
  In `order.go`, after `Update`:

```go
// ExpireIfPending moves a PENDING order to TIMEOUT and reports whether it is now
// TIMEOUT: true again on a retry, false for an order paid, failed or cancelled
// first. Conditional, so a timeout never overwrites a payment that won the race.
func (r *implRepository) ExpireIfPending(ctx context.Context, code string) (models.Order, bool, error) {
	expr, err := expression.NewBuilder().
		WithUpdate(expression.Set(expression.Name("status"), expression.Value(string(models.OrderStatusTimeout))).
			Set(expression.Name("updated_at"), expression.Value(r.clock()))).
		WithCondition(expression.Name("status").Equal(expression.Value(string(models.OrderStatusPending)))).
		Build()
	if err != nil {
		r.l.Errorf(ctx, "order.repository.ExpireIfPending.BuildExpression: %v", err)
		return models.Order{}, false, err
	}

	result, err := r.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableName),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pkgDynamo.BuildOrderPK(code)},
			"SK": &types.AttributeValueMemberS{Value: pkgDynamo.BuildOrderSK(code)},
		},
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ReturnValues:              types.ReturnValueAllNew,
	})
	if isConditionalCheckFailed(err) {
		// Not PENDING, or not there: read which.
		o, gErr := r.GetByCode(ctx, code)
		if gErr != nil {
			return models.Order{}, false, gErr
		}
		return o, o.Status == models.OrderStatusTimeout, nil
	}
	if err != nil {
		r.l.Errorf(ctx, "order.repository.ExpireIfPending.UpdateItem: %v", err)
		return models.Order{}, false, err
	}

	var o models.Order
	if err := attributevalue.UnmarshalMap(result.Attributes, &o); err != nil {
		r.l.Errorf(ctx, "order.repository.ExpireIfPending.UnmarshalMap: %v", err)
		return models.Order{}, false, err
	}
	return o, true, nil
}
```

  If `go vet ./...` then reports that a fake elsewhere no longer satisfies
  `Repository`, give it the method, returning its zero values.

- [ ] **Step 4: Run them.** Expected: 4 PASS. Break it: delete the
  `WithCondition(...)` line, and `TestExpireIfPending_LeavesAPaidOrderAlone` FAILS.
  Restore.

- [ ] **Step 5: Commit.**
  `git commit -m "feat(order): time out an order only while it is still pending"`, with
  the trailer.

---

### Task 2: order-svc can say a checkout expired

**Files:**
- Modify:
  - `services/order-svc/internal/order/delivery/kafka/constants.go`
  - `services/order-svc/internal/order/delivery/kafka/presenter.go`
  - `services/order-svc/internal/order/delivery/kafka/producer/producer.go`
  - `services/order-svc/internal/metrics/metrics.go`
  - `services/order-svc/internal/activities/order_activity.go`
  - `services/order-svc/internal/activities/event_publishing_activity.go`
- Test:
  - `services/order-svc/internal/order/delivery/kafka/producer/producer_test.go`
  - `services/order-svc/internal/activities/order_activity_test.go`

**Interfaces:**
- Consumes: Task 1's `ExpireIfPending`.
- Produces:
  - the topic `TopicCheckoutExpired`;
  - `kafka.CheckoutExpiredEvent`;
  - `Producer.PublishCheckoutExpired`;
  - `metrics.CheckoutsExpired`;
  - `activities.ExpireOrderResult{Order *models.Order; Expired bool}`;
  - `(*OrderActivities).ExpireOrder(ctx, code) (ExpireOrderResult, error)`;
  - `activities.PublishCheckoutExpiredInput{SessionID, UserID, EventID string}`;
  - `(*EventPublishingActivities).PublishCheckoutExpired(ctx, in) error`.

- [ ] **Step 1: Write the failing tests.** In `producer_test.go`, add a case to
  `TestPublishedCheckoutTimestampsAreRFC3339`'s table:

```go
		{
			name: "checkout expired",
			publish: func(p Producer, ctx context.Context) error {
				return p.PublishCheckoutExpired(ctx, kafka.CheckoutExpiredEvent{
					SessionID: "ss-1", UserID: "u-1", EventID: "e-1",
				})
			},
		},
```

and a new test:

```go
// The waitroom subscribes to checkout.expired and reads these names; a rename
// strands every chair it was meant to free.
func TestPublishCheckoutExpired_GoesWhereTheWaitroomReads(t *testing.T) {
	cap := &captureProducer{}
	p := NewProducer(cap, logger.InitializeZapLogger(logger.ZapConfig{Level: "error", Mode: "development", Encoding: "console"}))

	if err := p.PublishCheckoutExpired(context.Background(), kafka.CheckoutExpiredEvent{
		SessionID: "ss-1", UserID: "u-1", EventID: "e-1",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if got := cap.sent[0].Topic; got != "checkout.expired" {
		t.Fatalf("topic = %q, want checkout.expired", got)
	}
	body := cap.lastBody(t)
	for _, k := range []string{"session_id", "user_id", "event_id", "expired_at", "timestamp"} {
		if v, _ := body[k].(string); v == "" {
			t.Errorf("%s is missing or empty in %v", k, body)
		}
	}
}
```

In `order_activity_test.go`:

```go
func TestExpireOrder_TimesOutAnUnpaidOrderAndCountsIt(t *testing.T) {
	a := newTestOrderActivities(t)
	ctx := context.Background()
	opt := repo.CreateOrderOption{
		Code: "TB-EXPACT-0001", UserID: "u1", EventID: "e1",
		Currency: "VND", TotalAmount: 1000, Status: models.OrderStatusPending,
	}
	if _, err := a.CreateOrder(ctx, opt); err != nil {
		t.Fatalf("create: %v", err)
	}
	before := counterValue(t, metrics.CheckoutsExpired)

	res, err := a.ExpireOrder(ctx, opt.Code)
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if !res.Expired || res.Order.Status != models.OrderStatusTimeout {
		t.Fatalf("result = %+v, want expired TIMEOUT", res)
	}
	if got := counterValue(t, metrics.CheckoutsExpired) - before; got != 1 {
		t.Fatalf("expired counter moved by %v, want 1", got)
	}
}

func TestExpireOrder_AMissingOrderIsNotRetried(t *testing.T) {
	a := newTestOrderActivities(t)

	_, err := a.ExpireOrder(context.Background(), "TB-EXPACT-NONE")
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("err = %v, want a non-retryable application error", err)
	}
}
```

  Add imports as the file needs them: `errors`, `go.temporal.io/sdk/temporal`,
  `internal/models`, `internal/metrics`.

  Run `go test ./internal/order/delivery/kafka/producer/ ./internal/activities/ 2>&1 | head`.
  Expected: it fails to compile, because `PublishCheckoutExpired`, `CheckoutsExpired`
  and `ExpireOrder` are undefined.

- [ ] **Step 2: Implement.**
  - **`constants.go`:** add `TopicCheckoutExpired = "checkout.expired"`, aligned with
    its neighbours.
  - **`presenter.go`**, after `CheckoutFailedEvent`:

```go
// CheckoutExpiredEvent tells the waitroom an order was never paid for, so it frees
// the buyer's chair. Read by the waitroom's own CheckoutExpiredEvent.
type CheckoutExpiredEvent struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	EventID   string `json:"event_id"`
	ExpiredAt string `json:"expired_at"`
	Timestamp string `json:"timestamp"`
}
```

  - **`producer.go`:** add `PublishCheckoutExpired(ctx context.Context, event kafka.CheckoutExpiredEvent) error`
    to the `Producer` interface. After `PublishCheckoutFailed`:

```go
func (p *implProducer) PublishCheckoutExpired(ctx context.Context, event kafka.CheckoutExpiredEvent) error {
	now := util.TimeToISO8601Str(time.Now())
	event.Timestamp = now
	if event.ExpiredAt == "" {
		event.ExpiredAt = now
	}
	val, err := json.Marshal(event)
	if err != nil {
		p.l.Errorf(ctx, "order.delivery.kafka.producer.publishCheckoutExpired: %v", err)
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic:   kafka.TopicCheckoutExpired,
		Key:     sarama.StringEncoder(event.EventID),
		Value:   sarama.ByteEncoder(val),
		Headers: []sarama.RecordHeader{{Key: []byte("timestamp"), Value: []byte(now)}},
	}

	_, _, err = p.prod.SendMessage(msg)
	return err
}
```

    Any fake `Producer` in another test file needs the method too; `go vet ./...` names
    them.
  - **`metrics.go`**, after `OrdersRefundRequired`:

```go
	// CheckoutsExpired counts orders timed out unpaid: the abandonment rate the
	// waitroom's room size is sized by. See docs/design/admission-sizing.md.
	CheckoutsExpired = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "tb_order_checkouts_expired_total",
			Help: "Orders timed out because nobody paid before their hold expired.",
		},
	)
```

  - **`order_activity.go`**, after `UpdateOrderStatus`:

```go
// ExpireOrderResult is the order after a timeout attempt; Expired says it is TIMEOUT.
type ExpireOrderResult struct {
	Order   *models.Order
	Expired bool
}

// ExpireOrder times out an order nobody paid for; see Repository.ExpireIfPending.
// A re-run after a landed write answers Expired again and counts again: the rate,
// not the exact count, is what is read.
func (a *OrderActivities) ExpireOrder(ctx context.Context, code string) (ExpireOrderResult, error) {
	o, expired, err := a.Repo.ExpireIfPending(ctx, code)
	if err != nil {
		if errors.Is(err, repo.ErrOrderNotFound) {
			notFoundErr := temporal.NewNonRetryableApplicationError(
				order.ErrOrderNotFound.Error(), order.ErrTypeOrderNotFound, err,
			)
			metrics.RecordActivityFailure("ExpireOrder", notFoundErr)
			return ExpireOrderResult{}, notFoundErr
		}
		metrics.RecordActivityFailure("ExpireOrder", err)
		return ExpireOrderResult{}, err
	}

	if expired {
		metrics.CheckoutsExpired.Inc()
	}
	return ExpireOrderResult{Order: &o, Expired: expired}, nil
}
```

  - **`event_publishing_activity.go`**, after `PublishCheckoutCompleted`:

```go
type PublishCheckoutExpiredInput struct {
	SessionID string
	UserID    string
	EventID   string
}

func (a *EventPublishingActivities) PublishCheckoutExpired(ctx context.Context, in PublishCheckoutExpiredInput) error {
	if in.SessionID == "" {
		return nil
	}

	err := a.Prod.PublishCheckoutExpired(ctx, kafka.CheckoutExpiredEvent{
		SessionID: in.SessionID,
		UserID:    in.UserID,
		EventID:   in.EventID,
	})
	metrics.RecordActivityFailure("PublishCheckoutExpired", err)
	return err
}
```

- [ ] **Step 3: Run.** `go vet ./... && go test ./internal/order/delivery/kafka/producer/ ./internal/activities/ -v 2>&1 | grep -E "^(--- |ok|FAIL)"`.
  Expected: all PASS. Break it: change the topic constant to `"checkout.expire"`, and
  `TestPublishCheckoutExpired_GoesWhereTheWaitroomReads` FAILS. Restore.

- [ ] **Step 4: Commit.**
  `git commit -m "feat(order): publish checkout.expired and count timed-out orders"`, with
  the trailer.

---

### Task 3: the ExpireOrder workflow, and what a late payment gets

**Files:**
- Create: `services/order-svc/internal/workflows/expire_order.go`
- Modify:
  - `services/order-svc/internal/workflows/shared.go` (`CheckoutLifetime`)
  - `services/order-svc/internal/workflows/steps.go` (`publishCheckoutExpired`)
  - `services/order-svc/internal/workflows/confirm_order.go` (call 2)
  - `services/order-svc/internal/order/delivery/grpc/presenter.go` (call 3)
  - `services/order-svc/cmd/consumer/main.go` (register the workflow)
- Test:
  - Create `services/order-svc/internal/workflows/expire_order_test.go`
  - Modify `services/order-svc/internal/workflows/confirm_order_test.go`
  - Create `services/order-svc/internal/order/delivery/grpc/presenter_test.go`, or add
    to an existing test file in that package

**Interfaces:**
- Consumes: Task 2's activities.
- Produces:
  - `workflows.CheckoutLifetime`;
  - `workflows.GetExpireOrderWorkflowID(code) string`;
  - `workflows.ExpireOrderWorkflowInput{OrderCode string}`;
  - `workflows.ExpireOrder`.

- [ ] **Step 1: Write the failing tests** (`expire_order_test.go`):

```go
package workflows

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"go.temporal.io/sdk/temporal"
)

func timedOut(code, session string) activities.ExpireOrderResult {
	return activities.ExpireOrderResult{Expired: true, Order: &models.Order{
		Code: code, Status: models.OrderStatusTimeout, SessionID: session, UserID: "u1", EventID: "e1",
	}}
}

func TestExpireOrder_AnUnpaidOrderGivesBackEverythingItHeld(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0001").Return(timedOut("TB-EXP-0001", "sess-1"), nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, "TB-EXP-0001").Return(nil).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, "sess-1", "TB-EXP-0001").Return(nil).Once()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, activities.PublishCheckoutExpiredInput{
		SessionID: "sess-1", UserID: "u1", EventID: "e1",
	}).Return(nil).Once()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0001"})

	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("completed = %v, err = %v", env.IsWorkflowCompleted(), env.GetWorkflowError())
	}
}

// Paid, failed or cancelled first: the timer has nothing left to do.
func TestExpireOrder_ASettledOrderIsLeftAlone(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0002").Return(activities.ExpireOrderResult{
		Order: &models.Order{Code: "TB-EXP-0002", Status: models.OrderStatusCompleted, SessionID: "sess-2"},
	}, nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0002"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	requireNotCalled(t, env, "ReleaseInventory", mock.Anything, mock.Anything)
	requireNotCalled(t, env, "ReleasePurchaseSlot", mock.Anything, mock.Anything, mock.Anything)
	requireNotCalled(t, env, "PublishCheckoutExpired", mock.Anything, mock.Anything)
}

// The hold expires on its own anyway; the chair must not wait on it.
func TestExpireOrder_AFailedReleaseStillFreesTheChair(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0003").Return(timedOut("TB-EXP-0003", "sess-3"), nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, "TB-EXP-0003").
		Return(temporal.NewNonRetryableApplicationError("inventory down", "Unavailable", errors.New("inventory down"))).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, "sess-3", "TB-EXP-0003").Return(nil).Once()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0003"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed release failed the timeout: %v", err)
	}
}

func TestExpireOrder_WithoutAWaitingRoomNothingIsPublished(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("ExpireOrder", mock.Anything, "TB-EXP-0004").Return(timedOut("TB-EXP-0004", ""), nil).Once()
	env.OnActivity("ReleaseInventory", mock.Anything, "TB-EXP-0004").Return(nil).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, "user#u1:event#e1", "TB-EXP-0004").Return(nil).Once()
	env.OnActivity("PublishCheckoutExpired", mock.Anything, mock.Anything).Return(nil).Maybe()

	env.ExecuteWorkflow(ExpireOrder, &ExpireOrderWorkflowInput{OrderCode: "TB-EXP-0004"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}
	requireNotCalled(t, env, "PublishCheckoutExpired", mock.Anything, mock.Anything)
}
```

In `confirm_order_test.go` (call 2):

```go
// The provider took the money inside its window; the confirmation ran long. A
// ticket that is still there is theirs, as it was before orders could time out.
func TestConfirmOrder_APaymentOnATimedOutOrderStillGetsItsTicket(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0101", Status: models.OrderStatusTimeout, SessionID: "sess-9", UserID: "u9", EventID: "e9"}, nil).Once()
	env.OnActivity("ConfirmInventory", mock.Anything, "TB-TEST-0101").Return(nil).Once()
	env.OnActivity("UpdateOrderStatus", mock.Anything, "TB-TEST-0101", models.OrderStatusCompleted).Return(nil).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity("PublishCheckoutCompleted", mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{OrderCode: "TB-TEST-0101", Status: models.OrderStatusCompleted})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a payment on a timed-out order with stock left failed: %v", err)
	}
}

func TestConfirmOrder_APaymentOnATimedOutOrderWhoseStockIsGoneIsRefunded(t *testing.T) {
	env := newTestEnv(t)
	env.OnActivity("GetOrder", mock.Anything, mock.Anything).
		Return(&models.Order{Code: "TB-TEST-0102", Status: models.OrderStatusTimeout, UserID: "u9", EventID: "e9"}, nil).Once()
	env.OnActivity("ConfirmInventory", mock.Anything, mock.Anything).
		Return(temporal.NewNonRetryableApplicationError("stock is gone", order.ErrTypeInventoryCannotConfirm, errors.New("stock is gone"))).Once()
	env.OnActivity("UpdateOrderStatus", mock.Anything, mock.Anything, models.OrderStatusRefundRequired).Return(nil).Once()
	env.OnActivity("PublishRefundRequired", mock.Anything, mock.Anything).Return(nil).Once()
	env.OnActivity("ReleasePurchaseSlot", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(ConfirmOrder, &ConfirmOrderWorkflowInput{OrderCode: "TB-TEST-0102", Status: models.OrderStatusCompleted})

	if env.GetWorkflowError() == nil {
		t.Fatal("an unfulfillable paid order must still fail the workflow so it is visible")
	}
}
```

In the grpc delivery package (call 3):

```go
// The order contract has no expired status; a buyer must see a finished checkout.
func TestATimedOutOrderReadsCanceled(t *testing.T) {
	if got := GrpcOrderStatusValue[models.OrderStatusTimeout]; got != orderpb.OrderStatus_ORDER_STATUS_CANCELED {
		t.Fatalf("TIMEOUT reads %s, want ORDER_STATUS_CANCELED", got)
	}
}
```

  Run `go test ./internal/workflows/ ./internal/order/delivery/grpc/ 2>&1 | head`.
  Expected: it fails to compile, because `ExpireOrder` is undefined.

- [ ] **Step 2: Implement.**
  - **`shared.go`:** replace the body of `reservationExpiry` with
    `return now.Add(CheckoutLifetime)`, and add to the `const` block:

```go
	// CheckoutLifetime is how long an unpaid checkout lives: the hold's length, and
	// the delay before ExpireOrder times the order out.
	CheckoutLifetime = PaymentTimeout + ReservationHoldGrace
```

  - **`steps.go`**, after `publishCheckoutCompleted`:

```go
func publishCheckoutExpired(ctx workflow.Context, o *models.Order) error {
	if o.SessionID == "" {
		return nil
	}

	return executeShortStep(ctx, epActs.PublishCheckoutExpired,
		activities.PublishCheckoutExpiredInput{
			SessionID: o.SessionID,
			UserID:    o.UserID,
			EventID:   o.EventID,
		}).Get(ctx, nil)
}
```

  - **`expire_order.go`:**

```go
package workflows

import (
	"fmt"

	"github.com/vogiaan1904/ticketbottle-order/internal/activities"
	"go.temporal.io/sdk/workflow"
)

func GetExpireOrderWorkflowID(oCode string) string {
	return fmt.Sprintf("ExpireOrder:%s", oCode)
}

type ExpireOrderWorkflowInput struct {
	OrderCode string
}

// ExpireOrder times out an order nobody paid for, once its hold has expired.
//
//	still PENDING -> TIMEOUT; release the hold, free the purchase slot and the chair
//	anything else -> nothing: paid, failed or cancelled first
//
// Started delayed by CheckoutLifetime. See docs/design/admission-sizing.md#when-a-checkout-is-abandoned.
func ExpireOrder(ctx workflow.Context, in *ExpireOrderWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	ctx = workflow.WithActivityOptions(ctx, getCompensationActivityOptions())

	var res activities.ExpireOrderResult
	if err := executeShortStep(ctx, oActs.ExpireOrder, in.OrderCode).Get(ctx, &res); err != nil {
		return err
	}
	if !res.Expired {
		logger.Info("Order settled before its hold expired; nothing to expire", "orderCode", in.OrderCode)
		return nil
	}
	o := res.Order

	// Best-effort from here: the order is TIMEOUT, and the hold and the chair
	// each still have their own expiry behind them.
	if err := workflow.ExecuteActivity(ctx, iActs.ReleaseInventory, o.Code).Get(ctx, nil); err != nil {
		logger.Error("Failed to release a timed-out order's hold; inventory's expiry worker will", "error", err, "orderCode", o.Code)
	}
	freePurchaseSlot(ctx, o)
	if err := publishCheckoutExpired(ctx, o); err != nil {
		logger.Error("Failed to tell the waiting room; the chair is reclaimed by its TTL", "error", err, "orderCode", o.Code)
	}

	logger.Info("Order timed out unpaid", "orderCode", o.Code)
	return nil
}
```

  - **`confirm_order.go`** (call 2): change `if o.Status != models.OrderStatusPending {`
    to

```go
	// TIMEOUT is confirmed like PENDING: the provider only takes money inside its
	// window, and inventory re-acquires the ticket or it is refunded below.
	if o.Status != models.OrderStatusPending && o.Status != models.OrderStatusTimeout {
```

    Remove `models.OrderStatusTimeout` from the `case` that calls `markForRefund`. No
    workflow version is needed: nothing has ever written `TIMEOUT`, so no recorded
    history took that branch.
  - **`presenter.go`** (call 3): add
    `models.OrderStatusTimeout: orderpb.OrderStatus_ORDER_STATUS_CANCELED,` to
    `GrpcOrderStatusValue`, with the comment `// No expired status on the wire; see 0024.`
  - **`cmd/consumer/main.go`**: `w.RegisterWorkflow(workflows.ExpireOrder)`, after
    `ConfirmOrder`.

- [ ] **Step 3: Run.** `go vet ./... && go test ./internal/workflows/ ./internal/order/delivery/grpc/ -v 2>&1 | grep -E "^(--- FAIL|ok|FAIL)"`.
  Expected: `ok` for both, including `replay_test.go`, which proves the recorded
  histories still replay. Break it:
  - Drop `&& o.Status != models.OrderStatusTimeout`:
    `TestConfirmOrder_APaymentOnATimedOutOrderStillGetsItsTicket` FAILS.
  - Make the release error `return err`:
    `TestExpireOrder_AFailedReleaseStillFreesTheChair` FAILS.

  Restore after each.

- [ ] **Step 4: Commit.**
  `git commit -m "feat(order): time out an unpaid order when its hold expires"`, with the
  trailer.

---

### Task 4: every order starts its clock

**Files:**
- Modify: `services/order-svc/internal/order/service/order.go`
- Test: `services/order-svc/internal/order/service/create_test.go`

**Interfaces:** consumes Task 3's `ExpireOrder`, `ExpireOrderWorkflowInput`,
`GetExpireOrderWorkflowID` and `CheckoutLifetime`.

- [ ] **Step 1: Write the failing tests.** First, teach `fakeTemporalClient` the second
  start. Add these fields:

```go
	expireOpts     *temporalCli.StartWorkflowOptions
	expireInput    *workflows.ExpireOrderWorkflowInput
	expireStartErr error
```

and, first in `ExecuteWorkflow`:

```go
	if len(args) > 0 {
		if in, ok := args[0].(*workflows.ExpireOrderWorkflowInput); ok {
			c.expireOpts, c.expireInput = &options, in
			return &fakeWorkflowRun{id: options.ID}, c.expireStartErr
		}
	}
```

Then the tests:

```go
// The clock runs as long as the hold, and lives on the worker that has the producer.
func TestCreate_StartsTheCheckoutClockForTheHoldsLength(t *testing.T) {
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}}
	s, _ := newCreateService(t, tprCli, true, "sess-1")

	if _, err := s.Create(context.Background(), createInput()); err != nil {
		t.Fatalf("create: %v", err)
	}

	code := tprCli.startedInput.OrderCode
	if tprCli.expireOpts == nil {
		t.Fatal("no clock was started for the order")
	}
	if o := tprCli.expireOpts; o.StartDelay != workflows.CheckoutLifetime ||
		o.ID != workflows.GetExpireOrderWorkflowID(code) || o.TaskQueue != temporal.ConfirmOrderTaskQueue ||
		tprCli.expireInput.OrderCode != code {
		t.Fatalf("clock = %+v for %+v; want %s on %s after %s", *o, tprCli.expireInput,
			workflows.GetExpireOrderWorkflowID(code), temporal.ConfirmOrderTaskQueue, workflows.CheckoutLifetime)
	}
}

// The order is live; without its clock it only ends as it did before one existed.
func TestCreate_AFailedClockDoesNotFailThePurchase(t *testing.T) {
	tprCli := &fakeTemporalClient{run: &fakeWorkflowRun{}, expireStartErr: errors.New("frontend unavailable")}
	s, _ := newCreateService(t, tprCli, true, "sess-1")

	if _, err := s.Create(context.Background(), createInput()); err != nil {
		t.Fatalf("a failed clock start failed the purchase: %v", err)
	}
}

func TestCreate_ALostRaceStartsNoClock(t *testing.T) {
	tprCli := &fakeTemporalClient{
		run: &fakeWorkflowRun{getErr: workflows.NewInsufficientInventoryError(workflows.ErrInsufficientInventory)},
	}
	s, _ := newCreateService(t, tprCli, false, "")

	_, _ = s.Create(context.Background(), createInput())

	if tprCli.expireOpts != nil {
		t.Fatal("an order that was never written got a clock")
	}
}
```

  The `temporal` in these tests is the infra package,
  `github.com/vogiaan1904/ticketbottle-order/internal/infra/temporal`. Alias it if the
  file already uses that name.

  Run `go test ./internal/order/service/ -run 'Clock' -v 2>&1 | grep -E "^(--- |ok|FAIL)"`.
  Expected: `TestCreate_StartsTheCheckoutClockForTheHoldsLength` FAILS with "no
  clock was started".

- [ ] **Step 2: Implement.** In `order.go`, in `Create`, just before
  `observeDuration("completed")`, add `s.startCheckoutClock(ctx, code)`. After
  `Create`:

```go
// startCheckoutClock schedules ExpireOrder for when the order's hold expires.
// Logged, not returned: the order is live, and without its clock it ends as it did
// before one existed -- chair at its TTL, order left PENDING.
func (s *implService) startCheckoutClock(ctx context.Context, code string) {
	_, err := s.temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:         workflows.GetExpireOrderWorkflowID(code),
		TaskQueue:  temporal.ConfirmOrderTaskQueue,
		StartDelay: workflows.CheckoutLifetime,
	}, workflows.ExpireOrder, &workflows.ExpireOrderWorkflowInput{OrderCode: code})
	if err != nil {
		s.l.Errorf(ctx, "internal.order.service.startCheckoutClock: order %s will not time out: %v", code, err)
	}
}
```

- [ ] **Step 3: Run the whole service.**
  `go vet ./... && go test ./... 2>&1 | grep -E "^(ok|FAIL)"`. Expected: every package
  `ok`. Break it: move the `startCheckoutClock` call above `wfRun.Get`, and
  `TestCreate_ALostRaceStartsNoClock` FAILS. Restore.

- [ ] **Step 4: Commit.**
  `git commit -m "feat(order): start each order's checkout clock once the order exists"`,
  with the trailer.

---

### Task 5: the acceptance run, and the documents

**Files:**
- Create: `deploy/scripts/gate-checkout-expiry.sh` (executable)
- Modify:
  - `deploy/Makefile`: add `k3s-gate-checkout-expiry`
  - `services/order-svc/CLAUDE.md`
  - `services/order-svc/docs/RESERVATION_HOLD.md`
  - `services/order-svc/docs/PURCHASE_SLOT.md`
  - `.claude/skills/system-map/references/order.md`
  - `.claude/skills/system-map/SKILL.md`
  - root `CLAUDE.md`
  - the design's `Status:`

- [ ] **Step 1: The script.** Model it on `deploy/scripts/gate-sold-out.sh`: the same
  helpers, and the same event setup with a class of 10 tickets. It takes two
  buyers:
  1. A and B are admitted, and each orders 1 ticket. B pays and is `COMPLETED`; A
     never pays. Record `T0` after A's order.
  2. Every 15s, up to 12 minutes, read
     `redis-cli ZSCORE waitroom:<event>:checkouts <A's session>` through
     `kubectl -n ticketbottle exec statefulset/redis`. Record when it becomes empty.
  3. Once it is empty, check:
     - A's order reads `CANCELED` through `GET /orders/code/<code>`;
     - it is `TIMEOUT` in DynamoDB: `aws dynamodb get-item --region us-east-1 --table-name ticketbottle-orders`
       with the key built as `pkg/dynamodb` builds it (`BuildOrderPK`/`BuildOrderSK`);
     - A's session in Redis reads `"status":"expired"`;
     - A's reservation is no longer `ACTIVE`:
       `psql ... -d ticketbottle_inventory -c "SELECT status FROM reservation WHERE order_code='<code>'"`;
     - B's order still reads `COMPLETED`;
     - the chair freed between 8m30s and 11m after `T0`, well under the chair TTL of
       15m.
  4. The order-consumer's logs contain B's code with `nothing to expire`.
  5. End with `echo "CHECKOUT-EXPIRY GATE PASSED"`.

  Run `bash -n` on it. Expected: no output.

- [ ] **Step 2: The target.** In `deploy/Makefile`, next to `k3s-gate-sold-out`, and in
  `.PHONY`:

```make
k3s-gate-checkout-expiry: ## An unpaid order times out at its hold's expiry and frees its chair (~12 min; tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-checkout-expiry.sh
```

- [ ] **Step 3: Route the documents.**
  - **order-svc `CLAUDE.md`.**
    - The workflows list gains `ExpireOrder`: started by `Create`, delayed by
      `CheckoutLifetime`, flips `PENDING` to `TIMEOUT` conditionally, releases, frees
      the slot and publishes `checkout.expired`.
    - `ConfirmOrder` confirms a `TIMEOUT` order like a `PENDING` one.
    - The short-steps list gains `ExpireOrder` and `PublishCheckoutExpired`.
  - **`RESERVATION_HOLD.md`.** The hold's length is `CheckoutLifetime`, which is also
    when `ExpireOrder` fires. The re-acquire backstop now also covers a payment on a
    `TIMEOUT` order.
  - **`PURCHASE_SLOT.md`.** Under *Lifecycle*, `ExpireOrder` also releases the slot,
    so an unpaid order no longer locks a buyer out.
  - **System-map `order.md`.** A *By question* row: "An order nobody pays for" →
    `docs/design/admission-sizing.md`, *When a checkout is abandoned*. Verify in
    `services/order-svc/internal/workflows/expire_order.go`.
  - **System-map `SKILL.md`.** The acceptance row adds
    `deploy/scripts/gate-checkout-expiry.sh`.
  - **Root `CLAUDE.md`.**
    - The topic list gains `checkout.expired`.
    - A *Decided* row: "What happens to an order nobody pays for?" |
      `docs/decisions/0024` | "Times out at its hold's expiry, frees the chair; built,
      not yet verified on k3s".
  - **Design `Status:`** "built; not yet verified on k3s".

- [ ] **Step 4: Checks.** `python3 .claude/skills/system-map/scripts/check_map.py`,
  `python3 docs/decisions/index.py && python3 docs/decisions/index.py --check`. Expected:
  both clean.

- [ ] **Step 5: Commit.**
  `git commit -m "test(deploy): check an unpaid order times out and frees its chair"`,
  with the trailer.

---

### Task 6: verify on k3s, then record it

This needs the architect's go-ahead: it pushes `dev` and starts the box.

1. Push. Wait for `build-push-ecr`, `go-tests` and `ts-tests` on the new SHA.
2. `aws eks list-clusters --region us-east-1` prints nothing. Then:
   - start the box;
   - allow your IP: `make -C deploy update-my-ip`, then a saved
     `terraform plan -out` in `deploy/terraform/envs/k3s`, checked to change only the
     SSH CIDR, then applied. Add the second egress IP by CLI if the network rotates;
   - open the tunnel with `-o ServerAliveInterval=15`;
   - run `k3s-kubeconfig`.
3. `make -C deploy k3s-deploy`. Expected: every app is on the new SHA, and
   order-consumer's worker registers `ExpireOrder`.
4. `make -C deploy k3s-gate2` and `make -C deploy k3s-gate-sold-out`. Expected: both
   pass. The door and the purchase flow are unchanged.
5. `make -C deploy k3s-gate-checkout-expiry 2>&1 | tee /tmp/expiry.log`, with
   `set -o pipefail`. Expected: `CHECKOUT-EXPIRY GATE PASSED`, with the chair freed at
   about 9 minutes.
6. Query Prometheus: `tb_order_checkouts_expired_total` is at least 1, and
   `sum by (method, code) (tb_grpc_requests_total{service="inventory-service",method=~".*Release"})`
   shows the timeout's `Release` as `OK`.
7. Stop the box.
8. Record:
   - 0024 becomes `accepted`, with an Outcome;
   - the design's `Status:` reads verified, and its step 3 reads built;
   - the register row reads verified;
   - this plan reads COMPLETE, with *Results*;
   - regenerate the decisions index, commit, and push on the go-ahead.
