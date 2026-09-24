package activities

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/vogiaan1904/ticketbottle-order/internal/metrics"
	"github.com/vogiaan1904/ticketbottle-order/internal/models"
	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"go.temporal.io/sdk/temporal"
)

// An activity can be retried after its side effect already landed -- a worker
// crashing between a successful PutItem and the recorded result. The retry must
// return the written order, not fail the workflow into compensation.
func TestCreateOrder_RetryOfALandedWriteReturnsTheSameOrder(t *testing.T) {
	a := newTestOrderActivities(t)

	opt := repo.CreateOrderOption{
		Code: "TB-RETRY-0001", UserID: "u1", EventID: "e1",
		Currency: "VND", TotalAmount: 1000,
	}

	first, err := a.CreateOrder(context.Background(), opt)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := a.CreateOrder(context.Background(), opt)
	if err != nil {
		t.Fatalf("retry returned an error instead of the existing order: %v", err)
	}

	if second.Code != first.Code || second.UserID != first.UserID || second.EventID != first.EventID {
		t.Fatalf("retry returned a different order: first=%+v second=%+v", first, second)
	}
}

// A code reused by a different user/event is a collision, not a retry: serving
// the first buyer's order to the second hands over someone else's order. Fail,
// and fail without retrying into the same wrong answer five times.
func TestCreateOrder_CodeReusedByADifferentOrderIsRefused(t *testing.T) {
	a := newTestOrderActivities(t)

	first := repo.CreateOrderOption{
		Code: "TB-COLLIDE-0001", UserID: "u1", EventID: "e1",
		Currency: "VND", TotalAmount: 1000,
	}
	if _, err := a.CreateOrder(context.Background(), first); err != nil {
		t.Fatalf("first call: %v", err)
	}

	second := first
	second.UserID = "u2"

	_, err := a.CreateOrder(context.Background(), second)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected a *temporal.ApplicationError, got %T: %v", err, err)
	}
	if appErr.Type() != order.ErrTypeOrderCodeCollision {
		t.Fatalf("error type = %q, want %q", appErr.Type(), order.ErrTypeOrderCodeCollision)
	}
	if !appErr.NonRetryable() {
		t.Fatal("expected the collision error to be non-retryable")
	}
}

// OrdersNeedingRefund pages on this counter, so it must move exactly when a
// paid order is written into REFUND_REQUIRED -- and on no other status write.
func TestUpdateOrderStatus_CountsOnlyAWrittenRefundRequired(t *testing.T) {
	a := newTestOrderActivities(t)
	ctx := context.Background()

	opt := repo.CreateOrderOption{
		Code: "TB-REFUND-0001", UserID: "u1", EventID: "e1",
		Currency: "VND", TotalAmount: 1000,
	}
	if _, err := a.CreateOrder(ctx, opt); err != nil {
		t.Fatalf("create: %v", err)
	}

	before := counterValue(t, metrics.OrdersRefundRequired)

	if err := a.UpdateOrderStatus(ctx, opt.Code, models.OrderStatusCompleted); err != nil {
		t.Fatalf("update to COMPLETED: %v", err)
	}
	if got := counterValue(t, metrics.OrdersRefundRequired) - before; got != 0 {
		t.Fatalf("a COMPLETED write moved the refund counter by %v", got)
	}

	if err := a.UpdateOrderStatus(ctx, opt.Code, models.OrderStatusRefundRequired); err != nil {
		t.Fatalf("update to REFUND_REQUIRED: %v", err)
	}
	if got := counterValue(t, metrics.OrdersRefundRequired) - before; got != 1 {
		t.Fatalf("refund counter moved by %v, want 1", got)
	}
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}
