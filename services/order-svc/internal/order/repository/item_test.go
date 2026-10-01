package repository

import (
	"context"
	"testing"
)

// CreateOrderItems is retried by Temporal, and as a local activity it re-runs when
// the workflow task that ran it fails. A retry must rewrite the same items, not
// add a second set to the order.
func TestCreateManyItems_ARetryWritesTheSameItems(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	opts := []CreateOrderItemOption{
		{OrderCode: "TB-ITEMS-0001", TicketClassID: "1", TicketClassName: "GA", Quantity: 1},
		{OrderCode: "TB-ITEMS-0001", TicketClassID: "2", TicketClassName: "VIP", Quantity: 1},
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := repo.CreateManyItems(ctx, "TB-ITEMS-0001", opts); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	got, err := repo.ListItemByOrderCode(ctx, "TB-ITEMS-0001")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != len(opts) {
		t.Fatalf("order has %d items after a retried write, want %d", len(got), len(opts))
	}
}
