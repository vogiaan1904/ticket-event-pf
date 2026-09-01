package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
)

// A retry that reuses a code must be refused, not served. The old PutItem had
// no condition, so a replay would overwrite a paid order with a pending one.
func TestCreate_SecondWriteOfTheSameCodeIsRefused(t *testing.T) {
	repo := newTestRepo(t)

	opt := CreateOrderOption{
		Code: "TB-DUP-0001", UserID: "u1", EventID: "e1",
		Currency: "VND", TotalAmount: 1000,
	}

	if _, err := repo.Create(context.Background(), opt); err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err := repo.Create(context.Background(), opt)
	if !errors.Is(err, order.ErrOrderAlreadyExists) {
		t.Fatalf("second create returned %v, want ErrOrderAlreadyExists", err)
	}
}
