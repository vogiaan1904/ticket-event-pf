package activities

import (
	"context"
	"errors"
	"testing"

	orderpkg "github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeInventoryClient stubs inventory.InventoryServiceClient. Only Confirm is
// ever driven by these tests; every other method exists solely to satisfy the
// interface.
type fakeInventoryClient struct {
	inventory.InventoryServiceClient
	confirmErr error
}

func (f *fakeInventoryClient) Confirm(ctx context.Context, in *inventory.ConfirmRequest, opts ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, f.confirmErr
}

// Rows hard-deleted (admin ticket-class delete) or never created leave nothing
// to confirm -- as unfulfillable as a resold hold. Tag it non-retryable like
// FailedPrecondition, not as infrastructure that might still work.
func TestConfirmInventory_NotFoundIsUnfulfillable(t *testing.T) {
	a := NewInventoryActivities(&fakeInventoryClient{
		confirmErr: status.Error(codes.NotFound, "no reservations for order"),
	})

	err := a.ConfirmInventory(context.Background(), "TB-GONE-0001")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected a *temporal.ApplicationError, got %T: %v", err, err)
	}
	if appErr.Type() != orderpkg.ErrTypeInventoryCannotConfirm {
		t.Fatalf("error type = %q, want %q", appErr.Type(), orderpkg.ErrTypeInventoryCannotConfirm)
	}
	if !appErr.NonRetryable() {
		t.Fatal("expected the NotFound confirm failure to be non-retryable")
	}
}

// Internal means inventory-svc's own counters and reservation rows disagree
// with each other -- corruption, not a business outcome the buyer caused. It
// must keep failing the workflow for a retry/manual-intervention path rather
// than being classified as unfulfillable and silently refunded.
func TestConfirmInventory_InternalIsNotClassifiedAsUnfulfillable(t *testing.T) {
	a := NewInventoryActivities(&fakeInventoryClient{
		confirmErr: status.Error(codes.Internal, "inventory counter drift detected"),
	})

	err := a.ConfirmInventory(context.Background(), "TB-DRIFT-0001")
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		t.Fatalf("Internal must not be tagged %q, got a non-retryable ApplicationError: %v", orderpkg.ErrTypeInventoryCannotConfirm, appErr)
	}
}
