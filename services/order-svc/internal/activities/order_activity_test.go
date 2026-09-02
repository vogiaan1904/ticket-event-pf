package activities

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	repo "github.com/vogiaan1904/ticketbottle-order/internal/order/repository"
	"go.temporal.io/sdk/temporal"
)

// Temporal guarantees an activity can be retried after its side effect
// already landed -- a worker crashing between a successful PutItem and the
// result being recorded is exactly the case the retry policy exists for. A
// retry of CreateOrder with the same input must return the order that was
// already written, not fail the workflow and trigger compensation.
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

// A code reused by a different user/event is not a retry -- it is a
// code-generation collision, and serving the first buyer's order back to the
// second would hand over someone else's order. It must fail, and fail
// without Temporal retrying it into the same wrong answer five times.
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
