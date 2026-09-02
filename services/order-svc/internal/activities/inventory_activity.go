package activities

import (
	"context"

	"github.com/vogiaan1904/ticketbottle-order/internal/order"
	"github.com/vogiaan1904/ticketbottle-order/pkg/grpc/inventory"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InventoryActivities struct {
	Client inventory.InventoryServiceClient
}

func NewInventoryActivities(client inventory.InventoryServiceClient) *InventoryActivities {
	return &InventoryActivities{
		Client: client,
	}
}

func (a *InventoryActivities) ReserveInventory(ctx context.Context, orderCode string, expiresAt string, items []*inventory.ReserveItem) error {
	_, err := a.Client.Reserve(ctx, &inventory.ReserveRequest{
		OrderCode: orderCode,
		ExpiresAt: expiresAt,
		Items:     items,
	})
	if err != nil {
		// A business rejection from inventory-svc (sold out, sale closed, state
		// conflict) arrives as FailedPrecondition. Retrying it burns the default
		// five attempts with backoff before failing anyway, and reaches the
		// caller as an opaque activity error, so mark it non-retryable and tag
		// it for the gRPC layer to turn into a 4xx.
		if status.Code(err) == codes.FailedPrecondition {
			return temporal.NewNonRetryableApplicationError(
				order.ErrNotEnoughTickets.Error(),
				order.ErrTypeInsufficientInventory,
				err,
			)
		}
		return err
	}

	return nil
}

func (a *InventoryActivities) ReleaseInventory(ctx context.Context, orderCode string) error {
	_, err := a.Client.Release(ctx, &inventory.ReleaseRequest{
		OrderCode: orderCode,
	})
	if err != nil {
		return err
	}

	return nil
}

func (a *InventoryActivities) ConfirmInventory(ctx context.Context, orderCode string) error {
	_, err := a.Client.Confirm(ctx, &inventory.ConfirmRequest{
		OrderCode: orderCode,
	})
	if err != nil {
		// A business rejection from inventory-svc arrives as either of two
		// codes, and both mean the order is definitively unfulfillable, not a
		// fault to retry: FailedPrecondition is the hold already expired and
		// the stock was resold, or the reservation is in a conflicting state;
		// NotFound is no reservation rows at all, which happens when they were
		// hard-deleted (an admin ticket-class delete) or a reserve never
		// happened for this order. Either way it is tagged non-retryable and
		// distinguishable from an infrastructure failure on the same call.
		// Internal (a counter/row drift) is deliberately left out of this --
		// that is our own bug, not the buyer's, and belongs with whoever gets
		// paged rather than being silently refunded.
		code := status.Code(err)
		if code == codes.FailedPrecondition || code == codes.NotFound {
			return temporal.NewNonRetryableApplicationError(
				order.ErrInventoryCannotConfirm.Error(),
				order.ErrTypeInventoryCannotConfirm,
				err,
			)
		}
		return err
	}

	return nil
}

func (a *InventoryActivities) CheckAvailability(ctx context.Context, items []*inventory.CheckAvailabilityItem) (bool, error) {
	resp, err := a.Client.CheckAvailability(ctx, &inventory.CheckAvailabilityRequest{
		Items: items,
	})
	if err != nil {
		return false, err
	}

	return resp.Accept, nil
}
