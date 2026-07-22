package service

import (
	"context"
	"testing"
)

// Two line items for the same ticket class must be summed.
//
// This is a regression guard, not a discriminator: it passed against the
// broken implementation too. Both original defects fired on this input and the
// row-count check short-circuited first -- ids=[5,5] matched one row, so
// len(rows) != len(inputs) returned false before the qty loop ran. Only
// TestCheckAvailability_DuplicateIDsWithinCapacity_Accepts, which expects
// acceptance, could fail against the old code.
func TestCheckAvailability_DuplicateIDs_SumsQuantities(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)
	tc := seedTicketClass(t, repo, 5, 0, 0)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: tc.ID, Qty: 3},
		{TicketClassID: tc.ID, Qty: 4}, // 7 total against 5 available
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if ok {
		t.Fatal("accept = true, want false: 3 + 4 exceeds the 5 available")
	}
}

// The mirror case: duplicates that fit must not trip the row-count check.
func TestCheckAvailability_DuplicateIDsWithinCapacity_Accepts(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)
	tc := seedTicketClass(t, repo, 10, 0, 0)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: tc.ID, Qty: 3},
		{TicketClassID: tc.ID, Qty: 4}, // 7 against 10 available
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if !ok {
		t.Fatal("accept = false, want true: 3 + 4 fits within the 10 available")
	}
}

func TestCheckAvailability_UnknownID_Rejects(t *testing.T) {
	repo := newTestDB(t)
	tcSvc := NewTicketClassService(newTestLogger(), repo)

	ok, err := tcSvc.CheckAvailability(context.Background(), []CheckAvailabilityInput{
		{TicketClassID: 999999999, Qty: 1},
	})
	if err != nil {
		t.Fatalf("CheckAvailability: %v", err)
	}
	if ok {
		t.Fatal("accept = true, want false for an unknown ticket class")
	}
}
