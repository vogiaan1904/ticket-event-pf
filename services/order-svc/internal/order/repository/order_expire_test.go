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

func TestCancelIfPending_CancelsAPendingOrder(t *testing.T) {
	r := newTestRepo(t)
	seedOrder(t, r, "TB-CAN-1001", models.OrderStatusPending)

	o, cancelled, err := r.CancelIfPending(context.Background(), "TB-CAN-1001")
	if err != nil || !cancelled || o.Status != models.OrderStatusCancelled {
		t.Fatalf("cancelled = %v, status = %s, err = %v; want true, CANCELLED", cancelled, o.Status, err)
	}
}

func TestCancelIfPending_LeavesAPaidOrderAlone(t *testing.T) {
	r := newTestRepo(t)
	seedOrder(t, r, "TB-CAN-1002", models.OrderStatusCompleted)

	_, cancelled, err := r.CancelIfPending(context.Background(), "TB-CAN-1002")
	stored, _ := r.GetByCode(context.Background(), "TB-CAN-1002")
	if err != nil || cancelled || stored.Status != models.OrderStatusCompleted {
		t.Fatalf("cancelled = %v, stored = %s, err = %v; want false, COMPLETED", cancelled, stored.Status, err)
	}
}
